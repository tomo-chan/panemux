package taskhook

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestManagedRegistrationRejectsTargetCollisionAndNeverAttestsScope(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := TargetIdentity{Version: 1, UID: os.Geteuid(), AccountHome: home, CodexHome: filepath.Join(home, ".codex"),
		Machine: "linux-machine-id:0123456789abcdef0123456789abcdef"}
	if err = os.MkdirAll(filepath.Join(target.CodexHome, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	paths, err := ContextPaths(target)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(paths.State, 0700); err != nil {
		t.Fatal(err)
	}
	target.Context, _ = ContextID(target)
	metadata := map[string]any{"target": target, "helper_protocol": ProtocolVersion,
		"scope": map[string]bool{"supported": true}}
	data, _ := json.Marshal(metadata)
	metaPath := filepath.Join(paths.Context, "install.json")
	if err = os.WriteFile(metaPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(paths.State)
	c := Command{PID: os.Getpid(), Managed: &ManagedLaunch{Target: target, Executable: executable},
		Verifier: &Verifier{Store: s, Registry: NewRegistry(paths.State), Host: NewNativeHost(os.Geteuid())}}
	var out bytes.Buffer
	if err = c.Run(context.Background(), "register-managed-launch", bytes.NewReader(nil), &out); err != nil {
		t.Fatal(err)
	}
	entry, err := c.Verifier.Registry.Read(out.String())
	if err != nil || entry.Scope.Supported() || entry.Scope.NoStrict ||
		entry.Scope.PassiveHooks || entry.Scope.UserReviewer {

		t.Fatal("install metadata promoted to routing evidence", err)
	}
	changed := target
	changed.Machine = "linux-machine-id:1123456789abcdef0123456789abcdef"
	metadata["target"] = changed
	data, _ = json.Marshal(metadata)
	if err = os.WriteFile(metaPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	err = c.Run(context.Background(), "register-managed-launch", bytes.NewReader(nil), &out)
	if err == nil || out.Len() != 0 {
		t.Fatal("colliding/mismatched target metadata accepted")
	}
}

func TestManagedArgsDoNotAcceptScopeClaims(t *testing.T) {
	args := []string{"--uid", strconv.Itoa(os.Geteuid()), "--account-home", "/workspace/account",
		"--codex-home", "/workspace/account/.codex",
		"--machine-identity", "linux-machine-id:0123456789abcdef0123456789abcdef",
		"--executable", "/workspace/bin/codex"}
	if _, err := ParseManagedArgs(args); err != nil {
		t.Fatal(err)
	}
	args[0] = "--scope"
	if _, err := ParseManagedArgs(args); err == nil {
		t.Fatal("scope claimed through launch argv")
	}
}
