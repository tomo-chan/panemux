package tasks

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"panemux/internal/agent"
	"panemux/internal/fileops"
	"panemux/internal/homedir"
)

// The task summaries' file (issue #352): each summary's answer, kept on the
// panemux host so a restart does not summarize again. It holds what a card
// shows and what tells whether that answer is still current — the hash of the
// excerpt it was made from and the log version it was last checked against —
// and never the conversation or the excerpt themselves.
//
// The file is a cache: anything it holds can be made again. A file this
// build cannot use — not JSON, another format version, an entry it would
// not have written — is therefore moved aside to <name>.bad-<UTC time>,
// unchanged, and the store starts empty at once, rather than leaving
// summaries unsaved until someone repairs it. Until a load succeeds (the
// file could not be read or moved aside) nothing is written over it.

const (
	summaryStoreFileName = "task-summaries.json"
	// summaryStoreFileMode keeps the file private to the operator, like every
	// other file panemux writes under ~/.config/panemux.
	summaryStoreFileMode os.FileMode = 0600
	// summaryStoreFileVersion is the only format this build reads or writes.
	summaryStoreFileVersion = 1
	// summaryStoreBadSuffix and summaryStoreBadTime name a file moved aside.
	summaryStoreBadSuffix = ".bad-"
	summaryStoreBadTime   = "20060102T150405Z"
	// summaryStoreBadLimit is how many files can be moved aside in one
	// second before another is refused.
	summaryStoreBadLimit = 100
)

var validInputHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

// storedSummary is one summary as the file keeps it.
type storedSummary struct {
	SummarizedAt time.Time `json:"summarized_at"`
	// LastSeen is when the dashboard last listed the task, which decides
	// how long the summary is kept.
	LastSeen  time.Time `json:"last_seen"`
	Host      string    `json:"host"`
	Agent     string    `json:"agent"`
	SessionID string    `json:"session_id"`
	Text      string    `json:"text"`
	// InputHash is summaryInputHash of the excerpt the answer was made from,
	// and Summarizer the summarizerVersion that made it.
	InputHash  string   `json:"input_hash"`
	Summarizer string   `json:"summarizer"`
	Remaining  []string `json:"remaining"`
	// UnexpectedModel is Summary.UnexpectedModel.
	UnexpectedModel string    `json:"unexpected_model,omitempty"`
	Log             storedLog `json:"log"`
}

// storedLog is the log version the answer was last found current for.
type storedLog struct {
	File    string `json:"file,omitempty"`
	ModTime int64  `json:"mod_time"`
	Size    int64  `json:"size"`
}

func (s storedSummary) key() summaryKey {
	return summaryKey{host: s.Host, agent: s.Agent, sessionID: s.SessionID}
}

// maxStoredTextBytes and maxStoredItemBytes are the longest text and
// remaining item a summary can hold: truncateUTF8 adds its mark past the limit.
const (
	maxStoredTextBytes = maxSummaryTextBytes + len(truncationMark)
	maxStoredItemBytes = maxSummaryItemBytes + len(truncationMark)
)

// validate checks an entry as plan would have written it. The entry of an
// agent plan has no adapter for — one whose adapter was removed — is kept
// when its ID and session ID are well formed (issue #347).
func (s storedSummary) validate(plan summaryPlanned) error {
	_, adapted := plan.lookup(s.Agent)
	switch {
	case !agent.ID(s.Agent).Valid():
		return fmt.Errorf("invalid agent %q", s.Agent)
	case !validSessionID.MatchString(s.SessionID) || (adapted && !plan.summarizable(s.Agent, s.SessionID)):
		return fmt.Errorf("invalid session ID %q", s.SessionID)
	case !validInputHash.MatchString(s.InputHash):
		return errors.New("invalid input hash")
	case strings.TrimSpace(s.Text) == "":
		return errors.New("empty summary")
	case len(s.Text) > maxStoredTextBytes || len(s.Remaining) > maxSummaryRemaining:
		return errors.New("summary longer than a summary can be")
	case s.UnexpectedModel != "" && !validModelIDString(s.UnexpectedModel):
		return errors.New("invalid model")
	}
	for _, item := range s.Remaining {
		switch {
		case strings.TrimSpace(item) == "":
			return errors.New("empty remaining item")
		case len(item) > maxStoredItemBytes:
			return errors.New("remaining item longer than one can be")
		}
	}
	return nil
}

type summaryStoreFile struct {
	Summaries []storedSummary `json:"summaries"`
	Version   int             `json:"version"`
}

