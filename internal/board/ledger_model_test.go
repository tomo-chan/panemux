package board

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// ledgerTransitionTable is Tier 1's half of issue #168's model-checking
// split: the state graph TLC exported from spec/agentboard/OwnSendLedger.tla,
// checked in so a hermetic `go test` run needs no JDK and no tla2tools.jar.
// `make model-check` regenerates it and fails when it has drifted from the
// spec.
const ledgerTransitionTable = "testdata/ownsendledger-transitions.json"

func (c ledgerCounts) String() string {
	return fmt.Sprintf("{expired:%d live:%d}", c.Expired, c.Live)
}

func (c ledgerCounts) held() int { return c.Expired + c.Live }

type ledgerTransition struct {
	Action string       `json:"action"`
	Result string       `json:"result"`
	From   ledgerCounts `json:"from"`
	To     ledgerCounts `json:"to"`
}

type ledgerTransitionTableFile struct {
	Spec        string             `json:"spec"`
	Config      string             `json:"config"`
	Observed    []string           `json:"observedVariables"`
	Actions     []string           `json:"actions"`
	States      []ledgerCounts     `json:"states"`
	Transitions []ledgerTransition `json:"transitions"`
	MaxHeld     int                `json:"maxHeld"`
}

// ledgerEdge is one lookup into the model: which states may a given ledger
// method, returning a given result, lead to from a given state.
type ledgerEdge struct {
	Method string
	Result string
	From   ledgerCounts
}

// ledgerModel is Tier 1's reference model. It is nothing but the exported
// transition table indexed for lookup — deliberately so. A second,
// hand-written Go state machine would be a third thing to keep in sync, and
// it would drift from the spec in exactly the way the implementation it is
// supposed to be checking might.
type ledgerModel struct {
	edges  map[ledgerEdge][]ledgerCounts
	expire map[ledgerCounts][]ledgerCounts
	states map[ledgerCounts]bool
	file   ledgerTransitionTableFile
}

// ledgerMethodOf maps a spec action onto the ownSendLedger method that
// performs it. The spec splits Consume and Forget into cases the Go methods
// do not report (Forget does not say which occurrence it removed), so the
// mapping is many-to-one — but it is total: loadLedgerModel fails on an
// action name this does not know, so a spec that grows a new action cannot
// silently go unchecked here.
func ledgerMethodOf(action string) (string, bool) {
	switch action {
	case "Record":
		return "Record", true
	case "Expire":
		return "Expire", true
	case "ConsumeHit", "ConsumeMiss":
		return "Consume", true
	case "ForgetLive", "ForgetExpired", "ForgetEmpty":
		return "Forget", true
	default:
		return "", false
	}
}

func loadLedgerModel(t *testing.T) *ledgerModel {
	t.Helper()

	raw, err := os.ReadFile(ledgerTransitionTable)
	require.NoError(t, err,
		"%s is missing; regenerate it with `TLA_TOOLS_JAR=... make model-check-write`",
		ledgerTransitionTable)

	var file ledgerTransitionTableFile
	require.NoError(t, json.Unmarshal(raw, &file), "%s is not valid JSON", ledgerTransitionTable)
	require.NotEmpty(t, file.Transitions, "%s holds no transitions", ledgerTransitionTable)
	require.Equal(t, []string{"expired", "live"}, file.Observed,
		"%s was exported over different variables than this model reads", ledgerTransitionTable)
	require.Positive(t, file.MaxHeld, "%s declares no bound", ledgerTransitionTable)

	m := &ledgerModel{
		edges:  make(map[ledgerEdge][]ledgerCounts),
		expire: make(map[ledgerCounts][]ledgerCounts),
		states: make(map[ledgerCounts]bool, len(file.States)),
		file:   file,
	}
	for _, action := range file.Actions {
		_, ok := ledgerMethodOf(action)
		require.True(t, ok,
			"%s declares action %q, which ledgerMethodOf does not map to an ownSendLedger method; "+
				"the spec and this model have drifted", ledgerTransitionTable, action)
	}
	for _, state := range file.States {
		m.states[state] = true
	}
	for _, tr := range file.Transitions {
		method, ok := ledgerMethodOf(tr.Action)
		require.True(t, ok, "unmapped action %q in %s", tr.Action, ledgerTransitionTable)
		if method == "Expire" {
			m.expire[tr.From] = append(m.expire[tr.From], tr.To)
			continue
		}
		edge := ledgerEdge{From: tr.From, Method: method, Result: tr.Result}
		m.edges[edge] = append(m.edges[edge], tr.To)
	}
	return m
}

