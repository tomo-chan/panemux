package tasks

import (
	"slices"
	"strings"

	"panemux/internal/agent"
)

// fixtureRaw is one host's collection written as tests write it: the common
// sections and every adapter's sections side by side. snapshot hands each
// adapter its own part.
type fixtureRaw struct {
	ProcessCWDs   map[int]string
	PaneIDs       map[int]string
	CodexAges     map[int]int64
	StateFiles    []stateFile
	Processes     []process
	TmuxPanes     []tmuxPane
	Transcripts   []transcript
	CodexOpen     []codexOpenRollout
	CodexRollouts []codexRollout
	Now           int64
}

func (f fixtureRaw) snapshot() rawSnapshot {
	return rawSnapshot{
		ProcessCWDs: f.ProcessCWDs,
		PaneIDs:     f.PaneIDs,
		Processes:   f.Processes,
		TmuxPanes:   f.TmuxPanes,
		Now:         f.Now,
		Agents: map[agent.ID]agentCollected{
			agent.Claude: &claudeCollected{StateFiles: f.StateFiles, Transcripts: f.Transcripts},
			agent.Codex:  &codexCollected{Ages: f.CodexAges, Open: f.CodexOpen, Rollouts: f.CodexRollouts},
		},
	}
}

// claudeOf and codexOf are the adapters' parts of a parsed snapshot.
func claudeOf(raw rawSnapshot) *claudeCollected { return raw.Agents[agent.Claude].(*claudeCollected) }
func codexOf(raw rawSnapshot) *codexCollected   { return raw.Agents[agent.Codex].(*codexCollected) }

// fullOutput is joinLines with every section of the full collection the
// lines leave out added, empty, before "::end": a fixture names only the
// sections it is about.
func fullOutput(lines ...string) []byte {
	end := slices.Index(lines, "::end")
	if end < 0 {
		return joinLines(lines...)
	}
	present := map[string]bool{}
	for _, line := range lines[:end] {
		if name, ok := strings.CutPrefix(line, "::section "); ok {
			present[name] = true
		}
	}
	out := slices.Clone(lines[:end])
	for _, name := range collectPlan.full.sections {
		if !present[name] {
			out = append(out, "::section "+name)
		}
	}
	return joinLines(append(out, lines[end:]...)...)
}
