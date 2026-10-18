package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The registered set is a compatibility contract: Agent Board's detected
// types, the task board's two agents, and their order (the first matching
// Board rule wins) must not move without a deliberate change here.
func TestRegisteredAgents(t *testing.T) {
	var ids []ID
	for _, d := range All() {
		ids = append(ids, d.ID)
	}
	assert.Equal(t, []ID{Claude, Codex, Cursor, Gemini, GrokBuild, OpenCode}, ids)

	want := map[ID]struct {
		agmsg string
		caps  []Capability
	}{
		Claude:    {"claude-code", []Capability{Detect, Collect, Launch, Resume, Workdir, Summary, Board}},
		Codex:     {"codex", []Capability{Detect, Collect, Launch, Resume, Workdir, Summary, Board}},
		Cursor:    {"cursor", []Capability{Detect, Board}},
		Gemini:    {"gemini", []Capability{Detect, Board}},
		GrokBuild: {"grok-build", []Capability{Detect, Board}},
		OpenCode:  {"opencode", []Capability{Detect, Board}},
	}
	for _, d := range All() {
		w := want[d.ID]
		assert.Equal(t, w.agmsg, d.AgmsgType, d.ID)
		assert.Equal(t, w.caps, d.Capabilities, d.ID)
		for _, c := range w.caps {
			assert.True(t, d.Has(c), "%s declares %s", d.ID, c)
		}
		assert.False(t, d.Has(Usage), "%s: no agent declares the usage capability yet (#345)", d.ID)
	}
}

func TestRegisteredDefinitionsAreValid(t *testing.T) {
	require.NoError(t, Validate(registered()))
}

func TestAllReturnsACopy(t *testing.T) {
	all := All()
	all[0].ID = "mutated"
	all[0].Capabilities[0] = Usage
	assert.Equal(t, Claude, All()[0].ID)
	assert.Equal(t, Detect, All()[0].Capabilities[0])
}

func TestLookupAndResolve(t *testing.T) {
	d, ok := Lookup(Claude)
	require.True(t, ok)
	assert.Equal(t, "Claude Code", d.DisplayName)
	_, ok = Lookup("claude-code")
	assert.False(t, ok, "an alias is not an ID")
	_, ok = Lookup("devin")
	assert.False(t, ok)

	for name, want := range map[string]ID{
		"claude": Claude, "claude-code": Claude, "codex": Codex, "grok-build": GrokBuild,
	} {
		got, ok := Resolve(name)
		assert.True(t, ok, name)
		assert.Equal(t, want, got, name)
	}
	for _, name := range []string{"", "Claude", "devin", "claude-*"} {
		_, ok := Resolve(name)
		assert.False(t, ok, name)
	}
}

func TestByAgmsgType(t *testing.T) {
	d, ok := ByAgmsgType("claude-code")
	require.True(t, ok)
	assert.Equal(t, Claude, d.ID)
	_, ok = ByAgmsgType("claude")
	assert.False(t, ok, "claude is not an agmsg type")
	_, ok = ByAgmsgType("")
	assert.False(t, ok)
}

func validDef(id ID) Definition {
	return Definition{
		ID: id, DisplayName: string(id), Capabilities: []Capability{Detect, Workdir},
		Detection: Detection{Task: &TaskRule{Process: named(string(id)), UnknownArgs: Accept}},
	}
}

func boardDef(id ID, agmsg string) Definition {
	d := validDef(id)
	d.AgmsgType = agmsg
	d.Capabilities = []Capability{Detect, Workdir, Board}
	d.Detection.Board = &ProcessRule{Patterns: []string{string(id)}, UnknownArgs: Accept}
	return d
}

func modDef(id ID, f func(*Definition)) Definition {
	d := validDef(id)
	f(&d)
	return d
}

func aliased(id ID, aliases ...string) Definition {
	return modDef(id, func(d *Definition) { d.Aliases = aliases })
}

func anyProcess(string) bool { return true }

// named matches a command whose program is exactly name.
func named(name string) func(string) bool {
	return func(command string) bool { return strings.Fields(command + " ")[0] == name }
}

func assertAllRejected(t *testing.T, cases map[string][]Definition) {
	t.Helper()
	for name, defs := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, Validate(defs))
		})
	}
}

