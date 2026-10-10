package tasks

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// costStep is one step of the measured update sequence and the agent calls
// it may make.
type costStep struct {
	name  string
	calls int
}

// TestSummaryCost_TheMeasuredSequence runs the update sequence issue #353
// measures over the synthetic conversation and counts the agent calls each
// step makes. With PANEMUX_SUMMARY_COST_DIR set, it writes each excerpt sent
// there, to be sent to the real CLI by hand.
func TestSummaryCost_TheMeasuredSequence(t *testing.T) {
	f := newPersistedSummaryFixture(t)
	setLog := func(status string, turns ...[]costTurn) {
		log := costLog(turns...)
		f.host.set(hostCollection(len(log), status), log)
	}
	svc := f.start(t)
	steps := []costStep{}
	step := func(name string, run func()) {
		before := f.summarizer.calls()
		run()
		steps = append(steps, costStep{name, f.summarizer.calls() - before})
	}
	poll := func() { collectAndSummarize(svc) }

	step("first summary", func() { setLog("idle", costConversation); poll() })
	step("poll, log unchanged", poll)
	for i := range costToolAppends {
		step("tool result appended while busy", func() {
			setLog("busy", append([][]costTurn{costConversation}, costToolAppends[:i+1]...)...)
			poll()
		})
		step("idle again after tool results only", func() {
			f.clock.advance(time.Minute)
			setLog("idle", append([][]costTurn{costConversation}, costToolAppends[:i+1]...)...)
			poll()
		})
	}
	all := append([][]costTurn{costConversation}, costToolAppends...)
	all = append(all, costTextAppend)
	step("conversation text appended", func() { setLog("idle", all...); poll() })
	step("restart", func() {
		svc.Close()
		svc.waitSummaries()
		svc = f.start(t)
		poll()
	})
	step("summary asked for", func() {
		_, err := svc.RequestSummary("", "s10")
		require.NoError(t, err)
		svc.waitSummaries()
	})

	total := 0
	for _, s := range steps {
		t.Logf("%-40s %d", s.name, s.calls)
		total += s.calls
	}
	for i, excerpt := range f.summarizer.excerpts {
		t.Logf("excerpt %d: %d bytes", i+1, len(excerpt))
	}
	assert.Equal(t, 2, total, "the first summary and the new conversation text")

	if dir := os.Getenv("PANEMUX_SUMMARY_COST_DIR"); dir != "" {
		for i, excerpt := range f.summarizer.excerpts {
			name := filepath.Join(dir, "excerpt-"+strconv.Itoa(i+1)+".txt")
			require.NoError(t, os.WriteFile(name, []byte(excerpt), 0o600))
		}
	}
}
