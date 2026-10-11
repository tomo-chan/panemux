package taskhook

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestCommandRegistersKernelParentWithUnknownScope(t *testing.T) {
	s, root := newStore(t)
	h := NewNativeHost(os.Geteuid())
	r := NewRegistry(root)
	c := &Command{Verifier: &Verifier{Registry: r, Store: s, Host: h}, PID: os.Getpid()}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := LaunchRegistration{SessionsRoot: root, ExecutablePath: executable,
		Scope: LaunchScope{CodexVersion: "unknown", NoDaemon: true}}
	data, _ := json.Marshal(p)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var out bytes.Buffer
	if err = c.Run(ctx, "register-launch", bytes.NewReader(data), &out); err != nil {
		t.Fatal(err)
	}
	entry, err := r.Read(out.String())
	if err != nil || entry.Owner.PID != os.Getppid() || entry.Owner.UID != os.Geteuid() || entry.Scope.Supported() {
		t.Fatal("payload invented parent identity or routing eligibility", err)
	}
	parent, err := h.Process(ctx, os.Getppid())
	if err != nil || entry.Owner.Birth != parent.Birth {
		t.Fatal("parent birth not high precision kernel identity")
	}
	_, err = c.Verifier.Identify(ctx, os.Getpid(), entry.Owner.Generation, Permission{
		SessionID: testSession, TurnID: testTurn, ToolKind: ToolBash,
	})
	if err == nil {
		t.Fatal("unknown launch scope became active")
	}
}
