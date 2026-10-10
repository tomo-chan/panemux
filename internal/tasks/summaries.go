package tasks

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Task summaries (issue #258): what a task is doing and what is left, made
// by its matching agent CLI on the panemux host from an excerpt of the task's
// conversation log.
//
// A summary is kept keyed by (host, agent, session ID), together with the
// version of the log (modification time and size) it was last found current
// for and the hash of the excerpt it was made from (issue #352). It is reused
// while the log keeps that version, so the dashboard's 10-second poll does
// not summarize again. When the log changes, a summary that is due reads the
// log again, and when the excerpt's hash is the one the answer was made from
// — a tool result appended, which the excerpt leaves out — the answer is
// current again without asking the agent. With a SummaryStore the answers are
// saved (summary_store.go) and restored after a restart, which by itself
// asks nothing. Which tasks are summarized:
//
//   - A running task that waits for input or is idle is summarized when a
//     poll finds its log at a version not yet tried.
//   - A busy task is not: its log changes all the time. Nor is one in an
//     unknown state, which may be working too. Its last summary is shown,
//     marked outdated.
//   - A stopped or unknown task is summarized when the dashboard asks
//     (RequestSummary), which it does when the task is selected.
//
// A failed attempt is not retried by the poll for the same log version;
// RequestSummary retries it. At most summaryConcurrency summaries run at once.
//
// A summary is kept while its task is listed and for summaryRetention after
// it was last, at most maxStoredSummaries in all, the least recently listed
// dropped first; a host removed from the config takes its summaries with it.

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
// Outdated means the log has changed since that answer was made — or, for
// an unreadable log or a failure, since that attempt.
type SummaryView struct {
	SummarizedAt *time.Time   `json:"summarized_at,omitempty"`
	State        SummaryState `json:"state"`
	Text         string       `json:"text,omitempty"`
	Error        string       `json:"error,omitempty"`
	// UnexpectedModel is the model that made the last answer when it was not
	// the one asked for (Summary.UnexpectedModel).
	UnexpectedModel string   `json:"unexpected_model,omitempty"`
	Remaining       []string `json:"remaining,omitempty"`
	Outdated        bool     `json:"outdated,omitempty"`
	DoneCandidate   bool     `json:"done_candidate,omitempty"`
}

// ErrNoSummaryTask is a summary request for a session the host's last
// collection did not list with a conversation log.
var ErrNoSummaryTask = errors.New("no task with a conversation log for that agent and session ID")

const (
	// summaryConcurrency bounds the summaries running at once, across hosts.
	summaryConcurrency = 2
	// defaultSummaryTimeout bounds one `claude -p` run. The runs measured
	// while this was built took 4.6 to 22 seconds.
	defaultSummaryTimeout = 2 * time.Minute
	// summaryRetention is how long a summary is kept after its task was
	// last listed, and maxStoredSummaries how many are kept in all.
	summaryRetention   = 30 * 24 * time.Hour
	maxStoredSummaries = 1000
	// summarySeenSaveInterval is how stale the saved time a task was last
	// listed may be, so that a poll does not rewrite the file each time.
	summarySeenSaveInterval = 24 * time.Hour
)

type summaryKey struct {
	agent     string
	host      string
	sessionID string
}

type summaryEntry struct {
	resultAt time.Time
	// lastSeen is when the task was last listed, and savedSeen the lastSeen
	// last handed to the store.
	lastSeen  time.Time
	savedSeen time.Time
	result    *Summary
	// inputHash and summarizer are summaryInputHash of the excerpt result
	// was made from, and the summarizerVersion that made it.
	inputHash  string
	summarizer string
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
// summarized without being asked. A task in an unknown state is not one of
// them: it may be working, its log changing at every poll, as a busy one's
// does.
var automaticSummaryStates = map[State]bool{StateWait: true, StateIdle: true}

// rememberSummaryTasks records the tasks a host's collection listed with a
// log, which RequestSummary may summarize. The summaries of tasks no longer
// listed are kept for summaryRetention.
func (s *Service) rememberSummaryTasks(host string, tasks []Task) {
	listed := map[summaryKey]summaryTask{}
	for _, task := range tasks {
		if (task.Agent == AgentClaude || task.Agent == AgentCodex) && task.SessionID != "" && task.Log != nil {
			key := summaryKey{host: host, agent: task.Agent, sessionID: task.SessionID}
			listed[key] = summaryTask{host: host, sessionID: task.SessionID, log: *task.Log}
		}
	}
	s.summaryMu.Lock()
	defer s.summaryMu.Unlock()
	s.summaryTasks[host] = listed
}

// forgetSummaryHostsExcept drops what is known of hosts no longer configured.
func (s *Service) forgetSummaryHostsExcept(names []string) {
	keep := map[string]bool{"": true}
	for _, name := range names {
		keep[name] = true
	}
	s.summaryMu.Lock()
	s.summaryHosts = keep
	for host := range s.summaryTasks {
		if !keep[host] {
			delete(s.summaryTasks, host)
		}
	}
	dropped := false
	for key, entry := range s.summaries {
		if !keep[key.host] {
			dropped = dropped || entry.result != nil
			delete(s.summaries, key)
		}
	}
	save := s.summarySaveLocked(dropped)
	s.summaryMu.Unlock()
	save()
}

// Summaries returns the summary of each of tasks that has one, by task ID,
// and starts the automatic ones that are due.
func (s *Service) Summaries(tasks []Task) map[string]*SummaryView {
	views := map[string]*SummaryView{}
	s.summaryMu.Lock()
	dirty := s.loadSummariesLocked()
	now := s.opts.Now()
	for _, task := range tasks {
		if (task.Agent != AgentClaude && task.Agent != AgentCodex) || task.SessionID == "" || task.Log == nil {
			continue
		}
		key := summaryKey{host: task.Host, agent: task.Agent, sessionID: task.SessionID}
		if automaticSummaryStates[task.State] {
			s.startSummaryLocked(key, *task.Log, false)
		}
		if entry := s.summaries[key]; entry != nil {
			entry.lastSeen = now
			dirty = dirty || (entry.result != nil && now.Sub(entry.savedSeen) >= summarySeenSaveInterval)
			views[task.ID] = entry.view(*task.Log)
		}
	}
	dirty = s.pruneSummariesLocked(now) || dirty
	save := s.summarySaveLocked(dirty)
	s.summaryMu.Unlock()
	save()
	return views
}

// RequestSummary summarizes one listed session unless its summary for the
// current log is ready or running, retrying a failed one, and returns where
// its summary stands.
func (s *Service) RequestSummary(host, sessionID string) (*SummaryView, error) {
	return s.RequestAgentSummary(host, AgentClaude, sessionID)
}

// RequestAgentSummary identifies a task by host, agent and session, preserving
// RequestSummary as the legacy Claude-only entry point.
func (s *Service) RequestAgentSummary(host, agent, sessionID string) (*SummaryView, error) {
	if (agent != AgentClaude && agent != AgentCodex) ||
		!validSessionID.MatchString(sessionID) || (agent == AgentCodex && !validUUID.MatchString(sessionID)) {

		return nil, fmt.Errorf("%w: session ID %q", ErrInvalidSummary, sessionID)
	}
	s.summaryMu.Lock()
	save := s.summarySaveLocked(s.loadSummariesLocked())
	defer func() {
		s.summaryMu.Unlock()
		save()
	}()
	key := summaryKey{host: host, agent: agent, sessionID: sessionID}
	task, ok := s.summaryTasks[host][key]
	if !ok {
		return nil, ErrNoSummaryTask
	}
	if s.summaryCtx.Err() != nil {
		return nil, errors.New("task summaries are closed")
	}
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
		entry = &summaryEntry{lastSeen: s.opts.Now()}
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
	reuse := ""
	if entry.result != nil {
		reuse = entry.inputHash
	}
	s.summaryWG.Add(1)
	go s.runSummary(key, log, reuse)
}

// summaryOutcome is what one attempt made of a log: an answer and the hash
// of the excerpt it is for, or none when the log was unreadable or the
// excerpt was the one the last answer was made from (reused).
type summaryOutcome struct {
	hash       string
	summary    Summary
	reused     bool
	unreadable bool
}

func (s *Service) runSummary(key summaryKey, log LogVersion, reuse string) {
	defer s.summaryWG.Done()
	var (
		outcome summaryOutcome
		err     error
	)
	select {
	case s.summarySlots <- struct{}{}:
		outcome, err = s.summarize(key, log, reuse)
		<-s.summarySlots
	case <-s.summaryCtx.Done():
		err = errors.New("task summaries are closed")
	}

	s.summaryMu.Lock()
	entry := s.summaries[key]
	if entry == nil {
		// Forgotten while it ran: its host left the config.
		s.summaryMu.Unlock()
		return
	}
	entry.running = false
	entry.triedLog = log
	entry.failure = ""
	entry.unreadable = outcome.unreadable
	changed := false
	switch {
	case err != nil:
		entry.failure = err.Error()
	case outcome.reused:
		entry.resultLog = log
		changed = true
	case !outcome.unreadable:
		entry.result = &outcome.summary
		entry.resultAt = s.opts.Now()
		entry.resultLog = log
		entry.inputHash = outcome.hash
		entry.summarizer = summarizerVersion(key.agent)
		changed = true
	}
	warn := s.unexpectedModelWarningLocked(outcome)
	save := s.summarySaveLocked(changed)
	s.summaryMu.Unlock()
	warn()
	save()
}

// summarize reads the task's log and uses the matching CLI on the panemux
// host, unless the excerpt's hash is reuse.
func (s *Service) summarize(key summaryKey, log LogVersion, reuse string) (summaryOutcome, error) {
	script, err := buildTranscriptScript(key.sessionID)
	if key.agent == AgentCodex {
		script, err = buildCodexTranscriptScriptForLog(key.sessionID, log)
	}
	if err != nil { //coverage:exempt keys come from collected sessions, which passed validSessionID when parsed
		return summaryOutcome{}, err
	}
	fetchCtx, cancel := context.WithTimeout(s.summaryCtx, s.opts.HostTimeout)
	out, err := s.runHostScript(fetchCtx, key.host, script, "conversation log read")
	cancel()
	if err != nil {
		return summaryOutcome{}, err
	}
	data, err := parseTranscriptOutput(out)
	if key.agent == AgentCodex {
		data, err = parseCodexTranscriptOutput(out, key.sessionID)
	}
	if err != nil {
		return summaryOutcome{}, err
	}
	excerpt, ok := buildExcerpt(data)
	if key.agent == AgentCodex {
		excerpt, ok = buildCodexExcerpt(data)
	}
	if !ok {
		return summaryOutcome{unreadable: true}, nil
	}
	hash := summaryInputHash(key.agent, excerpt)
	if hash == reuse {
		return summaryOutcome{hash: hash, reused: true}, nil
	}
	summaryCtx, cancel := context.WithTimeout(s.summaryCtx, s.opts.SummaryTimeout)
	defer cancel()
	runner := s.opts.Summarize
	if key.agent == AgentCodex {
		runner = s.opts.SummarizeCodex
	}
	summary, err := runner(summaryCtx, excerpt)
	if err != nil {
		return summaryOutcome{}, err
	}
	return summaryOutcome{summary: summary, hash: hash}, nil
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
		v.UnexpectedModel = e.result.UnexpectedModel
		v.SummarizedAt = &at
		v.Outdated = e.resultLog != current
		v.DoneCandidate = !v.Outdated && v.State == SummaryReady && len(e.result.Remaining) == 0
	}
	// A log that could not be read, or whose summary failed, has changed
	// since: asking again is worth it now.
	if (v.State == SummaryUnreadable || v.State == SummaryFailed) && e.triedLog != current {
		v.Outdated = true
	}
	return v
}

// waitSummaries waits for every summary started so far. Tests use it.
func (s *Service) waitSummaries() {
	s.summaryWG.Wait()
}

// unexpectedModelWarningLocked returns what logs, once for each model, that
// a new answer came from a model other than the one asked for. The line names
// the model and nothing that claude said.
func (s *Service) unexpectedModelWarningLocked(outcome summaryOutcome) func() {
	model := outcome.summary.UnexpectedModel
	if outcome.reused || outcome.unreadable || model == "" || s.warnedModels[model] {
		return func() {}
	}
	if s.warnedModels == nil {
		s.warnedModels = map[string]bool{}
	}
	s.warnedModels[model] = true
	return func() {
		s.opts.Logf("task summaries: claude answered with %s, not %s, which costs many times more; "+
			"the account's availableModels setting may not allow %s", model, summaryModel, summaryModel)
	}
}
