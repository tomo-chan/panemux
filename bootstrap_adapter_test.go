package main

import (
	"context"
	"panemux/internal/agmsg"
	"panemux/internal/board"
	"panemux/internal/host"
	"panemux/internal/session"
)

type testBootstrapWatcherConfig struct {
	Manager       *session.Manager
	PaneHosts     map[string]string
	PaneModes     func() map[string]string
	ResolvedPaths map[string]string
	RunnerForHost func(string) host.Runner
	ResolvePath   func(context.Context, string) (string, error)
	Persist       func([]string)
	Team          string
}
type testParticipation struct {
	board.Participation
	resolve func(context.Context, string) (string, error)
	runner  host.Runner
	id      string
}

func (p *testParticipation) Available(ctx context.Context) (bool, error) {
	if p.resolve != nil {
		path, err := p.resolve(ctx, p.id)
		if err != nil {
			return false, err
		}
		p.Participation = agmsg.NewClient(agmsg.ClientConfig{HostID: p.id, Path: path, Runner: p.runner})
		p.resolve = nil
	}
	return p.Participation.Available(ctx)
}
func newTestBootstrapWatcher(cfg testBootstrapWatcherConfig) *bootstrapWatcher {
	participants := map[string]board.Participation{}
	for _, id := range cfg.PaneHosts {
		runner := host.Runner(nil)
		if cfg.RunnerForHost != nil {
			runner = cfg.RunnerForHost(id)
		}
		if runner == nil {
			continue
		}
		path, ok := cfg.ResolvedPaths[id]
		if !ok && cfg.ResolvePath == nil {
			continue
		}
		participants[id] = &testParticipation{
			Participation: agmsg.NewClient(agmsg.ClientConfig{HostID: id, Path: path, Runner: runner}),
			runner:        runner, resolve: cfg.ResolvePath, id: id}
	}
	return newBootstrapWatcher(bootstrapWatcherConfig{
		Manager:   cfg.Manager,
		PaneHosts: cfg.PaneHosts,
		PaneModes: cfg.PaneModes, Team: cfg.Team, Persist: cfg.Persist,
		ParticipationForHost: func(id string) board.Participation { return participants[id] }})
}
