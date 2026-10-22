package tasks

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"panemux/internal/agent"
)

// launchAdapters is every agent panemux can start a new session of, each
// registered under the registry ID whose Launch capability it implements;
// resumeAdapters is the same for Resume. Nothing registers itself.
// TestLaunchAdapters_MatchTheRegistry keeps both lists and the registry's
// declarations in step.
func launchAdapters() []registeredLaunch {
	return []registeredLaunch{
		{id: agent.Claude, adapter: claudeLaunch{}},
		{id: agent.Codex, adapter: codexLaunch{}},
	}
}

func resumeAdapters() []registeredResume {
	return []registeredResume{
		{id: agent.Claude, adapter: claudeLaunch{}},
		{id: agent.Codex, adapter: codexLaunch{}},
	}
}

type registeredLaunch struct {
	adapter launchAdapter
	id      agent.ID
}

type registeredResume struct {
	adapter resumeAdapter
	id      agent.ID
}

// launchAdapter is an agent's part of starting a new session: what the
// session is known by, and the fixed shell command that starts the agent.
// The task board defines it in its own terms; internal/agent only declares
// that an agent launches.
type launchAdapter interface {
	// newSession names a new session: the session ID the agent is started
	// with — "" when the agent picks its own once it runs, in which case
	// the session is known by its tmux session until a collection finds it
	// there (PendingLabels) — and the tmux session it runs in.
	newSession(r io.Reader) (sessionID, tmuxSession string, err error)
	// validNewSessionID reports whether id is one newSession could have
	// returned.
	validNewSessionID(id string) bool
	// newCommand is a fixed shell fragment, run once the agent's binary is
	// found, that sets run: the `sh -c` script tmux runs with the prompt
	// file, the binary, the session ID and the working directory as $1–$4.
	newCommand() string
	// note is a fixed text on what to know before starting a new session,
	// "" when there is nothing to say (issue #376).
	note() string
	// refusals are the refusal codes the adapter's fragments print, beyond
	// the launch script's own, each with its fixed message.
	refusals() map[string]string
}

// resumeAdapter is an agent's part of reopening one of its sessions.
type resumeAdapter interface {
	// validSessionID reports whether id may be passed to the agent's
	// resume. It is checked again on the listed task's ID.
	validSessionID(id string) bool
	// tmuxSessionFor names the tmux session a resume of id runs in.
	tmuxSessionFor(id string) string
	// resumeCommand is a fixed shell fragment that sets resume: the
	// `sh -c` script tmux runs with the working directory, the binary and
	// the session ID as $1–$3.
	resumeCommand() string
	refusals() map[string]string
}

// launchPlanned is the launch script composed from the adapters, and what
// it takes to fill it.
type launchPlanned struct {
	launch   map[agent.ID]launchAdapter
	resume   map[agent.ID]resumeAdapter
	messages map[string]string
	script   string
	// launchIDs are the agents that launch, in registry order.
	launchIDs []agent.ID
}

var launchPlan = mustComposeLaunch(agent.All(), launchAdapters(), resumeAdapters())

func mustComposeLaunch(defs []agent.Definition, launches []registeredLaunch, resumes []registeredResume) launchPlanned {
	plan, err := composeLaunch(defs, launches, resumes)
	//coverage:exempt runs only if launchAdapters disagrees with the registry, which a test forbids
	if err != nil {
		panic("tasks: " + err.Error())
	}
	return plan
}

// launchScriptCases marks where the adapters' commands go in
// launchScriptTemplate.
const launchScriptCases = "{{AGENT_COMMANDS}}\n"

// composeLaunch checks the adapters against the registry and puts each
// command in the case of its agent and mode, in registry order.
func composeLaunch(
	defs []agent.Definition, launches []registeredLaunch, resumes []registeredResume,
) (launchPlanned, error) {
	if err := validateLaunchAdapters(defs, launches, resumes); err != nil {
		return launchPlanned{}, err
	}
	plan := launchPlanned{
		launch: map[agent.ID]launchAdapter{}, resume: map[agent.ID]resumeAdapter{}, messages: map[string]string{},
	}
	for code, msg := range launchErrorMessages {
		plan.messages[code] = msg
	}
	for _, r := range launches {
		plan.launch[r.id] = r.adapter
	}
	for _, r := range resumes {
		plan.resume[r.id] = r.adapter
	}
	var cases strings.Builder
	cases.WriteString("case $agent:$mode in\n")
	for _, d := range defs {
		l, launches := plan.launch[d.ID]
		r, resumes := plan.resume[d.ID]
		if !launches && !resumes {
			continue
		}
		plan.messages[refusedNoAgent(d.ID)] = string(d.ID) + " was not found on the host"
		if launches {
			plan.launchIDs = append(plan.launchIDs, d.ID)
			writeLaunchCase(&cases, d.ID, launchNew, l.newCommand())
			addRefusals(plan.messages, l.refusals())
		}
		if resumes {
			writeLaunchCase(&cases, d.ID, launchResume, r.resumeCommand())
			addRefusals(plan.messages, r.refusals())
		}
	}
	cases.WriteString("*) say 'error " + RefusedUnsupported + "'; exit 0 ;;\nesac\n")
	plan.script = strings.Replace(launchScriptTemplate, launchScriptCases, cases.String(), 1)
	return plan, nil
}