func (m *ledgerModel) knows(c ledgerCounts) bool { return m.states[c] }

// successors returns every state the model allows method (returning result)
// to reach from `from`, and whether the model describes that combination at
// all. An unknown combination is a stronger finding than a wrong target: it
// means the implementation did something the spec has no case for.
func (m *ledgerModel) successors(from ledgerCounts, method, result string) ([]ledgerCounts, bool) {
	to, ok := m.edges[ledgerEdge{From: from, Method: method, Result: result}]
	return to, ok
}

func (m *ledgerModel) allows(from ledgerCounts, method, result string, to ledgerCounts) bool {
	targets, ok := m.successors(from, method, result)
	if !ok {
		return false
	}
	for _, candidate := range targets {
		if candidate == to {
			return true
		}
	}
	return false
}

// expireReachable reports whether `to` is reachable from `from` by zero or
// more Expire steps. Expire is the spec's model of the clock advancing
// between two ledger calls — it is not a ledger method, so a trace never
// names it and the checker has to allow for any number of them having
// happened since the previous step.
func (m *ledgerModel) expireReachable(from, to ledgerCounts) bool {
	if from == to {
		return true
	}
	seen := map[ledgerCounts]bool{from: true}
	queue := []ledgerCounts{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range m.expire[cur] {
			if next == to {
				return true
			}
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	return false
}

// nonExpireEdges is every (from, method, result, to) the model allows,
// excluding Expire. It is what the conformance drivers measure their own
// coverage against: "the implementation never contradicted the model" is a
// much weaker claim than "the implementation matched the model on every
// transition the model has", and only the second one rules out a table so
// permissive it would accept anything.
func (m *ledgerModel) nonExpireEdges() map[ledgerTransition]bool {
	edges := make(map[ledgerTransition]bool, len(m.file.Transitions))
	for _, tr := range m.file.Transitions {
		method, _ := ledgerMethodOf(tr.Action)
		if method == "Expire" {
			continue
		}
		edges[ledgerTransition{Action: method, Result: tr.Result, From: tr.From, To: tr.To}] = true
	}
	return edges
}

func TestLedgerTransitionTableIsWellFormed(t *testing.T) {
	m := loadLedgerModel(t)

	assert := require.New(t)
	assert.Equal("spec/agentboard/OwnSendLedger.tla", m.file.Spec)
	assert.NotEmpty(m.states)

	for _, tr := range m.file.Transitions {
		assert.True(m.knows(tr.From), "transition leaves undeclared state %s", tr.From)
		assert.True(m.knows(tr.To), "transition enters undeclared state %s", tr.To)
		assert.LessOrEqual(tr.From.held(), m.file.MaxHeld, "state %s exceeds maxHeld", tr.From)
		assert.LessOrEqual(tr.To.held(), m.file.MaxHeld, "state %s exceeds maxHeld", tr.To)
	}

	// Every state must offer both Consume outcomes' preconditions somewhere
	// and a Forget, or the table is too thin for the drivers below to mean
	// anything. Record is the one exception: it is unavailable at the bound.
	for state := range m.states {
		_, hasConsume := m.successors(state, "Consume", consumeResultFor(state))
		assert.True(hasConsume, "no Consume transition from %s", state)
		_, hasForget := m.successors(state, "Forget", ledgerNoResult)
		assert.True(hasForget, "no Forget transition from %s", state)
		_, hasRecord := m.successors(state, "Record", ledgerNoResult)
		assert.Equal(state.held() < m.file.MaxHeld, hasRecord,
			"Record should be available from %s exactly below the bound", state)
	}
}

// consumeResultFor is the result the spec says a Consume out of `state`
// must produce. It exists only so the well-formedness test above can ask for
// the right edge; the conformance drivers never use it — they compare the
// implementation's own return value against the model.
func consumeResultFor(state ledgerCounts) string {
	if state.Live > 0 {
		return ledgerResultTrue
	}
	return ledgerResultFalse
}
