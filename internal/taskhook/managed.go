package taskhook

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strconv"
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
	for i := 0; i+1 < len(args); i += 2 {
		if _, exists := values[args[i]]; exists {
			return nil, ErrInput
		}
		values[args[i]] = args[i+1]
	}
	for _, key := range []string{"--account-home", "--codex-home", "--machine-identity", "--uid", "--executable"} {
		if values[key] == "" {
			return nil, ErrInput
		}
	}
	uid, err := strconv.Atoi(values["--uid"])
	if err != nil || strconv.Itoa(uid) != values["--uid"] {
		return nil, ErrInput
	}
	return &ManagedLaunch{Executable: values["--executable"], Target: TargetIdentity{Version: 1, UID: uid,
		AccountHome: values["--account-home"], CodexHome: values["--codex-home"], Machine: values["--machine-identity"]}}, nil
}

func (c *Command) registerManaged(ctx context.Context, output io.Writer) error {
	if c.Managed == nil || c.Managed.Target.UID != os.Geteuid() {
		return ErrInput
	}
	if err := validateInstalledTarget(c.Verifier.Store.root, c.Managed.Target); err != nil {
		return err
	}
	data, err := json.Marshal(LaunchRegistration{SessionsRoot: c.Managed.Target.CodexHome + "/sessions",
		ExecutablePath: c.Managed.Executable, Scope: LaunchScope{CodexVersion: UnknownScopeVersion, NoDaemon: true}})
	if err != nil {
		return ErrInput
	}
	return c.register(ctx, bytes.NewReader(data), output)
}