func writeLaunchCase(b *strings.Builder, id agent.ID, mode launchMode, command string) {
	fmt.Fprintf(b, "%s:%s)\n%s\n  ;;\n", id, mode, strings.TrimRight(command, "\n"))
}

func addRefusals(messages, refusals map[string]string) {
	for code, msg := range refusals {
		messages[code] = msg
	}
}

// refusedNoAgent is the code the launch script refuses with when it finds
// no binary of the agent.
func refusedNoAgent(id agent.ID) string { return "no-" + string(id) }

var (
	validRefusalCode = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	// scriptRefusal is a refusal a fragment prints, as the template does.
	scriptRefusal = regexp.MustCompile(`say ['"]error ([^'"]*)['"]`)
)

// validateLaunchAdapters checks adapters against the registry: each is
// registered once, under an agent that declares its capability, every agent
// that declares Launch or Resume has an adapter for it, and each fragment
// sets its variable, stays inside its case and refuses only with codes the
// script or the adapter declares.
func validateLaunchAdapters(defs []agent.Definition, launches []registeredLaunch, resumes []registeredResume) error {
	known := map[agent.ID]agent.Definition{}
	for _, d := range defs {
		known[d.ID] = d
	}
	launched := map[agent.ID]bool{}
	for _, r := range launches {
		if err := checkRegistration("launch", agent.Launch, r.id, known, launched); err != nil {
			return err
		}
		if err := checkFragment("launch", r.id, "run", r.adapter.newCommand(), r.adapter.refusals()); err != nil {
			return err
		}
	}
	resumed := map[agent.ID]bool{}
	for _, r := range resumes {
		if err := checkRegistration("resume", agent.Resume, r.id, known, resumed); err != nil {
			return err
		}
		if err := checkFragment("resume", r.id, "resume", r.adapter.resumeCommand(), r.adapter.refusals()); err != nil {
			return err
		}
	}
	for _, d := range defs {
		switch {
		case d.Has(agent.Launch) && !launched[d.ID]:
			return fmt.Errorf("agent %q declares launch but has no launch adapter", d.ID)
		case d.Has(agent.Resume) && !resumed[d.ID]:
			return fmt.Errorf("agent %q declares resume but has no resume adapter", d.ID)
		}
	}
	return nil
}

func checkRegistration(
	kind string, c agent.Capability, id agent.ID, known map[agent.ID]agent.Definition, seen map[agent.ID]bool,
) error {
	d, ok := known[id]
	switch {
	case !ok:
		return fmt.Errorf("%s adapter %q is not registered in the agent registry", kind, id)
	case !d.Has(c):
		return fmt.Errorf("%s adapter %q does not declare %s in the agent registry", kind, id, c)
	case seen[id]:
		return fmt.Errorf("%s adapter %q is registered twice", kind, id)
	}
	seen[id] = true
	return nil
}

func checkFragment(kind string, id agent.ID, variable, command string, refusals map[string]string) error {
	for code, msg := range refusals {
		switch {
		case !validRefusalCode.MatchString(code):
			return fmt.Errorf("%s adapter %q: invalid refusal code %q", kind, id, code)
		case launchErrorMessages[code] != "" || code == RefusedUnsupported || strings.HasPrefix(code, "no-"):
			return fmt.Errorf("%s adapter %q: refusal code %q is the launch script's own", kind, id, code)
		case msg == "":
			return fmt.Errorf("%s adapter %q: refusal code %q has no message", kind, id, code)
		}
	}
	for _, marker := range []string{"{{", ";;", "esac"} {
		if strings.Contains(command, marker) {
			return fmt.Errorf("%s adapter %q: its command holds %q", kind, id, marker)
		}
	}
	if !regexp.MustCompile(`(^|[\s;])` + variable + `=`).MatchString(command) {
		return fmt.Errorf("%s adapter %q: its command does not set %s", kind, id, variable)
	}
	for _, m := range scriptRefusal.FindAllStringSubmatch(command, -1) {
		if _, ok := refusals[m[1]]; !ok && m[1] != refusedNoAgent(id) {
			return fmt.Errorf("%s adapter %q: its command refuses with %q, which it does not declare", kind, id, m[1])
		}
	}
	return nil
}