// SummaryStore reads and writes the task summaries' file. The Service reads
// it once, when summaries are first used, and from then on writes the
// summaries it holds in memory; an edit made while panemux runs is
// overwritten by the next save.
type SummaryStore struct {
	err  error
	path string
	// attempted is the generation of the last set a save was tried for,
	// whether or not it was written; an older one that arrives later is
	// dropped, so it cannot replace a newer set or clear the newer one's error.
	attempted uint64
	loaded    bool
	mu        sync.Mutex
}

// NewSummaryStore returns a store for the file at path, or for
// DefaultSummaryStorePath when path is "".
func NewSummaryStore(path string) *SummaryStore {
	return &SummaryStore{path: path}
}

// DefaultSummaryStorePath returns ~/.config/panemux/task-summaries.json.
func DefaultSummaryStorePath() (string, error) {
	home, err := homedir.Dir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".config", "panemux", summaryStoreFileName), nil
}

// Err is why summaries are not being saved: the file could not be loaded, or
// the last save failed. It is nil while they are.
func (s *SummaryStore) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// load reads the file's summaries. A file it cannot use is moved aside, and
// moved names where to.
func (s *SummaryStore) load(now time.Time, plan summaryPlanned) (entries []storedSummary, moved string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() { s.err = err }()
	if s.path == "" {
		if s.path, err = DefaultSummaryStorePath(); err != nil {
			return nil, "", err
		}
	}
	data, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		s.loaded = true
		return nil, "", nil
	case err != nil:
		return nil, "", fmt.Errorf("reading task summary file: %w", err)
	}
	entries, problem := parseSummaryStoreFile(data, plan)
	if problem != nil {
		if moved, err = moveAside(s.path, s.path+summaryStoreBadSuffix+now.UTC().Format(summaryStoreBadTime)); err != nil {
			return nil, "", fmt.Errorf("moving aside task summary file (%v): %w", problem, err)
		}
		entries = nil
	}
	s.loaded = true
	return entries, moved, nil
}

// moveAside renames path to base, or to base-1, base-2, ... when an earlier
// file moved aside in the same second holds that name, and returns the name
// used. A name it cannot check is tried, and the rename reports why not.
func moveAside(path, base string) (string, error) {
	target := base
	for n := 1; n <= summaryStoreBadLimit; n++ {
		if _, err := os.Lstat(target); err != nil {
			if err := os.Rename(path, target); err != nil {
				return "", fmt.Errorf("renaming to %s: %w", target, err)
			}
			return target, nil
		}
		target = fmt.Sprintf("%s-%d", base, n)
	}
	return "", fmt.Errorf("%d files already moved aside this second", summaryStoreBadLimit)
}

func parseSummaryStoreFile(data []byte, plan summaryPlanned) ([]storedSummary, error) {
	var file summaryStoreFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing: %w", err)
	}
	if file.Version != summaryStoreFileVersion {
		return nil, fmt.Errorf("unsupported version %d", file.Version)
	}
	seen := map[summaryKey]bool{}
	for _, entry := range file.Summaries {
		if err := entry.validate(plan); err != nil {
			return nil, err
		}
		if seen[entry.key()] {
			return nil, fmt.Errorf("duplicate summary for %s session %s", entry.Agent, entry.SessionID)
		}
		seen[entry.key()] = true
	}
	return file.Summaries, nil
}

// save writes entries as generation gen of the summaries, unless a later
// generation was written already.
func (s *SummaryStore) save(gen uint64, entries []storedSummary) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded {
		return errors.New("task summary file not loaded; not saving over it")
	}
	//mutation:exempt[CONDITIONALS_BOUNDARY] unreachable — each save gets a new, higher generation
	if gen < s.attempted {
		return nil
	}
	s.attempted = gen
	file := summaryStoreFile{Version: summaryStoreFileVersion, Summaries: slices.Clone(entries)}
	if file.Summaries == nil {
		file.Summaries = []storedSummary{}
	}
	slices.SortFunc(file.Summaries, func(a, b storedSummary) int {
		return cmp.Or(strings.Compare(a.Host, b.Host), strings.Compare(a.Agent, b.Agent),
			strings.Compare(a.SessionID, b.SessionID))
	})
	data, err := json.Marshal(file)
	//coverage:exempt a struct of strings, times and integers always marshals
	if err != nil {
		return fmt.Errorf("encoding task summary file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		s.err = fmt.Errorf("creating task summary directory: %w", err)
		return s.err
	}
	target := resolveRecordsWriteTarget(s.path)
	if err := fileops.AtomicWrite(target, data, summaryStoreFileMode, "task summary file"); err != nil {
		s.err = err
		return err
	}
	s.err = nil
	return nil
}
