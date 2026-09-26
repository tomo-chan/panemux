package tasks

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Task summaries (issue #258): what a task is doing and what is left, made
// by `claude -p` on the panemux host from an excerpt of the task's
// conversation log.
//
// A summary is kept in memory, keyed by (host, session ID), together with
// the version of the log (modification time and size) it was made from. It
// is reused while the log keeps that version, so the dashboard's 10-second
// poll does not summarize again. Which tasks are summarized:
//
//   - A running task that is not working (wait, idle, unknown) is summarized
//     when a poll finds its log at a version not yet tried.
//   - A busy task is not: its log changes all the time. Its last summary is
//     shown, marked outdated.
//   - A stopped task is summarized when the dashboard asks (RequestSummary),
//     which it does when the task is selected.
//
// A failed attempt is not retried by the poll for the same log version;
// RequestSummary retries it. At most summaryConcurrency summaries run at once.

// SummaryState is where a task's summary stands.
type SummaryState string

// Summary states. Unreadable is a log in which no message was understood —
// a Claude Code release that changed the log's format — which is not sent
// to claude.
const (
	SummaryPending    SummaryState = "pending"
	SummaryReady      SummaryState = "ready"
	SummaryFailed     SummaryState = "error"
	SummaryUnreadable SummaryState = "unreadable"
)

// SummaryView is a task's summary as the API reports it. Text, Remaining and
// SummarizedAt are the last answer, when there is one, whatever State says;
// Outdated means the log has changed since that answer was made.
type SummaryView struct {
	SummarizedAt  *time.Time   `json:"summarized_at,omitempty"`
	State         SummaryState `json:"state"`
	Text          string       `json:"text,omitempty"`
	Error         string       `json:"error,omitempty"`
	Remaining     []string     `json:"remaining,omitempty"`
	Outdated      bool         `json:"outdated,omitempty"`
	DoneCandidate bool         `json:"done_candidate,omitempty"`
}

// ErrNoSummaryTask is a summary request for a session the host's last
// collection did not list with a conversation log.
var ErrNoSummaryTask = errors.New("no claude task with a conversation log for that session ID")

const (
	// summaryConcurrency bounds the summaries running at once, across hosts.
	summaryConcurrency = 2
	// defaultSummaryTimeout bounds one `claude -p` run. The runs measured
	// while this was built took 4.6 to 22 seconds.
	defaultSummaryTimeout = 2 * time.Minute
)

type summaryKey struct {
	host      string
	sessionID string
}

type summaryEntry struct {
	resultAt time.Time
	result   *Summary
	// triedLog is the log version of the last attempt that finished, and
	// failure and unreadable its outcome when it made no summary.
	failure    string
	resultLog  LogVersion
	triedLog   LogVersion
	unreadable bool
	running    bool
}

// summaryTask is what a summary needs to know of a listed task.
type summaryTask struct {
	host      string
	sessionID string
	log       LogVersion
}

// automaticSummaryStates are the states in which a running task is
// summarized without being asked.
var automaticSummaryStates = map[State]bool{StateWait: true, StateIdle: true, StateUnknown: true}

// rememberSummaryTasks records the tasks a host's collection listed with a
// log, which RequestSummary may summarize, and drops the summaries of tasks
// no longer listed.
func (s *Service) rememberSummaryTasks(host string, tasks []Task) {
	listed := map[string]summaryTask{}
	for _, task := range tasks {
		if task.Agent == AgentClaude && task.SessionID != "" && task.Log != nil {
			listed[task.SessionID] = summaryTask{host: host, sessionID: task.SessionID, log: *task.Log}
		}
	}
	s.summaryMu.Lock()
	defer s.summaryMu.Unlock()
	s.summaryTasks[host] = listed
	for key := range s.summaries {
		if _, ok := listed[key.sessionID]; key.host == host && !ok {
			delete(s.summaries, key)
		}
	}
}

// forgetSummaryHostsExcept drops what is known of hosts no longer configured.
func (s *Service) forgetSummaryHostsExcept(names []string) {
	keep := map[string]bool{"": true}
	for _, name := range names {
		keep[name] = true
	}
	s.summaryMu.Lock()
	defer s.summaryMu.Unlock()
	for host := range s.summaryTasks {
		if !keep[host] {
			delete(s.summaryTasks, host)
		}
	}
	for key := range s.summaries {
		if !keep[key.host] {
			delete(s.summaries, key)
		}
	}
}

// Summaries returns the summary of each of tasks that has one, by task ID,
// and starts the automatic ones that are due.
func (s *Service) Summaries(tasks []Task) map[string]*SummaryView {
	views := map[string]*SummaryView{}
	s.summaryMu.Lock()
	defer s.summaryMu.Unlock()
	for _, task := range tasks {
		if task.Agent != AgentClaude || task.SessionID == "" || task.Log == nil {
			continue
		}
		key := summaryKey{host: task.Host, sessionID: task.SessionID}
		if automaticSummaryStates[task.State] {
			s.startSummaryLocked(key, *task.Log, false)
		}
		if view := s.summaries[key].view(*task.Log); view != nil {
			views[task.ID] = view
		}
	}
	return views
}

// RequestSummary summarizes one listed session unless its summary for the
// current log is ready or running, retrying a failed one, and returns where
// its summary stands.
func (s *Service) RequestSummary(host, sessionID string) (*SummaryView, error) {
	if !validSessionID.MatchString(sessionID) {
		return nil, fmt.Errorf("%w: session ID %q", ErrInvalidSummary, sessionID)
	}
	s.summaryMu.Lock()
	defer s.summaryMu.Unlock()
	task, ok := s.summaryTasks[host][sessionID]
	if !ok {
		return nil, ErrNoSummaryTask
	}
	if s.summaryCtx.Err() != nil {
		return nil, errors.New("task summaries are closed")
	}
	key := summaryKey{host: host, sessionID: sessionID}
	s.startSummaryLocked(key, task.log, true)
	return s.summaries[key].view(task.log), nil
}

// startSummaryLocked starts a summary of key's log at version log unless one
// is running, or the last attempt was for this version — and, unless
// retryFailed, whatever its outcome. s.summaryMu must be held.
func (s *Service) startSummaryLocked(key summaryKey, log LogVersion, retryFailed bool) {
	if s.summaryCtx.Err() != nil {
		return
	}
	entry := s.summaries[key]
	if entry == nil {
		entry = &summaryEntry{}
		s.summaries[key] = entry
	} else {
		if entry.running {
			return
		}
		tried := entry.result != nil || entry.failure != "" || entry.unreadable
		if tried && entry.triedLog == log && (entry.failure == "" || !retryFailed) {
			return
		}
	}
	entry.running = true
	s.summaryWG.Add(1)
	go s.runSummary(key, log)
}

func (s *Service) runSummary(key summaryKey, log LogVersion) {
	defer s.summaryWG.Done()
	var (
		summary    Summary
		unreadable bool
		err        error
	)
	select {
	case s.summarySlots <- struct{}{}:
		summary, unreadable, err = s.summarize(key)
		<-s.summarySlots
	case <-s.summaryCtx.Done():
		err = errors.New("task summaries are closed")
	}

	s.summaryMu.Lock()
	defer s.summaryMu.Unlock()
	entry := s.summaries[key]
	if entry == nil {
		// Forgotten while it ran: the task left the list.
		return
	}
	entry.running = false
	entry.triedLog = log
	entry.failure = ""
	entry.unreadable = unreadable
	switch {
	case err != nil:
		entry.failure = err.Error()
	case !unreadable:
		entry.result = &summary
		entry.resultAt = s.opts.Now()
		entry.resultLog = log
	}
}

// summarize reads the task's log on its host and has claude summarize it.
func (s *Service) summarize(key summaryKey) (Summary, bool, error) {
	script, err := buildTranscriptScript(key.sessionID)
	if err != nil {
		return Summary{}, false, err
	}
	fetchCtx, cancel := context.WithTimeout(s.summaryCtx, s.opts.HostTimeout)
	out, err := s.runHostScript(fetchCtx, key.host, script, "conversation log read")
	cancel()
	if err != nil {
		return Summary{}, false, err
	}
	data, err := parseTranscriptOutput(out)
	if err != nil {
		return Summary{}, false, err
	}
	excerpt, ok := buildExcerpt(data)
	if !ok {
		return Summary{}, true, nil
	}
	summaryCtx, cancel := context.WithTimeout(s.summaryCtx, s.opts.SummaryTimeout)
	defer cancel()
	summary, err := s.opts.Summarize(summaryCtx, excerpt)
	if err != nil {
		return Summary{}, false, err
	}
	return summary, false, nil
}

// view is the entry as the API reports it for a task whose log is at
// version current. A nil entry is a task never summarized.
func (e *summaryEntry) view(current LogVersion) *SummaryView {
	if e == nil {
		return nil
	}
	v := &SummaryView{State: SummaryReady}
	switch {
	case e.running:
		v.State = SummaryPending
	case e.unreadable:
		v.State = SummaryUnreadable
	case e.failure != "":
		v.State = SummaryFailed
		v.Error = e.failure
	}
	if e.result != nil {
		at := e.resultAt
		v.Text = e.result.Text
		v.Remaining = e.result.Remaining
		v.SummarizedAt = &at
		v.Outdated = e.resultLog != current
		v.DoneCandidate = !v.Outdated && v.State == SummaryReady && len(e.result.Remaining) == 0
	}
	return v
}

// waitSummaries waits for every summary started so far. Tests use it.
func (s *Service) waitSummaries() {
	s.summaryWG.Wait()
}
