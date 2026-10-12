package taskhook

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestControlFailuresEmitNoPrivateInputOrSuccess(t *testing.T) {
	v, _, _ := verifierFixture(t)
	c := Command{Verifier: v, PID: 100}
	for _, mode := range []string{"inspect", providedMode, recoveryMode, resolveMode, "register-launch"} {
		for _, input := range []io.Reader{brokenReader{}, strings.NewReader("private-body")} {
			var out bytes.Buffer
			require.Error(t, c.run(context.Background(), mode, input, &out))
			require.Empty(t, out.String())
		}
	}
	_, err := v.Store.Append(context.Background(), binding(testTurn))
	require.NoError(t, err)
	otherOwner := testOwner()
	otherOwner.Birth = "other"
	data, err := json.Marshal(Lookup{SessionID: testSession, Owner: otherOwner})
	require.NoError(t, err)
	var out bytes.Buffer
	require.ErrorIs(t, c.run(context.Background(), "inspect", bytes.NewReader(data), &out), ErrConflict)
	for _, mode := range []string{providedMode, recoveryMode, resolveMode} {
		require.Error(t, c.run(context.Background(), mode, strings.NewReader(`{}`), &out))
	}
	require.ErrorIs(t, c.run(context.Background(), "record-permission", brokenReader{}, &out), ErrInput)
	require.Error(t, emitRecord(&out, Record{}))
	require.Empty(t, out.String())
	require.ErrorIs(t, emitRecord(brokenWriter{}, start(t)), ErrStorage)
	require.ErrorIs(t, c.selfcheck(context.Background(), brokenWriter{}), ErrStorage)
	c.PID = 1234567
	require.NoError(t, c.selfcheck(context.Background(), &out))
	require.Contains(t, out.String(), "unavailable")
	var lookup Lookup
	require.Error(t, readCommand(strings.NewReader(strings.Repeat(" ", MaxInputBytes)+"{}"), &lookup))
}

func TestLaunchRegistrationFailuresNeverReturnGeneration(t *testing.T) {
	for _, stage := range []string{"invalid-path", "long-path", "missing-self", "missing-parent", "foreign-parent",
		"missing-root", "unsafe-root", "missing-executable", "unsafe-executable", "random", "registry", "output"} {
		t.Run(stage, func(t *testing.T) {
			s, root := newStore(t)
			registry := NewRegistry(root)
			home, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			require.NoError(t, os.Chmod(home, 0700))
			executable := filepath.Join(home, "helper")
			require.NoError(t, os.WriteFile(executable, []byte("fixture"), 0600))
			uid := os.Geteuid()
			host := &bindingHost{processes: map[int]ProcessIdentity{100: {PID: 100, PPID: 42, UID: uid, Birth: "child"},
				42: {PID: 42, PPID: 1, UID: uid, Birth: "parent"}}}
			c := Command{Verifier: &Verifier{Store: s, Registry: registry, Host: host}, PID: 100}
			p := LaunchRegistration{SessionsRoot: home, ExecutablePath: executable,
				Scope: LaunchScope{CodexVersion: UnknownScopeVersion}}
			switch stage {
			case "invalid-path":
				p.SessionsRoot = "relative"
			case "long-path":
				p.ExecutablePath = "/" + strings.Repeat("x", 4096)
			case "missing-self":
				delete(host.processes, 100)
			case "missing-parent":
				delete(host.processes, 42)
			case "foreign-parent":
				host.processes[42] = ProcessIdentity{PID: 42, UID: uid + 1, Birth: "parent"}
			case "missing-root":
				p.SessionsRoot = filepath.Join(home, "missing")
			case "unsafe-root":
				require.NoError(t, os.Chmod(home, 0777))
			case "missing-executable":
				p.ExecutablePath = filepath.Join(home, "missing")
			case "unsafe-executable":
				require.NoError(t, os.Chmod(executable, 0777))
			case "random":
				s.random = bytes.NewReader(nil)
			case "registry":
				registry.store.root = filepath.Join(home, "missing")
			}
			data, err := json.Marshal(p)
			require.NoError(t, err)
			var out bytes.Buffer
			output := io.Writer(&out)
			if stage == "output" {
				output = brokenWriter{}
			}
			require.Error(t, c.register(context.Background(), bytes.NewReader(data), output))
			require.Empty(t, out.String())
		})
	}
}

func TestManagedArgumentUIDAndDuplicatesCannotClaimOwnership(t *testing.T) {
	args := []string{accountHomeFlag, "/workspace/account", codexHomeFlag, "/workspace/account/.codex",
		machineFlag, "linux-machine-id:0123456789abcdef0123456789abcdef", uidFlag, strconv.Itoa(os.Geteuid()),
		executableFlag, "/workspace/bin/codex"}
	for _, uid := range []string{"-1", "01", "not-number"} {
		bad := append([]string(nil), args...)
		bad[7] = uid
		_, err := ParseManagedArgs(bad)
		require.Error(t, err)
	}
	bad := append([]string(nil), args...)
	bad[8] = uidFlag
	_, err := ParseManagedArgs(bad)
	require.Error(t, err)
	v, _, _ := verifierFixture(t)
	c := Command{Verifier: v}
	require.ErrorIs(t, c.registerManaged(context.Background(), io.Discard), ErrInput)
	c.Managed = &ManagedLaunch{Target: TargetIdentity{UID: os.Geteuid() + 1}}
	require.ErrorIs(t, c.registerManaged(context.Background(), io.Discard), ErrInput)
}

func TestCommandInputLimitIncludesFinalJSONDelimiter(t *testing.T) {
	var value map[string]any
	exact := "{" + strings.Repeat(" ", MaxInputBytes-2) + "}"
	require.NoError(t, readCommand(strings.NewReader(exact), &value))
	require.Error(t, readCommand(strings.NewReader(exact+" "), &value))
	v, _, entry := verifierFixture(t)
	c := Command{Verifier: v, PID: 100, Generation: entry.Owner.Generation}
	payload := permissionJSON()
	payload = payload[:len(payload)-1] + strings.Repeat(" ", MaxInputBytes-len(payload)) + "}"
	require.NoError(t, c.run(context.Background(), "record-permission", strings.NewReader(payload), io.Discard))
	owner := Owner{Generation: entry.Owner.Generation, UID: entry.Owner.UID, PID: 42, Birth: "boot1:42"}
	recorded, err := v.Store.Read(testSession, owner)
	require.NoError(t, err)
	require.Len(t, PendingIDs(recorded), 1)
}
