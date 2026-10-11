package taskhook

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestCommandTerminalUsesFullRevisionAndFileEvidence(t *testing.T) {
	v, _, entry := verifierFixture(t)
	c := &Command{Verifier: v, PID: 100, Generation: entry.Owner.Generation}
	ctx := context.Background()
	b := binding(testTurn)
	r, err := v.Store.Append(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	p := Terminal{SessionID: r.SessionID, Owner: r.Owner, Revision: r.Revision,
		TurnID: testTurn, FileID: b.Anchor.FileID, Offset: 200}
	data, _ := json.Marshal(p)
	var out bytes.Buffer
	if err = c.Run(ctx, "resolve-turn", bytes.NewReader(data), &out); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeRecord(out.Bytes())
	if err != nil || len(PendingIDs(got)) != 0 {
		t.Fatal("same-turn terminal not resolved")
	}
	if err = c.Run(ctx, "resolve-turn", bytes.NewReader(data), &out); err == nil {
		t.Fatal("stale terminal revision accepted")
	}
}
