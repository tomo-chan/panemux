package preview

import (
	"context"
	"errors"
	"testing"

	"panemux/internal/config"
	"panemux/internal/session"
	"panemux/internal/tasks"
)

type paneStub struct {
	session.Session
	err       error
	kind      session.Type
	host, cwd string
}

func (p *paneStub) Type() session.Type      { return p.kind }
func (p *paneStub) ConnectionName() string  { return p.host }
func (p *paneStub) GetCWD() (string, error) { return p.cwd, p.err }

type taskStub struct {
	err   error
	id    string
	task  tasks.Task
	calls int
}

func (f *taskStub) FindTask(_ context.Context, id string) (tasks.Task, error) {
	f.calls++
	f.id = id
	return f.task, f.err
}

func TestCaptureSourceFreshAndFixed(t *testing.T) {
	conns := map[string]config.SSHConnection{"dev": {Host: "host.test", User: "demo"}}
	pane := &paneStub{kind: session.TypeSSHTmux, host: "dev", cwd: "/workspace/user/project"}
	source, err := CapturePane(context.Background(), pane, conns, "/missing/ssh-config")
	if err != nil {
		t.Fatal(err)
	}
	pane.cwd = "/workspace/other"
	pane.host = "other"
	conns["dev"] = config.SSHConnection{Host: "changed.test"}
	if source.Host() != "dev" || source.Workdir() != "/workspace/user/project" || source.ssh.Host != "host.test" {
		t.Fatal("captured source retargeted")
	}
	finder := &taskStub{task: tasks.Task{Host: "", CWD: "/workspace/task"}}
	taskSource, err := CaptureTask(context.Background(), finder, "local:task", conns, "/missing/ssh-config")
	if err != nil || finder.calls != 1 || finder.id != "local:task" ||
		taskSource.Workdir() != "/workspace/task" || taskSource.Host() != "" {

		t.Fatalf("fresh task capture: %v", err)
	}
	finder.task.CWD = "/workspace/new-task"
	if taskSource.Workdir() != "/workspace/task" {
		t.Fatal("task source changed")
	}
}

func TestCaptureSourceErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	finder := &taskStub{}
	if _, err := CaptureTask(ctx, finder, "task", nil, ""); !errors.Is(err, context.Canceled) || finder.calls != 0 {
		t.Fatalf("cancel: %v", err)
	}
	finder.err = tasks.ErrTaskNotFound
	if _, err := CaptureTask(context.Background(), finder, "task", nil, ""); !errors.Is(err, tasks.ErrTaskNotFound) {
		t.Fatalf("not found: %v", err)
	}
	pane := &paneStub{kind: session.TypeSSH, host: "missing", cwd: "/workspace/project"}
	if _, err := CapturePane(context.Background(), pane, nil, "/missing/ssh-config"); err == nil {
		t.Fatal("missing host accepted")
	}
	pane.kind = session.TypeLocal
	pane.err = errors.New("cwd unavailable")
	if _, err := CapturePane(context.Background(), pane, nil, ""); !errors.Is(err, pane.err) {
		t.Fatalf("cwd: %v", err)
	}
}
