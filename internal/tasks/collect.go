package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"panemux/internal/agent"
)

// collectScript is the one command the dashboard runs on every host, local
// or remote, to learn which agent sessions exist there, and attentionScript
// the part of it the input-wait notifications run (issue #278), without the
// stopped sessions' sections. Both are composed once, at startup, from Go
// constants alone: the common sections below and the sections each collect
// adapter (collect_adapters.go) declares. No value from a request, a config
// file or a remote host is ever spliced into them, and they reach the shell
// on stdin (`sh -s`), not as an argument, so the remote login shell never
// parses them. See docs/security/command-execution.md, "Task dashboard
// collection".
//
// They read only what the agents themselves write and what `ps`/`tmux`
// report, and never fail as a whole: every probe that can be missing on a
// host (no tmux, no ~/.claude, BSD stat) is allowed to print nothing. Their
// output is a line protocol parsed by parseCollectOutput:
//
//	::panemux-tasks v2           header; anything before it is ignored
//	::now <unix seconds>         the host's own clock
//	::section <agent>/<name>     an adapter's live section, in registration order
//	::section ps                 "<pid> <ppid> <command>", this user's processes only
//	::section tmux               "<pane pid> <tmux session name>"
//	::section cwd                "<pid> <cwd>" for this user's processes that may be agents
//	::section env                "<pid> <PANEMUX_PANE_ID>" for the same processes, when set
//	::section <agent>/<name>     an adapter's stopped section (collectScript only)
//	::file <name>                starts a file, in a section that declares files
//	::end                        the output is complete
//
// The common sections carry no namespace; an adapter's are named after its
// agent's ID, which cannot hold a "/", so the two never collide. Every
// section the run declares must appear before "::end", or the host's
// collection fails: a section a host did not report is not taken as empty.
// The version changes when the directives' grammar or a common section's
// rows change, not when an adapter adds a section; script and parser ship
// in one binary, so it only tells a mismatched output apart.
//
// An adapter's live sections come before the common ones, so a session
// file or an open rollout is read before `ps` lists the processes: a
// process that exits in between is then not listed, rather than listed
// without its session.
//
// "Processes that may be agents" are those whose first two command fields
// match the prefilter composed from the adapters' process hints
// (collectPlan.prefilter). It only narrows the per-process probes; which
// processes are agents is decided in Go by each agent's task rule, and
// TestCollectPrefilter_CoversEveryTaskRule keeps the prefilter wide enough.
//
// The env section names the pane an agent outside tmux was started from
// (issue #254). It is read from the process's initial environment, which on
// Linux is /proc/<pid>/environ; a host that has neither it nor the macOS
// reading below reports nothing. Entries there are NUL-separated and a value
// may itself hold newlines, so newlines become \001 before NULs become
// newlines: a line inside another variable's value can then never start a
// row, and a value carrying \001 fails validPaneID.
//
// On macOS (issue #263) the environment is read with `ps -E`, which prints
// the arguments, a space, and the NAME=value entries joined by spaces, with
// nothing to tell an argument or a value from an entry. The arguments
// `ps -o command=` reports are removed from the front — output that does not
// start with them followed by a space is not read — and the value is taken
// only when exactly one " PANEMUX_PANE_ID=" is left, up to the next space; two
// or more may be another variable's value, so none is taken. The shell keeps
// only a value of validPaneID's characters, so a newline in it cannot start a
// row. macOS shows no environment for Apple's own binaries or another user's
// processes; those report nothing.
//
// The value is untrusted — any process of the user can set it — and is
// checked against validPaneID here and against the panes it names in the
// browser.
var (
	collectPlan     = mustComposeCollect(agent.All(), collectAdapters())
	collectScript   = collectPlan.full.script
	attentionScript = collectPlan.live.script
)

// collectPreamble starts every run: the header, the host's clock, and the
// shell helpers the sections share (uid, and mtime printing a file's
// modification time and size).
const collectPreamble = `LC_ALL=C
export LC_ALL
echo '::panemux-tasks v2'
echo "::now $(date +%s)"
uid=$(id -u)
if stat -c %Y / >/dev/null 2>&1; then
	mtime() { stat -c '%Y %s' "$1"; }
else
	mtime() { stat -f '%m %z' "$1"; }
fi
`

const collectEnd = `echo '::end'
exit 0
`

const (
	collectHeader   = "::panemux-tasks v2"
	directivePrefix = "::"
	sectionPS       = "ps"
	sectionTmux     = "tmux"
	sectionCWD      = "cwd"
	sectionEnv      = "env"
)

