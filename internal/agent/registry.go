package agent

import "fmt"

const boardDetectionVerified = "interactive process rules checked 2026-08"

// The registered agent IDs.
const (
	Claude    ID = "claude"
	Codex     ID = "codex"
	Cursor    ID = "cursor"
	Gemini    ID = "gemini"
	GrokBuild ID = "grok-build"
	OpenCode  ID = "opencode"
)

// registered is the whole registry, in matching order: the first Board rule
// that matches a process names its type. Adding an agent means adding its
// Definition to shipped; nothing registers itself. fixtureAgents is empty in
// every shipped build: only a build with the panemux_fixture_agent tag, which
// the tests and the E2E run use, registers the fixture agent (issue #377).
func registered() []Definition {
	return append(shipped(), fixtureAgents()...)
}

// shipped are the agents every build registers.
func shipped() []Definition {
	return []Definition{
		{
			ID: Claude, DisplayName: "Claude Code", Aliases: claudeAliases,
			Capabilities: []Capability{Detect, Collect, Launch, Resume, Workdir, Summary, Board},
			Detection: Detection{
				Board: &ProcessRule{
					Patterns: []string{string(Claude), claudeCodeName, string(Claude) + "-*"}, ExcludeTokens: claudeHeadless,
					VerifiedWith: boardDetectionVerified, UnknownArgs: Accept,
				},
				Task: &TaskRule{Process: IsClaudeProcess, Interactive: IsInteractiveClaude, UnknownArgs: Accept},
			},
		},
		{
			ID: Codex, DisplayName: "Codex",
			Capabilities: []Capability{Detect, Collect, Launch, Resume, Workdir, Summary, Board},
			Detection: Detection{
				Board: &ProcessRule{
					Patterns: []string{string(Codex), string(Codex) + "-*"}, ExcludeTokens: codexHeadless,
					VerifiedWith: boardDetectionVerified, UnknownArgs: Accept,
				},
				Task: &TaskRule{
					Process: IsCodexProgram, Interactive: IsInteractiveCodex,
					VerifiedWith: codexVerified, UnknownArgs: Accept,
				},
			},
		},
		boardOnly(Cursor, "Cursor", "cursor-agent", "cursor-agent-*"),
		boardOnly(Gemini, "Gemini CLI", "gemini", "gemini-*"),
		boardOnly(GrokBuild, "Grok Build", "grok", "grok-*"),
		boardOnly(OpenCode, "opencode", "opencode", "opencode-*"),
	}
}

// boardOnly is an agent Agent Board detects and nothing else lists.
func boardOnly(id ID, displayName string, patterns ...string) Definition {
	return Definition{
		ID: id, DisplayName: displayName,
		Capabilities: []Capability{Detect, Board},
		Detection: Detection{Board: &ProcessRule{
			Patterns: patterns, VerifiedWith: boardDetectionVerified, UnknownArgs: Accept,
		}},
	}
}

type registry struct {
	byID  map[ID]int
	names map[string]ID
	defs  []Definition
}

// defaultRegistry is built when the package loads, so an invalid registration
// stops panemux at startup and fails every test that imports this package.
var defaultRegistry = newRegistry(registered())

func newRegistry(defs []Definition) *registry {
	if err := Validate(defs); err != nil {
		panic(fmt.Sprintf("agent registry: %v", err))
	}
	r := &registry{defs: defs, byID: map[ID]int{}, names: map[string]ID{}}
	for i, d := range defs {
		r.byID[d.ID] = i
		r.names[string(d.ID)] = d.ID
		for _, alias := range d.Aliases {
			r.names[alias] = d.ID
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
