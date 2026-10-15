package tasks

import (
	"fmt"
	"regexp"
	"strings"

	"panemux/internal/agent"
)

// collectAdapters is every agent the task board collects, in the order its
// sections run and its stopped sessions are merged. Each is registered under
// the registry ID whose Collect capability it implements; nothing registers
// itself. TestCollectAdapters_MatchTheRegistry keeps this list and the
// registry's declarations in step.
func collectAdapters() []registeredCollect {
	return []registeredCollect{
		{id: agent.Claude, adapter: claudeCollect{}},
		{id: agent.Codex, adapter: codexCollect{}},
	}
}

// registeredCollect is one entry of collectAdapters.
type registeredCollect struct {
	id      agent.ID
	adapter collectAdapter
}

// collectAdapter is an agent's part of the collection: the script sections
// that read what the agent writes, and what reads their rows. The task board
// defines it in its own terms; internal/agent only declares that an agent
// collects.
type collectAdapter interface {
	// sections are the adapter's sections, named without its namespace.
	sections() []collectSection
	// processHints are words, at least one of which is in the first two
	// fields of every process the agent's task rule lists.
	processHints() []string
	// newCollected is an empty part for one host's output.
	newCollected() agentCollected
}

// agentCollected is one adapter's part of one host's output, and how the
// adapter turns it into tasks. Live tasks are decided from the live
// sections alone, so the full and the attention collection agree on them.
type agentCollected interface {
	// addRow reads one row of section, a section name without namespace.
	addRow(section, line string)
	// addFile reads one file of a section that declares files.
	addFile(section, name string, data []byte)
	// prepared is the part ready to build tasks from; it does not change
	// the part it is called on.
	prepared() agentCollected
	// liveTasks are the agent's running sessions.
	liveTasks(b *taskBuilder) []Task
	// stoppedCandidates are the agent's logs no live task handles, newest
	// first and each session once. live is the agent's own live tasks.
	stoppedCandidates(b *taskBuilder, live []Task) []stoppedCandidate
	// addLogVersion sets the version of the log a task's summary reads,
	// when the stopped sections report one.
	addLogVersion(task *Task)
}

// collectStage says which runs a section belongs to.
type collectStage int

const (
	// stageLive sections find the running sessions; both runs have them.
	stageLive collectStage = iota
	// stageStopped sections list the logs stopped sessions are found
	// from; only the full collection has them.
	stageStopped
)

// collectSection is one section of the collection script.
type collectSection struct {
	// name is the section's name within its namespace.
	name string
	// script prints the section's rows. The composer prints its
	// "::section" line; the script must not print directives other
	// than "::file", and that only when files is set.
	script string
	stage  collectStage
	// files is whether the section is a list of "::file <name>" blocks.
	files bool
}

// collectRun is one composed script and what its output must carry.
type collectRun struct {
	// byName is every section of the composition, asked for or not.
	byName map[string]plannedSection
	script string
	// sections are the qualified names, in the order the script prints
	// them; each must appear in the output.
	sections []string
	adapters []registeredCollect
}

// plannedSection is a section of a run: its owner ("" for a common
// section) and its name within the owner's namespace.
type plannedSection struct {
	agent agent.ID
	name  string
	files bool
}

// collectPlans are the full and the attention collection, composed from
// the same adapters.
type collectPlans struct {
	// prefilter is the regular expression the cwd and env sections match
	// a process's first two command fields against.
	prefilter string
	full      collectRun
	live      collectRun
}

var (
	validSectionName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	// validProcessHint keeps a hint literal inside the awk regular
	// expression it is joined into.
	validProcessHint = regexp.MustCompile(`^[a-z0-9-]+$`)
)

func mustComposeCollect(defs []agent.Definition, adapters []registeredCollect) collectPlans {
	plans, err := composeCollect(defs, adapters)
	//coverage:exempt runs only if collectAdapters disagrees with the registry, which TestCollectAdapters_MatchTheRegistry forbids
	if err != nil {
		panic("tasks: " + err.Error())
	}
	return plans
}

