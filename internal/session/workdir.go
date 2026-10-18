package session

import (
	"iter"
	"path/filepath"
	"regexp"

	"panemux/internal/agent"
)

// WorkdirSession is one agent session whose working directories are wanted:
// the agent's own process a pane found, or a task the task board listed.
type WorkdirSession struct {
	Key agent.SessionKey
	// CWD is the working directory the session recorded for itself, "" when
	// unknown.
	CWD string
	// Log is the session's conversation log on its host, "" when unknown.
	Log string
	// PID is the agent's process.
	PID int
}

// workdirHost is where a session runs, the panemux host or an SSH host. It
// fetches — files, open files, a log's working directory through the read
// cache — and leaves what the result means to the adapters.
type workdirHost interface {
	// hostKey is the SessionKey.Host of the sessions found here.
	hostKey() string
	// claudeStateFiles are the ~/.claude/sessions files that may be pid's.
	claudeStateFiles(pid int) ([]claudeStateFile, error)
	// claudeProjectDirs are the ~/.claude/projects directories that may hold
	// a session's transcripts, best first. A later one is worked out only
	// when the caller asks for it.
	claudeProjectDirs(sessionID, cwd string) iter.Seq[string]
	// claudeSubagentLogs are the transcript names under dir, sorted.
	claudeSubagentLogs(dir string) ([]string, error)
	claudeLogCWD(path string) (string, error)
	agentOpenFiles(pid int) ([]string, error)
	codexLogCWD(path string) (string, error)
}

// workdirAdapter resolves one agent's sessions: locate finds the session an
// agent process runs, and workdirs lists every working directory it is in,
// its subagents' included.
type workdirAdapter interface {
	locate(h workdirHost, pid int) (WorkdirSession, bool, error)
	workdirs(h workdirHost, s WorkdirSession) ([]string, error)
}

// workdirAdapters are the adapters by agent, one for each agent that
// declares workdir (TestWorkdirAdaptersMatchDeclarations).
func workdirAdapters() map[agent.ID]workdirAdapter {
	return map[agent.ID]workdirAdapter{
		agent.Claude: claudeWorkdir{},
		agent.Codex:  codexWorkdir{},
	}
}

// resolveSessionWorkdirs is the working directories of the session id's
// process pid runs, none when the agent has no adapter or the session is
// not found.
func resolveSessionWorkdirs(h workdirHost, id agent.ID, pid int) ([]string, error) {
	adapter, ok := workdirAdapters()[id]
	if !ok {
		return nil, nil
	}
	s, ok, err := adapter.locate(h, pid)
	if err != nil || !ok {
		return nil, err
	}
	return adapter.workdirs(h, s)
}

type claudeWorkdir struct{}

// locate reads the state file Claude Code keeps for the process.
func (claudeWorkdir) locate(h workdirHost, pid int) (WorkdirSession, bool, error) {
	files, err := h.claudeStateFiles(pid)
	if err != nil {
		return WorkdirSession{}, false, err
	}
	for _, file := range files {
		state, problem, _, mistyped := parseClaudeState(file.data)
		// The first file whose pid is the process's decides, usable or not,
		// unless its cwd or sessionId is of another type: Go's struct
		// decoding passed over such a file before the parsers were merged.
		if problem == ClaudeStateNotJSON || problem == ClaudeStateInvalidPID || mistyped || state.PID != pid {
			continue
		}
		if problem != "" || state.CWD == "" {
			return WorkdirSession{}, false, nil
		}
		return WorkdirSession{
			Key: agent.SessionKey{Host: h.hostKey(), Agent: agent.Claude, SessionID: state.SessionID},
			PID: pid, CWD: state.CWD,
		}, true, nil
	}
	return WorkdirSession{}, false, nil
}

// workdirs reads the session's transcript and every Task subagent transcript
// beside it. Claude Code records delegated subagent activity in separate
// per-agent transcripts under a "subagents" directory next to the parent
// transcript; a subagent that works in a worktree never touches the parent's
// recorded cwd, so the pane header would otherwise fall back to the base
// working directory (see docs/behavior.md "Pane Git and PR metadata").
func (claudeWorkdir) workdirs(h workdirHost, s WorkdirSession) ([]string, error) {
	sessionID := s.Key.SessionID
	if sessionID == "" || s.CWD == "" || !validClaudeSessionID.MatchString(sessionID) {
		return nil, nil
	}
	var cwds []string
	for dir := range h.claudeProjectDirs(sessionID, s.CWD) {
		cwds = claudeTranscriptCWDsUnder(h, dir, sessionID)
		if len(cwds) > 0 {
			break
		}
	}
	return cwds, nil
}

func claudeTranscriptCWDsUnder(h workdirHost, dir, sessionID string) []string {
	var cwds []string
	seen := map[string]bool{}
	add := func(path string) {
		cwd, err := h.claudeLogCWD(path)
		if err != nil || cwd == "" || seen[cwd] {
			return
		}
		seen[cwd] = true
		cwds = append(cwds, cwd)
	}
	add(filepath.Join(dir, sessionID+".jsonl"))
	subagents := filepath.Join(dir, sessionID, "subagents")
	names, err := h.claudeSubagentLogs(subagents)
	if err != nil {
		return cwds
	}
	for _, name := range names {
		add(filepath.Join(subagents, name))
	}
	return cwds
}

type codexWorkdir struct{}

// codexRolloutSessionID is the session ID a rollout's name ends in.
var codexRolloutSessionID = regexp.MustCompile(
	`-([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})\.jsonl$`,
)

// locate finds the rollout the process has open.
func (codexWorkdir) locate(h workdirHost, pid int) (WorkdirSession, bool, error) {
	paths, err := h.agentOpenFiles(pid)
	if err != nil {
		return WorkdirSession{}, false, err
	}
	rollout, ok := codexSessionPath(paths)
	if !ok {
		return WorkdirSession{}, false, nil
	}
	s := WorkdirSession{Key: agent.SessionKey{Host: h.hostKey(), Agent: agent.Codex}, PID: pid, Log: rollout}
	if m := codexRolloutSessionID.FindStringSubmatch(filepath.Base(rollout)); m != nil {
		s.Key.SessionID = m[1]
	}
	return s, true, nil
}

// workdirs reads the rollout's working directory, finding the rollout by the
// process when the session does not name it.
func (c codexWorkdir) workdirs(h workdirHost, s WorkdirSession) ([]string, error) {
	if s.Log == "" {
		located, ok, err := c.locate(h, s.PID)
		if err != nil || !ok {
			return nil, err
		}
		s.Log = located.Log
	}
	cwd, err := h.codexLogCWD(s.Log)
	if err != nil || cwd == "" {
		return nil, err
	}
	return []string{cwd}, nil
}
