// Package agent is panemux's registry of coding agents: each agent's stable
// ID, display name, aliases, agmsg type name, the process rules that detect
// it, and the capabilities it declares. Every other package asks this one
// rather than naming an agent itself.
//
// The registry declares; it does not implement. The code behind each
// capability lives in the packages that use it. See docs/agent-registry.md.
//
// This package imports nothing else from panemux, so every package can import
// it.
package agent

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// ID is an agent's stable identifier: what records, task IDs and the API
// carry. It never changes once stored data names it.
type ID string

// validName is the shape of an ID, an alias and an agmsg type name: lower
// case, starting with a letter, words joined by single hyphens.
var validName = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// Valid reports whether id has the shape of an ID. It does not say whether
// the ID is registered: a stored record may name an agent this build does not
// know.
func (id ID) Valid() bool { return validName.MatchString(string(id)) }

// Capability is one thing panemux can do with an agent. Capabilities are
// independent: an agent declares any subset.
type Capability string

// The capabilities an agent can declare.
const (
	// Detect: its processes are recognized (Detection holds the rules).
	Detect Capability = "detect"
	// Collect: its sessions are listed on the task board, with their state.
	Collect Capability = "collect"
	// Launch: panemux can start a new session of it.
	Launch Capability = "launch"
	// Resume: panemux can reopen one of its sessions.
	Resume Capability = "resume"
	// Workdir: its working directory and conversation log can be resolved.
	Workdir Capability = "workdir"
	// Summary: its sessions can be summarized.
	Summary Capability = "summary"
	// Usage: its usage limits and spend can be read (#345; no agent yet).
	Usage Capability = "usage"
	// Board: it can join Agent Board through agmsg.
	Board Capability = "board"
)

var knownCapabilities = map[Capability]bool{
	Detect: true, Collect: true, Launch: true, Resume: true,
	Workdir: true, Summary: true, Usage: true, Board: true,
}

// UnknownPolicy is what a detection rule does with arguments it does not
// know — a flag or subcommand a newer release of the agent added.
type UnknownPolicy string

const (
	// Accept treats an unknown argument as an interactive session. A new
	// headless subcommand is then mistaken for a session until the rule
	// learns it.
	Accept UnknownPolicy = "accept"
	// Reject treats an unknown argument as not a session. A new interactive
	// mode is then missed until the rule learns it.
	Reject UnknownPolicy = "reject"
)

// Definition is one registered agent.
type Definition struct {
	ID          ID
	DisplayName string
	// AgmsgType is the agent's type name in agmsg, "" when agmsg has none.
	AgmsgType string
	Detection Detection
	// Aliases are other names that refer to this agent, such as the
	// "claude-code" path component of a Claude Code install.
	Aliases      []string
	Capabilities []Capability
}

// Has reports whether d declares c.
func (d Definition) Has(c Capability) bool {
	for _, have := range d.Capabilities {
		if have == c {
			return true
		}
	}
	return false
}

// Detection holds an agent's process rules: Agent Board's, which mirrors
// agmsg's own detection, and the Task rule the task board and the pane header
// share.
type Detection struct {
	// Board finds the agent in a pane for Agent Board, mirroring agmsg's own
	// type.conf detect_proc.
	Board *ProcessRule
	// Task finds the agent's processes in a host's `ps` output for the task
	// board, and the agent's session in a pane (MatchInteractiveSession).
	Task *TaskRule
}

// ProcessRule matches a `ps` command line by its program's base name.
type ProcessRule struct {
	// VerifiedWith names the release the rule was last checked against.
	VerifiedWith string
	UnknownArgs  UnknownPolicy
	// Patterns are lower-case base names; a trailing "*" matches any suffix.
	Patterns []string
	// ExcludeTokens are arguments that make the process a headless run
	// rather than a session.
	ExcludeTokens []string
}

// MatchBinary reports whether command's program matches one of the patterns,
// whatever its arguments.
func (r ProcessRule) MatchBinary(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	binary := strings.ToLower(filepath.Base(fields[0]))
	for _, p := range r.Patterns {
		if prefix, ok := strings.CutSuffix(p, "*"); ok {
			if strings.HasPrefix(binary, prefix) {
				return true
			}
		} else if binary == p {
			return true
		}
	}
	return false
}

// Match reports whether command's program matches and none of its arguments
// is an exclude token.
func (r ProcessRule) Match(command string) bool {
	if !r.MatchBinary(command) {
		return false
	}
	for _, arg := range strings.Fields(command)[1:] {
		for _, token := range r.ExcludeTokens {
			if arg == token {
				return false
			}
		}
	}
	return true
}