// composeCollect builds the two collection scripts: the preamble, each
// adapter's live sections, the common sections, and — in the full
// collection only — each adapter's stopped sections.
func composeCollect(defs []agent.Definition, adapters []registeredCollect) (collectPlans, error) {
	if err := validateCollectAdapters(defs, adapters); err != nil {
		return collectPlans{}, err
	}
	var hints []string
	for _, r := range adapters {
		hints = append(hints, r.adapter.processHints()...)
	}
	plans := collectPlans{prefilter: strings.Join(hints, "|")}

	type part struct {
		section collectSection
		owner   agent.ID
	}
	var live, stopped []part
	for _, r := range adapters {
		for _, s := range r.adapter.sections() {
			if s.stage == stageStopped {
				stopped = append(stopped, part{s, r.id})
			} else {
				live = append(live, part{s, r.id})
			}
		}
	}
	for _, s := range commonSections(plans.prefilter) {
		live = append(live, part{section: s})
	}

	// Every run knows every section: an output that carries a section its
	// run does not ask for is read, and its rows go unused.
	known := map[string]plannedSection{}
	for _, p := range append(live[:len(live):len(live)], stopped...) {
		known[qualifiedSection(p.owner, p.section.name)] = plannedSection{
			agent: p.owner, name: p.section.name, files: p.section.files,
		}
	}
	build := func(parts []part) collectRun {
		run := collectRun{byName: known, adapters: adapters}
		var script strings.Builder
		script.WriteString(collectPreamble)
		for _, p := range parts {
			name := qualifiedSection(p.owner, p.section.name)
			run.sections = append(run.sections, name)
			script.WriteString("echo '::section " + name + "'\n")
			script.WriteString(p.section.script)
		}
		script.WriteString(collectEnd)
		run.script = script.String()
		return run
	}
	plans.live = build(live)
	plans.full = build(append(live[:len(live):len(live)], stopped...))
	return plans, nil
}

func qualifiedSection(owner agent.ID, name string) string {
	if owner == "" {
		return name
	}
	return string(owner) + "/" + name
}

// validateCollectAdapters checks adapters against the registry: each is
// registered once, under an agent that declares Collect, every agent that
// declares Collect has one, and their section names and process hints are
// well formed and unique.
func validateCollectAdapters(defs []agent.Definition, adapters []registeredCollect) error {
	declared := map[agent.ID]bool{}
	known := map[agent.ID]bool{}
	for _, d := range defs {
		known[d.ID] = true
		declared[d.ID] = d.Has(agent.Collect)
	}
	registered := map[agent.ID]bool{}
	sections := map[string]bool{}
	for _, r := range adapters {
		switch {
		case !known[r.id]:
			return fmt.Errorf("collect adapter %q is not registered in the agent registry", r.id)
		case !declared[r.id]:
			return fmt.Errorf("collect adapter %q does not declare collect in the agent registry", r.id)
		case registered[r.id]:
			return fmt.Errorf("collect adapter %q is registered twice", r.id)
		}
		registered[r.id] = true
		for _, s := range r.adapter.sections() {
			if !validSectionName.MatchString(s.name) {
				return fmt.Errorf("collect adapter %q: invalid section name %q", r.id, s.name)
			}
			name := qualifiedSection(r.id, s.name)
			if sections[name] {
				return fmt.Errorf("collect adapter %q: section %q is declared twice", r.id, name)
			}
			sections[name] = true
		}
		hints := r.adapter.processHints()
		if len(hints) == 0 {
			return fmt.Errorf("collect adapter %q has no process hint", r.id)
		}
		for _, h := range hints {
			if !validProcessHint.MatchString(h) {
				return fmt.Errorf("collect adapter %q: invalid process hint %q", r.id, h)
			}
		}
	}
	for _, d := range defs {
		if declared[d.ID] && !registered[d.ID] {
			return fmt.Errorf("agent %q declares collect but has no collect adapter", d.ID)
		}
	}
	return nil
}