// commonSections are the sections every run has, whatever agents are
// registered. prefilter is the alternation of the adapters' process hints.
func commonSections(prefilter string) []collectSection {
	return []collectSection{
		{name: sectionPS, script: `ps -U "$uid" -o pid=,ppid=,command= 2>/dev/null
`},
		{name: sectionTmux, script: `tmux list-panes -a -F '#{pane_pid} #{session_name}' 2>/dev/null
`},
		{name: sectionCWD, script: `agents=$(ps -U "$uid" -o pid=,command= 2>/dev/null |
	awk '($2 " " $3) ~ /` + prefilter + `/ { print $1 }')
for pid in $agents; do
	c=$(readlink "/proc/$pid/cwd" 2>/dev/null ||
		lsof -a -p "$pid" -d cwd -Fn 2>/dev/null | awk '/^n/ { print substr($0, 2); exit }')
	if [ -n "$c" ]; then echo "$pid $c"; fi
done
`},
		{name: sectionEnv, script: `if [ "$(uname -s 2>/dev/null)" = Darwin ]; then
	for pid in $agents; do
		args=$(ps -p "$pid" -o command= 2>/dev/null)
		[ -n "$args" ] || continue
		full=$(ps -E -p "$pid" -o command= 2>/dev/null)
		case $full in
		"$args"*) rest=${full#"$args"} ;;
		*) continue ;;
		esac
		case $rest in
		" "*) ;;
		*) continue ;;
		esac
		case $rest in
		*" PANEMUX_PANE_ID="*" PANEMUX_PANE_ID="*) continue ;;
		*" PANEMUX_PANE_ID="*) v=${rest#*" PANEMUX_PANE_ID="} ;;
		*) continue ;;
		esac
		v=${v%% *}
		case $v in
		'' | *[!A-Za-z0-9_.-]*) continue ;;
		esac
		echo "$pid $v"
	done
else
	for pid in $agents; do
		v=$(tr '\n\000' '\001\n' <"/proc/$pid/environ" 2>/dev/null | grep -m 1 '^PANEMUX_PANE_ID=')
		if [ -n "$v" ]; then echo "$pid ${v#PANEMUX_PANE_ID=}"; fi
	done
fi
`},
	}
}

// rawSnapshot is one host's collection output, parsed but not interpreted.
type rawSnapshot struct {
	ProcessCWDs map[int]string
	// PaneIDs is the PANEMUX_PANE_ID each agent process was started with,
	// already limited to validPaneID.
	PaneIDs map[int]string
	// Agents is each collect adapter's own part of the output, by agent.
	Agents    map[agent.ID]agentCollected
	Processes []process
	TmuxPanes []tmuxPane
	Now       int64
}

type process struct {
	Command string
	PID     int
	PPID    int
}

type tmuxPane struct {
	Session string
	PanePID int
}

// parseCollectOutput reads the output of run's script. It fails only when
// the output as a whole cannot be trusted — no header, no clock, no
// terminating "::end" (a dropped connection mid-run), a directive or a
// section it does not know, or a section run declares that is missing. A
// malformed row inside a section is skipped: one odd `ps` line must not hide
// every task on the host.
func parseCollectOutput(out []byte, run collectRun) (rawSnapshot, error) {
	lines := strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")

	start := -1
	for i, line := range lines {
		if line == collectHeader {
			start = i + 1
			break
		}
	}
	//mutation:exempt[CONDITIONALS_BOUNDARY] equivalent — start is -1 or a line index plus one, never 0
	if start < 0 {
		return rawSnapshot{}, errors.New("task collection output has no header")
	}

	p := collectParser{run: run, seen: map[string]bool{}, raw: rawSnapshot{
		ProcessCWDs: map[int]string{},
		PaneIDs:     map[int]string{},
		Agents:      make(map[agent.ID]agentCollected, len(run.adapters)),
	}}
	for _, r := range run.adapters {
		p.raw.Agents[r.id] = r.adapter.newCollected()
	}
	for _, line := range lines[start:] {
		var err error
		if strings.HasPrefix(line, directivePrefix) {
			err = p.directive(line)
		} else {
			err = p.row(line)
		}
		if err != nil {
			return rawSnapshot{}, err
		}
		if p.complete {
			break
		}
	}

	if !p.complete {
		return rawSnapshot{}, errors.New("task collection output ended early")
	}
	if !p.haveNow {
		return rawSnapshot{}, errors.New("task collection output has no clock")
	}
	return p.raw, nil
}

