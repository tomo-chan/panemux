package taskhook

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const HelperVersion = "0.1.0"
const (
	resolveMode  = "resolve-turn"
	providedMode = "mark-provided"
	recoveryMode = "recover-provisional"
)

type Lookup struct {
	SessionID string `json:"session_id"`
	Owner     Owner  `json:"owner"`
}

// LaunchRegistration is control-plane input from a trusted managed launcher.
// Scope must reflect evidence the launcher actually verified. This command
// never infers no-strict/passive-hooks merely from user reviewer metadata.
type LaunchRegistration struct {
	SessionsRoot   string      `json:"sessions_root"`
	ExecutablePath string      `json:"executable_path"`
	Scope          LaunchScope `json:"scope"`
}

type Command struct {
	Verifier   *Verifier
	Managed    *ManagedLaunch
	Generation string
	PID        int
}

// Run bounds blocked stdin and OS I/O as well as lock acquisition. The process
// entry point exits immediately on return: an unknown in-flight commit is
// recovered by rereading durable state. Hook mode always returns success with
// empty stdout and never emits an allow/deny result or payload diagnostics.
func (c *Command) Run(ctx context.Context, mode string, input io.Reader, output io.Writer) error {
	done := make(chan error, 1)
	go func() { done <- c.run(ctx, mode, input, output) }()
	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		err = ErrStorage
	}
	if mode == "record-permission" {
		return nil
	}
	return err
}

func readCommand(input io.Reader, dst any) error {
	data, err := io.ReadAll(io.LimitReader(input, MaxInputBytes+1))
	if err != nil || !strictJSON(data, MaxInputBytes, dst) {
		return ErrInput
	}
	return nil
}

func emitRecord(output io.Writer, r Record) error {
	data, err := EncodeRecord(r)
	if err != nil {
		return err
	}
	if _, err = output.Write(data); err != nil {
		return ErrStorage
	}
	return nil
}

func (c *Command) run(ctx context.Context, mode string, input io.Reader, output io.Writer) error {
	switch mode {
	case "record-permission":
		data, err := io.ReadAll(io.LimitReader(input, MaxInputBytes+1))
		if err != nil {
			return ErrInput
		}
		p, err := ParsePermission(data)
		if err != nil {
			return err
		}
		_, err = c.Verifier.Record(ctx, c.PID, c.Generation, p)
		return err
	case "register-launch":
		return c.register(ctx, input, output)
	case "register-managed-launch":
		return c.registerManaged(ctx, output)
	case resolveMode:
		return c.resolve(ctx, input, output)
	case "inspect":
		var p Lookup
		if err := readCommand(input, &p); err != nil {
			return err
		}
		r, err := c.Verifier.Store.Read(p.SessionID, p.Owner)
		if err != nil {
			return err
		}
		return emitRecord(output, r)
	case providedMode, recoveryMode:
		var p Provided
		if err := readCommand(input, &p); err != nil {
			return err
		}
		var r Record
		var err error
		if mode == providedMode {
			r, err = c.Verifier.Store.MarkProvided(ctx, p)
		} else {
			r, err = c.Verifier.Store.RecoverProvisional(ctx, p)
		}
		if err != nil {
			return err
		}
		return emitRecord(output, r)
	case "selfcheck":
		return c.selfcheck(ctx, output)
	default:
		return ErrUnsupported
	}
}

func (c *Command) register(ctx context.Context, input io.Reader, output io.Writer) error {
	var p LaunchRegistration
	if err := readCommand(input, &p); err != nil {
		return err
	}
	if !filepath.IsAbs(p.SessionsRoot) || !filepath.IsAbs(p.ExecutablePath) ||
		len(p.SessionsRoot) > 4096 || len(p.ExecutablePath) > 4096 {

		return ErrInput
	}
	self, err := c.Verifier.Host.Process(ctx, c.PID)
	if err != nil {
		return ErrUnsupported
	}
	parent, err := c.Verifier.Host.Process(ctx, self.PPID)
	if err != nil || parent.UID != self.UID {
		return ErrUnsupported
	}
	root, err := filepath.EvalSymlinks(p.SessionsRoot)
	if err != nil {
		return ErrUnsupported
	}
	fd, err := openRoot(root)
	if err != nil {
		return err
	}
	_ = unix.Close(fd)
	executable, err := filepath.EvalSymlinks(p.ExecutablePath)
	if err != nil {
		return ErrUnsupported
	}
	id, err := FileIdentity(executable)
	if err != nil {
		return err
	}
	var random [16]byte
	if _, err = io.ReadFull(c.Verifier.Store.random, random[:]); err != nil {
		return ErrStorage
	}
	entry := Registration{Version: ProtocolVersion, ExecutableID: id, SessionsRoot: root, Scope: p.Scope,
		Owner: Owner{Generation: "g1-" + hex.EncodeToString(random[:]), UID: parent.UID,
			PID: parent.PID, Birth: parent.Birth}}
	if err = c.Verifier.Registry.Register(ctx, entry); err != nil {
		return err
	}
	if _, err = io.WriteString(output, entry.Owner.Generation); err != nil {
		return ErrStorage
	}
	return nil
}

func (c *Command) selfcheck(ctx context.Context, output io.Writer) error {
	_, err := c.Verifier.Host.Process(ctx, c.PID)
	probe := "available"
	if err != nil {
		probe = "unavailable"
	}
	data, _ := json.Marshal(struct {
		Version  string `json:"helper_version"`
		Probe    string `json:"process_probe"`
		Protocol int    `json:"protocol_version"`
	}{Version: HelperVersion, Probe: probe, Protocol: ProtocolVersion})
	if _, err = output.Write(data); err != nil {
		return ErrStorage
	}
	return nil
}

func (c *Command) resolve(ctx context.Context, input io.Reader, output io.Writer) error {
	var p Terminal
	if err := readCommand(input, &p); err != nil {
		return err
	}
	r, err := c.Verifier.Store.ResolveTurn(ctx, p)
	if err != nil {
		return err
	}
	return emitRecord(output, r)
}