func TestValidateRejectsNames(t *testing.T) {
	assertAllRejected(t, map[string][]Definition{
		"no definitions":         nil,
		"duplicate ID":           {validDef("a"), validDef("a")},
		"empty ID":               {validDef("")},
		"uppercase ID":           {validDef("Claude")},
		"ID with slash":          {validDef("a/b")},
		"ID starting with digit": {validDef("1a")},
		"ID with trailing dash":  {validDef("a-")},
		"empty display name":     {modDef("a", func(d *Definition) { d.DisplayName = "" })},
		"invalid alias":          {aliased("a", "A B")},
		"alias equals own ID":    {aliased("a", "a")},
		"alias equals other ID":  {validDef("a"), aliased("b", "a")},
		"alias collision":        {aliased("a", "x"), aliased("b", "x")},
		"ID equals other alias":  {aliased("a", "b"), validDef("b")},
		"duplicate agmsg type":   {boardDef("a", "t"), boardDef("b", "t")},
		"invalid agmsg type":     {boardDef("a", "T T")},
	})
}

func TestValidateRejectsCapabilities(t *testing.T) {
	assertAllRejected(t, map[string][]Definition{
		"unknown capability":   {modDef("a", func(d *Definition) { d.Capabilities = append(d.Capabilities, "teleport") })},
		"duplicate capability": {modDef("a", func(d *Definition) { d.Capabilities = append(d.Capabilities, Detect) })},
		"no capabilities":      {modDef("a", func(d *Definition) { d.Capabilities = nil })},
		"board without agmsg": {modDef("a", func(d *Definition) {
			d.Capabilities = []Capability{Detect, Workdir, Board}
			d.Detection.Board = &ProcessRule{Patterns: []string{"a"}, UnknownArgs: Accept}
		})},
		"board without rule": {modDef("a", func(d *Definition) {
			d.AgmsgType = "a"
			d.Capabilities = []Capability{Detect, Workdir, Board}
		})},
		"board rule undeclared": {modDef("a", func(d *Definition) {
			d.AgmsgType = "a"
			d.Detection.Board = &ProcessRule{Patterns: []string{"a"}, UnknownArgs: Accept}
		})},
		"agmsg type undeclared": {modDef("a", func(d *Definition) { d.AgmsgType = "a" })},
		"detect without rule":   {modDef("a", func(d *Definition) { d.Detection = Detection{} })},
		"rule without detect":   {modDef("a", func(d *Definition) { d.Capabilities = []Capability{Workdir, Launch} })},
		"collect without task": {modDef("a", func(d *Definition) {
			d.Capabilities = []Capability{Detect, Collect, Board}
			d.AgmsgType = "a"
			d.Detection = Detection{Board: &ProcessRule{Patterns: []string{"a"}, UnknownArgs: Accept}}
		})},
		"task rule undeclared": {modDef("a", func(d *Definition) {
			d.Capabilities = []Capability{Detect, Board}
			d.AgmsgType = "a"
			d.Detection.Board = &ProcessRule{Patterns: []string{"a"}, UnknownArgs: Accept}
		})},
		"workdir without rule": {modDef("a", func(d *Definition) {
			d.Capabilities = []Capability{Detect, Workdir, Board}
			d.AgmsgType = "a"
			d.Detection = Detection{Board: &ProcessRule{Patterns: []string{"a"}, UnknownArgs: Accept}}
		})},
	})
}

func TestValidateRejectsRules(t *testing.T) {
	withPatterns := func(patterns ...string) Definition {
		d := boardDef("a", "a")
		d.Detection.Board.Patterns = patterns
		return d
	}
	withBoard := func(f func(*ProcessRule)) Definition {
		d := boardDef("a", "a")
		f(d.Detection.Board)
		return d
	}
	withTask := func(rule TaskRule) Definition {
		return modDef("a", func(d *Definition) {
			d.Capabilities = []Capability{Detect, Workdir, Collect}
			d.Detection.Task = &rule
		})
	}
	assertAllRejected(t, map[string][]Definition{
		"empty patterns":              {withPatterns()},
		"uppercase pattern":           {withPatterns("A")},
		"inner wildcard":              {withPatterns("a*b")},
		"bare wildcard":               {withPatterns("*")},
		"dash-only wildcard":          {withPatterns("-*")},
		"double-dash wildcard":        {withPatterns("a--*")},
		"empty exclude token":         {withBoard(func(r *ProcessRule) { r.ExcludeTokens = []string{""} })},
		"unknown args policy":         {withBoard(func(r *ProcessRule) { r.UnknownArgs = "maybe" })},
		"missing args policy":         {withBoard(func(r *ProcessRule) { r.UnknownArgs = "" })},
		"task rule no process":        {withTask(TaskRule{UnknownArgs: Accept})},
		"unknown task policy":         {withTask(TaskRule{Process: anyProcess, UnknownArgs: "maybe"})},
		"interactive without process": {withTask(TaskRule{Interactive: anyProcess, UnknownArgs: Accept})},
	})
}

