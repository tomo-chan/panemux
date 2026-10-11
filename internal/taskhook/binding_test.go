package taskhook

import (
	"context"
	"errors"
	"testing"
)

type bindingHost struct {
	processes map[int]ProcessIdentity
	files     map[int]NativeFiles
	scan      func(context.Context, HeldSession, Permission) (RolloutEvidence, error)
}

func (h *bindingHost) Process(_ context.Context, pid int) (ProcessIdentity, error) {
	p, ok := h.processes[pid]
	if !ok {
		return ProcessIdentity{}, ErrUnsupported
	}
	return p, nil
}
func (h *bindingHost) Files(_ context.Context, pid int, _ string, _ string) (NativeFiles, error) {
	return h.files[pid], nil
}
func (h *bindingHost) Scan(ctx context.Context, f HeldSession, p Permission) (RolloutEvidence, error) {
	if h.scan != nil {
		return h.scan(ctx, f, p)
	}
	return RolloutEvidence{Anchor: Anchor{FileID: f.FileID, Offset: 100}}, nil
}

func verifierFixture(t *testing.T) (*Verifier, *bindingHost, Registration) {
	t.Helper()
	s, root := newStore(t)
	entry := registration()
	entry.Owner.PID = 20
	entry.Owner.Birth = "boot1:20"
	r := NewRegistry(root)
	if err := r.Register(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	h := &bindingHost{processes: map[int]ProcessIdentity{
		100: {PID: 100, PPID: 42, UID: entry.Owner.UID, Birth: "boot1:100"},
		42:  {PID: 42, PPID: 20, UID: entry.Owner.UID, Birth: "boot1:42"},
		20:  {PID: 20, PPID: 1, UID: entry.Owner.UID, Birth: entry.Owner.Birth},
	}, files: map[int]NativeFiles{42: {Native: true, Sessions: []HeldSession{
		{SessionID: testSession, FileID: "file-a", Path: "/workspace/user/.codex/sessions/a.jsonl"},
		{SessionID: "00000000-0000-4000-8000-000000000002", FileID: "file-b",
			Path: "/workspace/user/.codex/sessions/b.jsonl"},
	}}}}
	return &Verifier{Registry: r, Store: s, Host: h}, h, entry
}

func TestBindingRequiresVerifiedAncestorAndUniqueExplicitSession(t *testing.T) {
	for _, name := range []string{
		"multiple sessions", "duplicate fd", "two matching identities", "detached", "birth reused",
		"uid mismatch", "cycle", "wrong executable", "no strict proof",
	} {
		t.Run(name, func(t *testing.T) {
			v, h, entry := verifierFixture(t)
			ok := false
			switch name {
			case "multiple sessions":
				ok = true
			case "duplicate fd":
				f := h.files[42]
				f.Sessions = append(f.Sessions, f.Sessions[0])
				h.files[42] = f
				ok = true
			case "two matching identities":
				f := h.files[42]
				f.Sessions = append(f.Sessions, HeldSession{SessionID: testSession, FileID: "file-c"})
				h.files[42] = f
			case "detached":
				p := h.processes[42]
				p.PPID = 1
				h.processes[42] = p
			case "birth reused":
				p := h.processes[20]
				p.Birth = "boot1:999"
				h.processes[20] = p
			case "uid mismatch":
				p := h.processes[42]
				p.UID++
				h.processes[42] = p
			case "cycle":
				p := h.processes[42]
				p.PPID = 100
				h.processes[42] = p
			case "wrong executable":
				f := h.files[42]
				f.Native = false
				h.files[42] = f
			case "no strict proof":
				entry.Scope.NoStrict = false
			}
			// Scope mutation models a different, unsupported launch registration.
			if name == "no strict proof" {
				entry.Owner.Generation = "g1-00000000000000000000000000000002"
				if err := v.Registry.Register(context.Background(), entry); err != nil {
					t.Fatal(err)
				}
			}
			proof, err := v.Identify(context.Background(), 100, entry.Owner.Generation, Permission{
				SessionID: testSession, TurnID: testTurn, ToolKind: ToolBash,
			})
			if ok && (err != nil || proof.Binding.Owner.PID != 42 || proof.File.FileID != "file-a") {
				t.Fatalf("matching session failed: %v", err)
			}
			if !ok && err == nil {
				t.Fatal("unverified binding accepted")
			}
		})
	}
}

func TestVerifierRechecksBeforePublishAndCancelsUnknownIngress(t *testing.T) {
	for _, name := range []string{"publish", "veto", "unknown scan", "file changed", "owner changed", "ack during scan"} {
		t.Run(name, func(t *testing.T) {
			v, h, entry := verifierFixture(t)
			p := Permission{SessionID: testSession, TurnID: testTurn, ToolKind: ToolBash}
			h.scan = func(_ context.Context, f HeldSession, _ Permission) (RolloutEvidence, error) {
				switch name {
				case "unknown scan":
					return RolloutEvidence{}, ErrUnsupported
				case "file changed":
					files := h.files[42]
					files.Sessions[0].FileID = "replacement"
					h.files[42] = files
				case "owner changed":
					proc := h.processes[42]
					proc.Birth = "boot1:999"
					h.processes[42] = proc
				case "ack during scan":
					r, err := v.Store.Read(testSession, Owner{
						Generation: entry.Owner.Generation, PID: 42, UID: entry.Owner.UID, Birth: "boot1:42",
					})
					if err != nil {
						t.Fatal(err)
					}
					if _, err = v.Store.MarkProvided(context.Background(), claim(r)); err != nil {
						t.Fatal(err)
					}
				}
				return RolloutEvidence{Anchor: Anchor{FileID: f.FileID, Offset: 100}, Veto: name == "veto"}, nil
			}
			if name == "ack during scan" {
				b := binding(testTurn)
				b.Owner = Owner{Generation: entry.Owner.Generation, PID: 42, UID: entry.Owner.UID, Birth: "boot1:42"}
				if _, err := v.Store.Append(context.Background(), b); err != nil {
					t.Fatal(err)
				}
			}
			r, err := v.Record(context.Background(), 100, entry.Owner.Generation, p)
			if name == "publish" && (err != nil || len(PendingIDs(r)) != 1) {
				t.Fatalf("eligible not published: %v", err)
			}
			if name == "veto" && (err != nil || len(PendingIDs(r)) != 0) {
				t.Fatalf("veto ignored: %v", err)
			}
			if name != "publish" && name != "veto" && err == nil {
				t.Fatal("race or unknown accepted")
			}
			got, readErr := v.Store.Read(testSession, Owner{
				Generation: entry.Owner.Generation, PID: 42, UID: entry.Owner.UID, Birth: "boot1:42",
			})
			if readErr != nil {
				t.Fatal(readErr)
			}
			if name != "publish" && (len(PendingIDs(got)) != 0 || len(ProvisionalIDs(got)) != 0) {
				t.Fatal("failed verifier leaked active or provisional slot")
			}
			if name == "ack during scan" && !errors.Is(err, ErrConflict) {
				t.Fatal("ACK race not detected", err)
			}
		})
	}
}
