// Package preview captures a preview's source and owns its independent connection.
// It does not install routes, listeners, or terminal/task observers.
package preview

import (
	"context"
	"errors"
	"fmt"

	"panemux/internal/config"
	"panemux/internal/session"
	"panemux/internal/tasks"
)

// Source is a creation-time snapshot. Its connection settings and work directory
// cannot be replaced by later pane, task, or configuration changes.
type Source struct {
	ssh           *session.SSHConfig
	host, workdir string
}

func (s Source) Host() string    { return s.host }
func (s Source) Workdir() string { return s.workdir }

// TaskFinder is satisfied by tasks.Service; FindTask performs a fresh collection.
type TaskFinder interface {
	FindTask(context.Context, string) (tasks.Task, error)
}

// CaptureTask resolves the task at creation, without borrowing its collector's
// connection or following the task's subsequent lifetime.
func CaptureTask(
	ctx context.Context, finder TaskFinder, id string,
	connections map[string]config.SSHConnection, sshConfigPath string,
) (Source, error) {
	if err := ctx.Err(); err != nil {
		return Source{}, fmt.Errorf("capture preview: %w", err)
	}
	task, err := finder.FindTask(ctx, id)
	if err != nil {
		return Source{}, fmt.Errorf("capture preview task: %w", err)
	}
	return capture(ctx, task.Host, task.CWD, connections, sshConfigPath)
}

// CapturePane reads the live pane's work directory and host. Callers look up the
// pane by server-owned ID; they never accept browser-supplied host/root values.
// SSH settings come from the live pane. Configuration arguments are retained
// for caller compatibility and are used only by CaptureTask.
func CapturePane(
	ctx context.Context, pane session.Session,
	_ map[string]config.SSHConnection, _ string,
) (Source, error) {
	if err := ctx.Err(); err != nil {
		return Source{}, fmt.Errorf("capture preview: %w", err)
	}
	getter, ok := pane.(session.CWDGetter)
	if !ok {
		return Source{}, errors.New("pane cannot report its working directory")
	}
	var workdir string
	var err error
	if contextual, ok := pane.(session.CWDContextGetter); ok {
		workdir, err = contextual.GetCWDContext(ctx)
	} else {
		workdir, err = getter.GetCWD()
	}
	if err != nil {
		return Source{}, fmt.Errorf("capture preview workdir: %w", err)
	}
	host := ""
	if pane.Type() == session.TypeSSH || pane.Type() == session.TypeSSHTmux {
		namer, ok := pane.(session.SSHConnNamer)
		if !ok || namer.ConnectionName() == "" {
			return Source{}, errors.New("pane has no SSH connection name")
		}
		host = namer.ConnectionName()
	}
	return captureResolved(ctx, host, workdir, func() (session.SSHConfig, error) {
		snapshot, ok := pane.(session.SSHConfigGetter)
		if !ok {
			return session.SSHConfig{}, errors.New("pane cannot report its live SSH settings")
		}
		return snapshot.SSHConfigSnapshot(), nil
	})
}

func capture(
	ctx context.Context, host, workdir string,
	connections map[string]config.SSHConnection, sshConfigPath string,
) (Source, error) {
	return captureResolved(ctx, host, workdir, func() (session.SSHConfig, error) {
		return session.ResolveSSHConfig(host, connections, sshConfigPath)
	})
}

// captureResolved keeps cancellation on both sides of SSH settings resolution.
// The resolver is the same one used by panes, without borrowing a live client.
func captureResolved(
	ctx context.Context, host, workdir string, resolve func() (session.SSHConfig, error),
) (Source, error) {
	if err := ctx.Err(); err != nil {
		return Source{}, fmt.Errorf("capture preview: %w", err)
	}
	source := Source{host: host, workdir: workdir}
	if host != "" {
		cfg, err := resolve()
		if err != nil {
			return Source{}, fmt.Errorf("capture preview SSH settings: %w", err)
		}
		source.ssh = &cfg
	}
	if err := ctx.Err(); err != nil {
		return Source{}, fmt.Errorf("capture preview: %w", err)
	}
	return source, nil
}
