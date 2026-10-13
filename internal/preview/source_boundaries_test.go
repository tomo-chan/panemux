package preview

import (
	"context"
	"errors"
	"testing"

	"panemux/internal/config"
	"panemux/internal/session"
	"panemux/internal/tasks"
)

type workdirPane struct {
	session.Session
	workdir func() (string, error)
}

func (p workdirPane) Type() session.Type      { return session.TypeSSH }
func (p workdirPane) GetCWD() (string, error) { return p.workdir() }

type namedWorkdirPane struct{ workdirPane }

func (p namedWorkdirPane) ConnectionName() string { return "dev" }

func (p namedWorkdirPane) SSHConfigSnapshot() session.SSHConfig {
	return session.SSHConfig{Host: "host.test"}
}

type configlessPane struct{ workdirPane }

func (p configlessPane) ConnectionName() string { return "dev" }

type findingTask func(context.Context, string) (tasks.Task, error)

func (f findingTask) FindTask(ctx context.Context, id string) (tasks.Task, error) { return f(ctx, id) }

func TestCapturePaneRequiresServerCapabilities(t *testing.T) {
	for _, pane := range []session.Session{
		&struct{ session.Session }{},
		workdirPane{workdir: func() (string, error) { return "/workspace/project", nil }},
		&paneStub{kind: session.TypeSSH, host: "", cwd: "/workspace/project"},
		configlessPane{workdirPane{workdir: func() (string, error) { return "/workspace/project", nil }}},
	} {
		if _, err := CapturePane(context.Background(), pane, nil, ""); err == nil {
			t.Fatal("missing capability silently became a local source")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CapturePane(ctx, nil, nil, ""); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestCaptureCancellationDuringFreshResolution(t *testing.T) {
	for _, pane := range []bool{false, true} {
		t.Run(map[bool]string{false: "task", true: "pane"}[pane], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var source Source
			var err error
			if pane {
				p := namedWorkdirPane{workdirPane{workdir: func() (string, error) { cancel(); return "/workspace/old", nil }}}
				source, err = CapturePane(ctx, p, nil, "")
			} else {
				finder := findingTask(func(context.Context, string) (tasks.Task, error) {
					cancel()
					return tasks.Task{CWD: "/workspace/old"}, nil
				})
				source, err = CaptureTask(ctx, finder, "local:task", nil, "")
			}
			if !errors.Is(err, context.Canceled) || source.Workdir() != "" {
				t.Fatalf("canceled capture returned source: %v", err)
			}
		})
	}
}

func TestCaptureCancellationWhileSSHSettingsResolve(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source, err := captureResolved(ctx, "dev", "/workspace/old", func() (session.SSHConfig, error) {
		cancel()
		return session.SSHConfig{Host: "host.test"}, nil
	})
	if !errors.Is(err, context.Canceled) || source.Host() != "" {
		t.Fatalf("canceled SSH capture returned source: %v", err)
	}
}

func TestCaptureEveryPaneTypeAndFreshRemoteTask(t *testing.T) {
	connections := map[string]config.SSHConnection{"dev": {Host: "host.test", User: "demo"}}
	for _, kind := range []session.Type{session.TypeLocal, session.TypeTmux, session.TypeSSH, session.TypeSSHTmux} {
		pane := &paneStub{kind: kind, host: "dev", cwd: "/workspace/project"}
		source, err := CapturePane(context.Background(), pane, connections, "/missing/config")
		if err != nil {
			t.Fatal(err)
		}
		remote := kind == session.TypeSSH || kind == session.TypeSSHTmux
		if (source.ssh != nil) != remote || source.Workdir() != pane.cwd {
			t.Fatalf("wrong source for %s", kind)
		}
		if remote && (source.Host() != "dev" || source.ssh.Host != "host.test") {
			t.Fatal("SSH source lost host")
		}
		if !remote && source.Host() != "" {
			t.Fatal("local source inherited unrelated SSH alias")
		}
	}
	finder := findingTask(func(_ context.Context, id string) (tasks.Task, error) {
		if id != "ssh:dev:task" {
			return tasks.Task{}, errors.New("wrong task ID")
		}
		return tasks.Task{Host: "dev", CWD: "/workspace/task"}, nil
	})
	source, err := CaptureTask(context.Background(), finder, "ssh:dev:task", connections, "/missing/config")
	if err != nil || source.Host() != "dev" || source.Workdir() != "/workspace/task" || source.ssh.Host != "host.test" {
		t.Fatalf("remote task capture: %v", err)
	}
}
