package taskhook

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func registration() Registration {
	return Registration{Version: ProtocolVersion, Owner: testOwner(), SessionsRoot: "/workspace/user/.codex/sessions",
		ExecutableID: "binary-1", Scope: LaunchScope{CodexVersion: "0.160.0", NoDaemon: true, UserReviewer: true,
			NoStrict: true, PassiveHooks: true}}
}

func TestPrivateRegistryCannotRelabelOwnerOrScope(t *testing.T) {
	_, root := newStore(t)
	r := NewRegistry(root)
	ctx := context.Background()
	want := registration()
	if err := r.Register(ctx, want); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(ctx, want); err != nil {
		t.Fatal("identical retry failed", err)
	}
	got, err := NewRegistry(root).Read(want.Owner.Generation)
	if err != nil || got != want {
		t.Fatalf("registry not durable: %v", err)
	}
	for _, mutate := range []func(*Registration){
		func(p *Registration) { p.Owner.Birth = "boot1:999" },
		func(p *Registration) { p.Owner.PID++ },
		func(p *Registration) { p.SessionsRoot = "/workspace/other/sessions" },
		func(p *Registration) { p.Scope.PassiveHooks = false },
		func(p *Registration) { p.ExecutableID = "binary-2" },
	} {
		changed := want
		mutate(&changed)
		if err = r.Register(ctx, changed); !errors.Is(err, ErrConflict) {
			t.Fatal("registry relabeled", err)
		}
	}
	if _, err = r.Read("../outside"); !errors.Is(err, ErrInput) {
		t.Fatal("unsafe generation accepted")
	}
	name := filepath.Join(root, want.Owner.Generation+".owner.json")
	if err = os.WriteFile(name, []byte(`{"version":999}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = r.Register(ctx, want); !errors.Is(err, ErrCorrupt) {
		t.Fatal("corrupt owner silently replaced", err)
	}
}
