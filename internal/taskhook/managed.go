package taskhook

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strconv"
)

const (
	accountHomeFlag = "--account-home"
	codexHomeFlag   = "--codex-home"
	machineFlag     = "--machine-identity"
	uidFlag         = "--uid"
	executableFlag  = "--executable"
)

type ManagedLaunch struct {
	Executable string
	Target     TargetIdentity
}

// ParseManagedArgs accepts only the fixed launcher's discrete control argv.
// Paths never enter shell code. Scope is intentionally not a supported flag.
func ParseManagedArgs(args []string) (*ManagedLaunch, error) {
	if len(args) != 10 {
		return nil, ErrInput
	}
	values := map[string]string{}
	for len(args) >= 2 {
		if _, exists := values[args[0]]; exists {
			return nil, ErrInput
		}
		values[args[0]] = args[1]
		args = args[2:]
	}
	for _, key := range []string{accountHomeFlag, codexHomeFlag, machineFlag, uidFlag, executableFlag} {
		if values[key] == "" {
			return nil, ErrInput
		}
	}
	uid, err := strconv.Atoi(values[uidFlag])
	if err != nil || uid < 0 || strconv.Itoa(uid) != values[uidFlag] {
		return nil, ErrInput
	}
	return &ManagedLaunch{Executable: values[executableFlag], Target: TargetIdentity{Version: 1, UID: uid,
		AccountHome: values[accountHomeFlag], CodexHome: values[codexHomeFlag], Machine: values[machineFlag]}}, nil
}

func (c *Command) registerManaged(ctx context.Context, output io.Writer) error {
	if c.Managed == nil || c.Managed.Target.UID != os.Geteuid() {
		return ErrInput
	}
	if err := validateInstalledTarget(c.Verifier.Store.root, c.Managed.Target); err != nil {
		return err
	}
	data, _ := json.Marshal(LaunchRegistration{SessionsRoot: c.Managed.Target.CodexHome + "/sessions",
		ExecutablePath: c.Managed.Executable, Scope: LaunchScope{CodexVersion: UnknownScopeVersion, NoDaemon: true}})
	return c.register(ctx, bytes.NewReader(data), output)
}
