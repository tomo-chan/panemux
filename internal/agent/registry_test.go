package agent

import (
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
		Detection: Detection{Workdir: &ProcessRule{Patterns: []string{string(id)}, UnknownArgs: Accept}},
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
			d.Capabilities = []Capability{Detect, Workdir, Collect}
		})},
		"task rule undeclared": {modDef("a", func(d *Definition) {
			d.Detection.Task = &TaskRule{Process: anyProcess, UnknownArgs: Accept}
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
		return modDef("a", func(d *Definition) { d.Detection.Workdir.Patterns = patterns })
	}
	withTask := func(rule TaskRule) Definition {
		return modDef("a", func(d *Definition) {
			d.Capabilities = []Capability{Detect, Workdir, Collect}
			d.Detection.Task = &rule
		})
	}
	assertAllRejected(t, map[string][]Definition{
		"empty patterns":       {withPatterns()},
		"uppercase pattern":    {withPatterns("A")},
		"inner wildcard":       {withPatterns("a*b")},
		"bare wildcard":        {withPatterns("*")},
		"dash-only wildcard":   {withPatterns("-*")},
		"double-dash wildcard": {withPatterns("a--*")},
		"empty exclude token": {modDef("a", func(d *Definition) {
			d.Detection.Workdir.ExcludeTokens = []string{""}
		})},
		"unknown args policy":         {modDef("a", func(d *Definition) { d.Detection.Workdir.UnknownArgs = "maybe" })},
		"missing args policy":         {modDef("a", func(d *Definition) { d.Detection.Workdir.UnknownArgs = "" })},
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
	assert.NoError(t, Validate([]Definition{a, b, c}))
}

func TestNewRegistryPanicsOnInvalidDefinitions(t *testing.T) {
	assert.Panics(t, func() { newRegistry([]Definition{validDef("a"), validDef("a")}) })
}
