package tasks

import (
	"cmp"
	"slices"
	"strings"
	"time"
)

// How the Service keeps its summaries in opts.SummaryStore (issue #352). The
// summaries in memory are the ones that count; the store is handed all of
// them whenever one changes, and read once, when summaries are first used.

// SummaryStoreError is why summaries are not being saved, or nil while they
// are or there is no store.
func (s *Service) SummaryStoreError() error {
	if s.opts.SummaryStore == nil {
		return nil
	}
	return s.opts.SummaryStore.Err()
}

// loadSummariesLocked reads the store into memory unless it was read already,
// and reports whether what is in memory should be saved: something stored
// was dropped, or a summary was made while the store could not be read.
// s.summaryMu must be held.
func (s *Service) loadSummariesLocked() bool {
	store := s.opts.SummaryStore
	if store == nil || s.summaryLoaded {
		return false
	}
	entries, moved, err := store.load(s.opts.Now())
	if err != nil {
		if err.Error() != s.summaryLoadErr {
			s.opts.Logf("task summaries: not saved: %v", err)
			s.summaryLoadErr = err.Error()
		}
		return false
	}
	s.summaryLoaded, s.summaryLoadErr = true, ""
	if moved != "" {
		s.opts.Logf("task summaries: moved aside %s, which this version of panemux cannot use", moved)
	}
	dirty := false
	for _, entry := range s.summaries {
		dirty = dirty || entry.result != nil
	}
	for _, stored := range entries {
		key := stored.key()
		if s.summaryHosts != nil && !s.summaryHosts[key.host] {
			dirty = true
			continue
		}
		if s.summaries[key] != nil {
			continue
		}
		entry := &summaryEntry{
			result:     &Summary{Text: stored.Text, Remaining: stored.Remaining, UnexpectedModel: stored.UnexpectedModel},
			resultAt:   stored.SummarizedAt,
			inputHash:  stored.InputHash,
			summarizer: stored.Summarizer,
			lastSeen:   stored.LastSeen,
			savedSeen:  stored.LastSeen,
		}
		// An answer from another summarizer is current for no log.
		if stored.Summarizer == summarizerVersion(key.agent) {
			entry.resultLog = LogVersion{File: stored.Log.File, ModTime: stored.Log.ModTime, Size: stored.Log.Size}
			entry.triedLog = entry.resultLog
		}
		s.summaries[key] = entry
	}
	return dirty
}

// pruneSummariesLocked drops the summaries of tasks not listed for
// summaryRetention, then the least recently listed beyond
// maxStoredSummaries, and reports whether it dropped any. A running summary
// is kept. s.summaryMu must be held.
func (s *Service) pruneSummariesLocked(now time.Time) bool {
	dropped := false
	var keys []summaryKey
	for key, entry := range s.summaries {
		switch {
		case entry.running:
		case now.Sub(entry.lastSeen) > summaryRetention:
			delete(s.summaries, key)
			dropped = true
		default:
			keys = append(keys, key)
		}
	}
	if excess := len(s.summaries) - maxStoredSummaries; excess > 0 {
		slices.SortFunc(keys, func(a, b summaryKey) int {
			return cmp.Or(s.summaries[a].lastSeen.Compare(s.summaries[b].lastSeen),
				strings.Compare(a.host, b.host), strings.Compare(a.agent, b.agent), strings.Compare(a.sessionID, b.sessionID))
		})
		for _, key := range keys[:min(excess, len(keys))] {
			delete(s.summaries, key)
		}
		dropped = true
	}
	return dropped
}

// summarySaveLocked returns what saves the summaries when dirty, to be called
// once s.summaryMu is released: a file write is not made while polls wait.
// The generation it takes keeps a save that loses the race to a later one
// from replacing it. s.summaryMu must be held.
func (s *Service) summarySaveLocked(dirty bool) func() {
	store := s.opts.SummaryStore
	if !dirty || store == nil || !s.summaryLoaded {
		return func() {}
	}
	s.summaryGen++
	gen := s.summaryGen
	var entries []storedSummary
	for key, entry := range s.summaries {
		if entry.result == nil {
			continue
		}
		entry.savedSeen = entry.lastSeen
		entries = append(entries, storedSummary{
			Host:            key.host,
			Agent:           key.agent,
			SessionID:       key.sessionID,
			Text:            entry.result.Text,
			Remaining:       entry.result.Remaining,
			UnexpectedModel: entry.result.UnexpectedModel,
			SummarizedAt:    entry.resultAt,
			LastSeen:        entry.lastSeen,
			InputHash:       entry.inputHash,
			Summarizer:      entry.summarizer,
			Log:             storedLog{File: entry.resultLog.File, ModTime: entry.resultLog.ModTime, Size: entry.resultLog.Size},
		})
	}
	return func() {
		if err := store.save(gen, entries); err != nil {
			s.opts.Logf("task summaries: not saved: %v", err)
		}
	}
}
