package agmsg

import (
	"fmt"

	"panemux/internal/agent"
)

// boardAdapters retains registry ID to agmsg type mappings for registration.
func boardAdapters() []registeredBoard {
	return []registeredBoard{
		{id: agent.Claude, agmsgType: "claude-code"},
		{id: agent.Codex, agmsgType: string(agent.Codex)},
		{id: agent.Cursor, agmsgType: string(agent.Cursor)},
		{id: agent.Gemini, agmsgType: string(agent.Gemini)},
		{id: agent.GrokBuild, agmsgType: string(agent.GrokBuild)},
		{id: agent.OpenCode, agmsgType: string(agent.OpenCode)},
	}
}

type registeredBoard struct {
	id        agent.ID
	agmsgType string
}

// Validate the retained mapping at startup, as well as in registry contract tests.
var boardAdaptersByID = mustBoardAdapters(agent.All(), boardAdapters())

func mustBoardAdapters(defs []agent.Definition, adapters []registeredBoard) map[agent.ID]string {
	//coverage:exempt the registered set is checked by TestBoardAdapters_MatchTheRegistry
	if err := validateBoardAdapters(defs, adapters); err != nil {
		panic(err)
	}
	byID := make(map[agent.ID]string, len(adapters))
	for _, entry := range adapters {
		byID[entry.id] = entry.agmsgType
	}
	return byID
}

// validateBoardAdapters checks adapters against the registry: each is
// registered once, under an agent that declares board, and every agent that
// declares board has one.
func validateBoardAdapters(defs []agent.Definition, adapters []registeredBoard) error {
	known := map[agent.ID]agent.Definition{}
	for _, d := range defs {
		known[d.ID] = d
	}
	seen := map[agent.ID]bool{}
	types := map[string]bool{}
	for _, r := range adapters {
		d, ok := known[r.id]
		switch {
		case !ok:
			return fmt.Errorf("board adapter %q is not registered in the agent registry", r.id)
		case !d.Has(agent.Board):
			return fmt.Errorf("board adapter %q does not declare %s in the agent registry", r.id, agent.Board)
		case seen[r.id]:
			return fmt.Errorf("board adapter %q is registered twice", r.id)
		}
		if !agent.ID(r.agmsgType).Valid() || types[r.agmsgType] {
			return fmt.Errorf("board adapter %q has invalid or duplicate agmsg type %q", r.id, r.agmsgType)
		}
		types[r.agmsgType] = true
		seen[r.id] = true
	}
	for _, d := range defs {
		if d.Has(agent.Board) && !seen[d.ID] {
			return fmt.Errorf("agent %q declares board but has no board adapter", d.ID)
		}
	}
	return nil
}