// collectParser is parseCollectOutput's position in the output.
type collectParser struct {
	seen     map[string]bool
	file     *collectedFile
	run      collectRun
	section  plannedSection
	raw      rawSnapshot
	haveNow  bool
	complete bool
	inside   bool
}

// collectedFile is a file being read in a section that declares files.
type collectedFile struct {
	name string
	data []byte
}

func (p *collectParser) directive(line string) error {
	directive, arg, _ := strings.Cut(strings.TrimPrefix(line, directivePrefix), " ")
	switch directive {
	case "now":
		now, err := strconv.ParseInt(arg, 10, 64)
		if err != nil {
			return fmt.Errorf("task collection clock %q: %w", arg, err)
		}
		p.raw.Now = now
		p.haveNow = true
	case "section":
		p.flush()
		section, ok := p.run.byName[arg]
		if !ok {
			return fmt.Errorf("task collection output has unknown section %q", arg)
		}
		p.section, p.inside = section, true
		p.seen[arg] = true
	case "file":
		if !p.inside || !p.section.files {
			return errors.New("task collection output has a file in a section that takes none")
		}
		p.flush()
		p.file = &collectedFile{name: arg}
	case "end":
		p.flush()
		for _, name := range p.run.sections {
			if !p.seen[name] {
				return fmt.Errorf("task collection output is missing section %q", name)
			}
		}
		p.complete = true
	default:
		return fmt.Errorf("task collection output has unknown directive %q", line)
	}
	return nil
}

// flush hands the file being read, if any, to its adapter.
func (p *collectParser) flush() {
	if p.file != nil {
		data := []byte(strings.TrimSpace(string(p.file.data)))
		p.raw.Agents[p.section.agent].addFile(p.section.name, p.file.name, data)
		p.file = nil
	}
}

func (p *collectParser) row(line string) error {
	switch {
	case !p.inside:
		if strings.TrimSpace(line) != "" {
			return errors.New("task collection output has data outside a section")
		}
	case p.section.files:
		if p.file == nil {
			if strings.TrimSpace(line) == "" {
				return nil
			}
			return errors.New("task collection output has file data before a file name")
		}
		p.file.data = append(p.file.data, line...)
		p.file.data = append(p.file.data, '\n')
	case p.section.agent != "":
		p.raw.Agents[p.section.agent].addRow(p.section.name, line)
	default:
		p.raw.addRow(p.section.name, line)
	}
	return nil
}

// addRow reads one row of a common section.
func (raw *rawSnapshot) addRow(section, line string) {
	switch section {
	case sectionPS:
		if p, ok := parseProcessRow(line); ok {
			raw.Processes = append(raw.Processes, p)
		}
	case sectionTmux:
		if pid, rest, ok := leadingPID(line); ok && rest != "" {
			raw.TmuxPanes = append(raw.TmuxPanes, tmuxPane{PanePID: pid, Session: rest})
		}
	case sectionCWD:
		if pid, rest, ok := leadingPID(line); ok && rest != "" {
			raw.ProcessCWDs[pid] = rest
		}
	case sectionEnv:
		if pid, rest, ok := leadingPID(line); ok && validPaneID.MatchString(rest) {
			raw.PaneIDs[pid] = rest
		}
	}
}

// leadingPID splits "<pid> <rest>" and reports whether the pid parsed.
func leadingPID(line string) (int, string, bool) {
	head, rest, _ := strings.Cut(strings.TrimSpace(line), " ")
	pid, err := strconv.Atoi(head)
	if err != nil || pid <= 0 {
		return 0, "", false
	}
	return pid, strings.TrimSpace(rest), true
}

func parseProcessRow(line string) (process, bool) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return process{}, false
	}
	pid, err := strconv.Atoi(fields[0])
	// pid 0 (macOS lists kernel_task) is no process an agent can run under,
	// and dropping it keeps the parent walk from ever stepping onto it.
	if err != nil || pid <= 0 {
		return process{}, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return process{}, false
	}
	return process{PID: pid, PPID: ppid, Command: strings.Join(fields[2:], " ")}, true
}

// transcriptCWD decodes the `"cwd":"..."` fragment grep found in a
// transcript. Anything it cannot decode as a JSON string yields "": the
// working directory is only a label and a git lookup key, and a session
// without one is still listed.
func transcriptCWD(fragment string) string {
	value, ok := strings.CutPrefix(fragment, `"cwd":`)
	if !ok {
		return ""
	}
	var cwd string
	if err := json.Unmarshal([]byte(value), &cwd); err != nil {
		return ""
	}
	return cwd
}
