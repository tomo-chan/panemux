package agent

import "fmt"

// The registered agent IDs.
const (
	Claude    ID = "claude"
	Codex     ID = "codex"
	Cursor    ID = "cursor"
	Gemini    ID = "gemini"
	GrokBuild ID = "grok-build"
	OpenCode  ID = "opencode"
)

// agmsgVerified is the agmsg release whose type.conf detect_proc keys the
// Board rules mirror (github.com/fujibee/agmsg,
// scripts/drivers/types/<type>/type.conf, checked 2026-08). agmsg does no
// process detection for antigravity, copilot or hermes (they declare
// detect=explicit), and agmsg-app is the desktop app's own user rather than a
// CLI, so none of them is registered: an operator on one of them joins agmsg
// by hand.
//
// The claude and codex exclude tokens are confirmed by agmsg's template.md,
// which names claude -p/--print and codex exec as non-interactive. agmsg
// documents no headless invocation for the other four; their empty lists mean
// "not documented", not "none exists".
const agmsgVerified = "agmsg v1.1.13"

// registered is the whole registry, in matching order: the first Board rule
// that matches a process names its type. Adding an agent means adding its
// Definition here; nothing registers itself.
func registered() []Definition {
	return []Definition{
		{
			ID: Claude, DisplayName: "Claude Code", Aliases: claudeAliases, AgmsgType: claudeCodeName,
			Capabilities: []Capability{Detect, Collect, Launch, Resume, Workdir, Summary, Board},
			Detection: Detection{
				Board: &ProcessRule{
					Patterns: []string{string(Claude), claudeCodeName, string(Claude) + "-*"}, ExcludeTokens: claudeHeadless,
					VerifiedWith: agmsgVerified, UnknownArgs: Accept,
				},
				Task: &TaskRule{Process: IsClaudeProcess, Interactive: IsInteractiveClaude, UnknownArgs: Accept},
			},
		},
		{
			ID: Codex, DisplayName: "Codex", AgmsgType: string(Codex),
			Capabilities: []Capability{Detect, Collect, Launch, Resume, Workdir, Summary, Board},
			Detection: Detection{
				Board: &ProcessRule{
					Patterns: []string{string(Codex), string(Codex) + "-*"}, ExcludeTokens: codexHeadless,
					VerifiedWith: agmsgVerified, UnknownArgs: Accept,
				},
				Task: &TaskRule{
					Process: IsCodexProgram, Interactive: IsInteractiveCodex,
					VerifiedWith: codexVerified, UnknownArgs: Accept,
				},
			},
		},
		boardOnly(Cursor, "Cursor", "cursor", "cursor-agent", "cursor-agent-*"),
		boardOnly(Gemini, "Gemini CLI", "gemini", "gemini", "gemini-*"),
		boardOnly(GrokBuild, "Grok Build", "grok-build", "grok", "grok-*"),
		boardOnly(OpenCode, "opencode", "opencode", "opencode", "opencode-*"),
	}
}

// boardOnly is an agent Agent Board detects and nothing else lists.
func boardOnly(id ID, displayName, agmsgType string, patterns ...string) Definition {
	return Definition{
		ID: id, DisplayName: displayName, AgmsgType: agmsgType,
		Capabilities: []Capability{Detect, Board},
		Detection: Detection{Board: &ProcessRule{
			Patterns: patterns, VerifiedWith: agmsgVerified, UnknownArgs: Accept,
		}},
	}
}

type registry struct {
	byID  map[ID]int
	names map[string]ID
	agmsg map[string]int
	defs  []Definition
}

// defaultRegistry is built when the package loads, so an invalid registration
// stops panemux at startup and fails every test that imports this package.
var defaultRegistry = newRegistry(registered())

func newRegistry(defs []Definition) *registry {
	if err := Validate(defs); err != nil {
		panic(fmt.Sprintf("agent registry: %v", err))
	}
	r := &registry{defs: defs, byID: map[ID]int{}, names: map[string]ID{}, agmsg: map[string]int{}}
	for i, d := range defs {
		r.byID[d.ID] = i
		r.names[string(d.ID)] = d.ID
		for _, alias := range d.Aliases {
			r.names[alias] = d.ID
		}
		if d.AgmsgType != "" {
			r.agmsg[d.AgmsgType] = i
		}
	}
	return r
}

// All returns every registered agent in registration order. The result is a
// copy the caller may change.
func All() []Definition {
	out := make([]Definition, len(defaultRegistry.defs))
	for i, d := range defaultRegistry.defs {
		out[i] = d.clone()
	}
	return out
}

// Lookup returns the agent registered under id. An alias is not an ID.
func Lookup(id ID) (Definition, bool) {
	i, ok := defaultRegistry.byID[id]
	if !ok {
		return Definition{}, false
	}
	return defaultRegistry.defs[i].clone(), true
}

// Resolve returns the ID that name refers to, by ID or alias, exactly.
func Resolve(name string) (ID, bool) {
	id, ok := defaultRegistry.names[name]
	return id, ok
}

// ByAgmsgType returns the agent whose agmsg type name is agmsgType.
func ByAgmsgType(agmsgType string) (Definition, bool) {
	i, ok := defaultRegistry.agmsg[agmsgType]
	if !ok {
		return Definition{}, false
	}
	return defaultRegistry.defs[i].clone(), true
}

// MatchBoardProcess returns the first agent whose Board rule matches command.
func MatchBoardProcess(command string) (Definition, bool) {
	return matchRule(command, func(d Detection) *ProcessRule { return d.Board })
}

// MatchInteractiveSession returns the agent whose session command is, by the
// Task rule the task board lists sessions with, among the agents that declare
// workdir: a pane finds its agent this way.
func MatchInteractiveSession(command string) (Definition, bool) {
	return defaultRegistry.matchInteractiveSession(command)
}

func (r *registry) matchInteractiveSession(command string) (Definition, bool) {
	for _, d := range r.defs {
		// As in match, the first agent whose program matches decides: a
		// headless run of it is no agent's session.
		if t := d.Detection.Task; t != nil && d.Has(Workdir) && t.Process(command) {
			if !t.IsInteractive(command) {
				return Definition{}, false
			}
			return d.clone(), true
		}
	}
	return Definition{}, false
}

func matchRule(command string, rule func(Detection) *ProcessRule) (Definition, bool) {
	return defaultRegistry.match(command, rule)
}

func (r *registry) match(command string, rule func(Detection) *ProcessRule) (Definition, bool) {
	for _, d := range r.defs {
		// The first agent whose program matches decides: an exclude token
		// makes the process no agent's, rather than a later agent's whose
		// patterns overlap.
		if pr := rule(d.Detection); pr != nil && pr.MatchBinary(command) {
			if !pr.Match(command) {
				return Definition{}, false
			}
			return d.clone(), true
		}
	}
	return Definition{}, false
}

// clone copies d deeply, so nothing a caller does to it reaches the registry.
func (d Definition) clone() Definition {
	d.Aliases = append([]string(nil), d.Aliases...)
	d.Capabilities = append([]Capability(nil), d.Capabilities...)
	d.Detection.Board = d.Detection.Board.clone()
	if d.Detection.Task != nil {
		task := *d.Detection.Task
		d.Detection.Task = &task
	}
	return d
}

func (r *ProcessRule) clone() *ProcessRule {
	if r == nil {
		return nil
	}
	c := *r
	c.Patterns = append([]string(nil), r.Patterns...)
	c.ExcludeTokens = append([]string(nil), r.ExcludeTokens...)
	return &c
}