func TestValidateAccepts(t *testing.T) {
	a := aliased("a", "a-cli")
	b := boardDef("grok-build", "grok")
	b.Capabilities = append(b.Capabilities, Usage)
	b.Detection.Board.Patterns = []string{"grok", "grok-*"}
	b.Detection.Board.ExcludeTokens = []string{"-p"}
	c := validDef("c")
	c.Capabilities = []Capability{Detect, Collect, Workdir}
	c.Detection.Task = &TaskRule{Process: anyProcess, UnknownArgs: Reject}
	taskOnly := Definition{
		ID: "d", DisplayName: "d", Capabilities: []Capability{Detect, Collect},
		Detection: Detection{Task: &TaskRule{Process: anyProcess, UnknownArgs: Accept}},
	}
	assert.NoError(t, Validate([]Definition{a, b, c, taskOnly}))
}

func TestNewRegistryPanicsOnInvalidDefinitions(t *testing.T) {
	assert.Panics(t, func() { newRegistry([]Definition{validDef("a"), validDef("a")}) })
}

// A process whose program matches one agent's rule belongs to that agent:
// an exclude token makes it no agent's, rather than handing it to a later
// agent whose patterns overlap.
func TestMatchStopsAtFirstMatchingProgram(t *testing.T) {
	first := boardDef("a", "a")
	first.Detection.Board.Patterns = []string{"a-*"}
	first.Detection.Board.ExcludeTokens = []string{"-p"}
	second := boardDef("b", "b")
	second.Detection.Board.Patterns = []string{"a-*"}
	r := newRegistry([]Definition{first, second})

	_, ok := r.match("a-x -p", func(d Detection) *ProcessRule { return d.Board })
	assert.False(t, ok, "excluded by the first matching program's rule")
	d, ok := r.match("a-x", func(d Detection) *ProcessRule { return d.Board })
	assert.True(t, ok)
	assert.Equal(t, ID("a"), d.ID)
}

// The same holds for the Task rule a pane's session is found by: a headless
// run of the first agent whose program matches is no agent's session.
func TestMatchInteractiveSessionStopsAtFirstMatchingProgram(t *testing.T) {
	notC := func(command string) bool { return !named("c")(command) }
	first := validDef("a")
	first.Detection.Task.Process = notC
	first.Detection.Task.Interactive = func(command string) bool { return !strings.Contains(command, "-p") }
	second := validDef("b")
	second.Detection.Task.Process = notC
	taskOnly := validDef("c")
	taskOnly.Capabilities = []Capability{Detect, Collect}
	taskOnly.Detection.Task.Process = named("c")
	r := newRegistry([]Definition{taskOnly, first, second})

	_, ok := r.matchInteractiveSession("x -p")
	assert.False(t, ok, "headless by the first matching program's rule")
	d, ok := r.matchInteractiveSession("x")
	require.True(t, ok)
	assert.Equal(t, ID("a"), d.ID)
	_, ok = r.matchInteractiveSession("c")
	assert.False(t, ok, "an agent that declares no workdir has no pane session")
}

// What All and Lookup return shares nothing with the registry, rules
// included.
func TestDefinitionsAreDeepCopies(t *testing.T) {
	for _, d := range []Definition{All()[0], must(Lookup(Claude)), must(ByAgmsgType("claude-code"))} {
		d.Detection.Board.Patterns[0] = "mutated"
		d.Detection.Board.ExcludeTokens[0] = "mutated"
		d.Detection.Task.Process = func(string) bool { return false }
		d.Detection.Board = nil
	}
	d, ok := MatchBoardProcess("claude -p")
	assert.False(t, ok, "%+v", d)
	d, ok = MatchBoardProcess("claude")
	require.True(t, ok)
	assert.Equal(t, Claude, d.ID)
	_, ok = MatchInteractiveSession("claude")
	assert.True(t, ok)
	claude := must(Lookup(Claude))
	assert.True(t, claude.Detection.Task.Process("claude"))
}

func must(d Definition, ok bool) Definition {
	if !ok {
		panic("not registered")
	}
	return d
}
