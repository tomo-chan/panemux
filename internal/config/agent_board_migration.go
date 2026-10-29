package config

import (
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
)

// Decode the raw document before obsolete struct fields can be silently discarded.
// YAML decoding resolves anchors and merge keys, including settings inherited by panes.
func rejectRemovedBoardSettings(data []byte) error {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}
	if ab, ok := raw["agent_board"].(map[string]any); ok {
		if _, removed := ab["agmsg_path"]; removed {
			return errors.New("agent_board.agmsg_path was removed; agmsg uses ~/.agents/skills/agmsg on each host")
		}
	}
	return rejectPaneBoardSettings(raw)
}

func rejectPaneBoardSettings(value any) error {
	switch node := value.(type) {
	case map[string]any:
		_, pane := node["id"]
		if _, removed := node["agent_board"]; pane && removed {
			return errors.New("pane agent_board was removed; set top-level agent_board.enabled and agent_board.mode")
		}
		for _, child := range node {
			if err := rejectPaneBoardSettings(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range node {
			if err := rejectPaneBoardSettings(child); err != nil {
				return err
			}
		}
	}
	return nil
}