// TaskRule recognizes an agent's processes for the task board.
type TaskRule struct {
	// Process is any process of the agent, interactive or not.
	Process func(command string) bool
	// Interactive is a process of the agent that is a session; nil when
	// every process is.
	Interactive  func(command string) bool
	VerifiedWith string
	UnknownArgs  UnknownPolicy
}

// IsInteractive reports whether command is a process of the agent that is a
// session: Interactive's answer, or Process's when Interactive is nil.
func (r TaskRule) IsInteractive(command string) bool {
	if r.Interactive == nil {
		return r.Process(command)
	}
	return r.Interactive(command)
}

// SessionKey identifies one agent session: a session ID is unique only
// within its agent on its host. A pane, a PID and a tmux session are where a
// session is found, not what it is, so none of them is part of the key.
type SessionKey struct {
	// Host is the ssh_connections key, or "" for the panemux host.
	Host      string
	Agent     ID
	SessionID string
}

// Valid reports whether k identifies a session: its agent has the shape of an
// ID and its session ID is known. A session whose ID is not yet known has no
// key.
func (k SessionKey) Valid() bool { return k.Agent.Valid() && k.SessionID != "" }

// Validate checks that defs can form a registry: IDs, aliases and agmsg types
// are well formed and unique across all of them, capabilities are known and
// declared once, and every rule matches a declaration.
func Validate(defs []Definition) error {
	if len(defs) == 0 {
		return errors.New("agent: no definitions")
	}
	names := map[string]ID{}
	claim := func(name string, owner ID) error {
		if !validName.MatchString(name) {
			return fmt.Errorf("agent %q: invalid name %q", owner, name)
		}
		if other, taken := names[name]; taken {
			return fmt.Errorf("agent %q: name %q already names %q", owner, name, other)
		}
		names[name] = owner
		return nil
	}
	for _, d := range defs {
		if err := claim(string(d.ID), d.ID); err != nil {
			return err
		}
	}
	agmsgTypes := map[string]bool{}
	for _, d := range defs {
		for _, alias := range d.Aliases {
			if err := claim(alias, d.ID); err != nil {
				return err
			}
		}
		if d.AgmsgType != "" {
			if !validName.MatchString(d.AgmsgType) || agmsgTypes[d.AgmsgType] {
				return fmt.Errorf("agent %q: invalid or duplicate agmsg type %q", d.ID, d.AgmsgType)
			}
			agmsgTypes[d.AgmsgType] = true
		}
		if err := validateDefinition(d); err != nil {
			return fmt.Errorf("agent %q: %w", d.ID, err)
		}
	}
	return nil
}

func validateDefinition(d Definition) error {
	if d.DisplayName == "" {
		return errors.New("no display name")
	}
	if len(d.Capabilities) == 0 {
		return errors.New("no capabilities")
	}
	seen := map[Capability]bool{}
	for _, c := range d.Capabilities {
		if !knownCapabilities[c] || seen[c] {
			return fmt.Errorf("unknown or repeated capability %q", c)
		}
		seen[c] = true
	}
	det := d.Detection
	hasRule := det.Board != nil || det.Task != nil
	pairs := []struct {
		what     string
		declared bool
		present  bool
	}{
		{"detect needs a rule", seen[Detect], hasRule},
		{"board needs an agmsg type", seen[Board], d.AgmsgType != ""},
		{"board needs a board rule", seen[Board], det.Board != nil},
		{"a task rule needs collect or workdir", seen[Collect] || seen[Workdir], det.Task != nil},
	}
	for _, p := range pairs {
		if p.declared != p.present {
			return fmt.Errorf("declaration and rule disagree: %s", p.what)
		}
	}
	if det.Board != nil {
		if err := validateProcessRule(*det.Board); err != nil {
			return err
		}
	}
	if t := det.Task; t != nil {
		if t.Process == nil {
			return errors.New("task rule has no process predicate")
		}
		if err := validatePolicy(t.UnknownArgs); err != nil {
			return err
		}
	}
	return nil
}

func validateProcessRule(r ProcessRule) error {
	if len(r.Patterns) == 0 {
		return errors.New("process rule has no patterns")
	}
	for _, p := range r.Patterns {
		name, wildcard := strings.CutSuffix(p, "*")
		if wildcard {
			// A prefix may end where the next word starts: "claude-*".
			name = strings.TrimSuffix(name, "-")
		}
		if !validName.MatchString(name) {
			return fmt.Errorf("invalid pattern %q", p)
		}
	}
	for _, token := range r.ExcludeTokens {
		if token == "" {
			return errors.New("empty exclude token")
		}
	}
	return validatePolicy(r.UnknownArgs)
}

func validatePolicy(p UnknownPolicy) error {
	if p != Accept && p != Reject {
		return fmt.Errorf("unknown-arguments policy %q", p)
	}
	return nil
}