// CanLaunch reports whether panemux can start a new session of the agent
// named by id.
func CanLaunch(id string) bool {
	_, ok := launchPlan.launch[agent.ID(id)]
	return ok
}

// LaunchAgents are the agents panemux can start a new session of, in
// registry order.
func LaunchAgents() []string {
	ids := make([]string, 0, len(launchPlan.launchIDs))
	for _, id := range launchPlan.launchIDs {
		ids = append(ids, string(id))
	}
	return ids
}

// claudeLaunch starts and resumes Claude Code. A new session is given a
// session ID panemux mints, so the task and its labels are known before
// claude has written anything.
type claudeLaunch struct{}

func (claudeLaunch) newSession(r io.Reader) (string, string, error) {
	id, err := newSessionID(r)
	if err != nil {
		return "", "", err
	}
	return id, tmuxSessionForTask(id), nil
}

func (claudeLaunch) validNewSessionID(id string) bool { return validUUID.MatchString(id) }
func (claudeLaunch) validSessionID(id string) bool    { return validUUID.MatchString(id) }
func (claudeLaunch) tmuxSessionFor(id string) string  { return tmuxSessionForTask(id) }
func (claudeLaunch) refusals() map[string]string      { return nil }
func (claudeLaunch) note() string                     { return "" }

// A claude resume passes the session ID as "--resume=<id>": `--resume`
// takes an optional value, and an ID given as a separate argument that
// began with "-" would be parsed as an option.
func (claudeLaunch) newCommand() string {
	return `  run='p=$(cat -- "$1"); rm -f -- "$1"; cd -- "$4" || exit 1; exec "$2" "--session-id=$3" -- "$p"'`
}

func (claudeLaunch) resumeCommand() string {
	return `  resume='cd -- "$1" || exit 1; exec "$2" "--resume=$3"'`
}

// codexLaunch starts and resumes codex. Codex picks a new session's ID
// itself, once it has been given its first instruction, so a new session has
// none at launch.
type codexLaunch struct{}

func (codexLaunch) newSession(r io.Reader) (string, string, error) {
	name, err := newCodexTmuxSession(r)
	return "", name, err
}

func (codexLaunch) validNewSessionID(id string) bool { return id == "" }

// validSessionID refuses a session name: `codex resume` takes one too, and
// only a listed UUID is passed.
func (codexLaunch) validSessionID(id string) bool   { return validUUID.MatchString(id) }
func (codexLaunch) tmuxSessionFor(id string) string { return tmuxSessionForCodex(id) }

func (codexLaunch) note() string {
	return "Codex chooses its session ID once it has started its session, so the labels are recorded once codex " +
		"has started its session. If it stops at a start-up screen, open the task's pane to answer it."
}

func (codexLaunch) refusals() map[string]string {
	return map[string]string{RefusedCodexTooOld: "codex on the host is too old: it has no --no-daemon option"}
}

// codexPrelude refuses a codex whose help lists no --no-daemon, and sets
// codex: how the fixed `sh -c` runs it, with its own directory last on PATH,
// without the shared daemon and without the update prompt (see
// launchScriptTemplate).
const codexPrelude = `  PATH=$PATH:${bin%/*} "$bin" --help </dev/null 2>/dev/null | grep -q -e '--no-daemon' ||
    { say 'error codex-too-old'; exit 0; }
  codex='PATH=$PATH:${2%/*}; export PATH; exec "$2" --no-daemon -c check_for_update_on_startup=false'
`

// A codex prompt and a codex resume's session ID both follow "--": codex
// parses an unguarded leading "-" as an option, and `codex resume <id> '-h …'`
// prints its help and exits 0.
func (codexLaunch) newCommand() string {
	return codexPrelude + `  run='p=$(cat -- "$1"); rm -f -- "$1"; cd -- "$4" || exit 1; '"$codex"' -- "$p"'`
}

func (codexLaunch) resumeCommand() string {
	return codexPrelude + `  resume='cd -- "$1" || exit 1; '"$codex"' resume -- "$3"'`
}
