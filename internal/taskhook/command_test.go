package taskhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type waitingInput struct{ release <-chan struct{} }

func (r waitingInput) Read([]byte) (int, error) { <-r.release; return 0, io.EOF }

func TestHookCommandIsSilentAndBoundedOnEveryFailure(t *testing.T) {
	v, _, entry := verifierFixture(t)
	c := &Command{Verifier: v, PID: 100, Generation: entry.Owner.Generation}
	for _, data := range []string{
		"{broken}", strings.Repeat(" ", MaxInputBytes+1), `{"hook_event_name":"PermissionRequest","private":"secret"}`,
	} {
		var out bytes.Buffer
		if err := c.Run(context.Background(), "record-permission", strings.NewReader(data), &out); err != nil ||
			out.Len() != 0 {

			t.Fatal("hook failure changed stdout or exit status", err)
		}
	}
	release := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	var out bytes.Buffer
	err := c.Run(ctx, "record-permission", waitingInput{release}, &out)
	close(release)
	if err != nil || out.Len() != 0 || time.Since(started) > time.Second {
		t.Fatal("blocked stdin held approval")
	}
}

func TestCommandInspectClaimRecoveryAndSelfcheck(t *testing.T) {
	v, _, entry := verifierFixture(t)
	c := &Command{Verifier: v, PID: 100, Generation: entry.Owner.Generation}
	ctx := context.Background()
	b := binding(testTurn)
	b.Owner = Owner{Generation: entry.Owner.Generation, UID: entry.Owner.UID, PID: 42, Birth: "boot1:42"}
	r, err := v.Store.Append(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	request, _ := json.Marshal(Lookup{SessionID: r.SessionID, Owner: r.Owner})
	var out bytes.Buffer
	if err = c.Run(ctx, "inspect", bytes.NewReader(request), &out); err != nil {
		t.Fatal(err)
	}
	read, err := DecodeRecord(out.Bytes())
	if err != nil || read.NoticeID != r.NoticeID {
		t.Fatal("inspect changed identity")
	}
	provided, _ := json.Marshal(claim(r))
	out.Reset()
	if err = c.Run(ctx, "mark-provided", bytes.NewReader(provided), &out); err != nil {
		t.Fatal(err)
	}
	if err = c.Run(ctx, "recover-provisional", bytes.NewReader(provided), &out); !errors.Is(err, ErrConflict) {
		t.Fatal("recovery accepted active IDs")
	}
	out.Reset()
	if err = c.Run(ctx, "selfcheck", strings.NewReader(""), &out); err != nil ||
		!strings.Contains(out.String(), `"protocol_version":1`) {

		t.Fatal("selfcheck lacks protocol capability", err)
	}
	if err = c.Run(ctx, "unknown", strings.NewReader(""), &out); !errors.Is(err, ErrUnsupported) {
		t.Fatal("unknown mode accepted")
	}
}
