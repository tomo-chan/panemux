package codexhook

import (
	"context"
	"io"
	"panemux/internal/host"
	"panemux/internal/hostexec"
	"panemux/internal/hosttools"
	"time"
)

type Conn = hostexec.Conn

// This fixture composes the new application and execution layers; assertions
// in the moved tests still exercise real local scripts and SSH command channels.
type testOptions struct {
	Hosts         func() []string
	Dial          func(string) (Conn, error)
	RunLocal      func(context.Context, string) ([]byte, error)
	Rand          io.Reader
	CodexHookAuto *hosttools.CodexHookAutoStore
	Logf          func(string, ...any)
	HostTimeout   time.Duration
}
type testService struct {
	manager *hosttools.Service
	pool    *hostexec.Pool
}

func newTestService(opts testOptions) *testService {
	pool := hostexec.New(hostexec.Options{Hosts: opts.Hosts, Dial: opts.Dial, RunLocal: opts.RunLocal})
	adapter := New(Options{Execution: pool, Rand: opts.Rand, HostTimeout: opts.HostTimeout})
	manager := hosttools.New(hosttools.Options{
		Hosts: opts.Hosts, Tools: map[hosttools.ID]hosttools.Tool{hosttools.CodexHookTool: adapter},
		CodexHookAuto: opts.CodexHookAuto, Logf: opts.Logf,
	})
	return &testService{manager, pool}
}
func (s *testService) Close() { s.manager.Close(); s.pool.Close() }
func (s *testService) CodexHook(ctx context.Context, name string) (hosttools.State, error) {
	return s.manager.Check(ctx, hosttools.CodexHookTool, host.Host(name))
}
func (s *testService) InstallCodexHook(ctx context.Context, name string) (hosttools.State, error) {
	return s.manager.Install(ctx, hosttools.CodexHookTool, host.Host(name))
}
func (s *testService) SetCodexHookAuto(name string, on bool) error {
	return s.manager.SetCodexHookAuto(name, on)
}
func runLocal(ctx context.Context, script string) ([]byte, error) {
	return hostexec.RunLocal(ctx, script)
}
