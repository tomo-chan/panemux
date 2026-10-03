package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// collectScript is the one command the dashboard runs on every host, local
// or remote, to learn which agent sessions exist there. It is a constant: no
// value from a request, a config file or a remote host is ever spliced into
// it, and it reaches the shell on stdin (`sh -s`), not as an argument, so the
// remote login shell never parses it. See docs/security/command-execution.md,
// "Task dashboard collection".
//
// It reads only what the agents themselves write and what `ps`/`tmux` report,
// and never fails as a whole: every probe that can be missing on a host (no
// tmux, no ~/.claude, BSD stat) is allowed to print nothing. Its output is a
// line protocol parsed by parseCollectOutput:
//
//	::panemux-tasks v1           header; anything before it is ignored
//	::now <unix seconds>         the host's own clock
//	::section state              ~/.claude/sessions/*.json, each after "::file <name>"
//	::section ps                 "<pid> <ppid> <command>", this user's processes only
//	::section tmux               "<pane pid> <tmux session name>"
//	::section cwd                "<pid> <cwd>" for this user's processes that may be claude or codex
//	::section env                "<pid> <PANEMUX_PANE_ID>" for the same processes, when set
//	::section codex-open         one row per rollout a codex process holds open (below)
//	::section transcripts        "<mtime>\t<file name>\t<first "cwd":"..." in it>\t<size in bytes>"
//	::section codex-rollouts     "<mtime>\t<size>\t<file name>\t<"cwd":"...">\t<"originator":"...">" of its first line
//	::end                        the output is complete
//
// Transcripts are the conversation logs directly under ~/.claude/projects/*/
// modified in the last 7 days, newest first, at most 100 of them. The window
// and the count are the range decided for stopped sessions (issue #252); the
// Go side keeps the stopped ones and caps them at maxStoppedTasks. Codex
// rollouts (~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl) are listed the same
// way, counting only the TUI's (originator "codex-tui"): the 100 are taken
// after that filter, so a host where codex exec writes many rollouts does not
// push the TUI's sessions out.
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
//
// Codex writes no file that ties a process to its session (issue #264): the
// rollout a codex process holds open is its session, found through
// /proc/<pid>/fd, or lsof where there is no /proc. Each codex process gets a
// "<pid>\t<etime>" row, then one row per rollout it holds open:
//
//	<pid>\t<etime>\t<mtime> <size>\t<status> <started_at>\t<turn event line>\t<response item line>\t<"cwd":"...">
//	\t<"originator":"...">\t<path>        (one line)
//
// where etime is how long the process has run (ps's [[dd-]hh:]mm:ss), status
// and started_at are the session's newest thread_turns row in
// ~/.codex/thread_history_1.sqlite when sqlite3 can read one, and the two
// lines are the first 512 bytes of the last turn event (task_started,
// task_complete, turn_aborted) and the last response item in the rollout's
// final MiB. Only a session ID made of hex digits and dashes — the shape of
// the UUID in the file name — is put into the query.
const collectScript = collectLiveScript + collectStoppedScript + collectEnd

// attentionScript is collectScript without collectStoppedScript: what the
// input-wait notifications run (issue #278). It lists the running sessions
// only, so it never searches ~/.claude/projects or ~/.codex/sessions.
const attentionScript = collectLiveScript + collectEnd

// collectLiveScript is the part of the collection that finds the running
// sessions: state files, processes, panes, and the rollouts codex holds open.
const collectLiveScript = `LC_ALL=C
export LC_ALL
echo '::panemux-tasks v1'
echo "::now $(date +%s)"
echo '::section state'
for f in "$HOME"/.claude/sessions/*.json; do
	[ -f "$f" ] || continue
	echo "::file ${f##*/}"
	cat "$f" 2>/dev/null
	echo
done
uid=$(id -u)
echo '::section ps'
ps -U "$uid" -o pid=,ppid=,command= 2>/dev/null
echo '::section tmux'
tmux list-panes -a -F '#{pane_pid} #{session_name}' 2>/dev/null
agents=$(ps -U "$uid" -o pid=,command= 2>/dev/null | awk '($2 " " $3) ~ /claude|codex/ { print $1 }')
echo '::section cwd'
for pid in $agents; do
	c=$(readlink "/proc/$pid/cwd" 2>/dev/null ||
		lsof -a -p "$pid" -d cwd -Fn 2>/dev/null | awk '/^n/ { print substr($0, 2); exit }')
	if [ -n "$c" ]; then echo "$pid $c"; fi
done
echo '::section env'
if [ "$(uname -s 2>/dev/null)" = Darwin ]; then
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
if stat -c %Y / >/dev/null 2>&1; then
	mtime() { stat -c '%Y %s' "$1"; }
else
	mtime() { stat -f '%m %z' "$1"; }
fi
echo '::section codex-open'
db="$HOME/.codex/thread_history_1.sqlite"
ps -U "$uid" -o pid=,etime=,command= 2>/dev/null |
awk '{ n = $3; sub(/.*\//, "", n); if (n == "codex") print $1, $2 }' |
while read -r pid et; do
	printf '%s\t%s\n' "$pid" "$et"
	if [ -d "/proc/$pid/fd" ]; then
		for fd in "/proc/$pid/fd"/*; do readlink "$fd"; done 2>/dev/null
	else
		lsof -p "$pid" -Fn 2>/dev/null | sed -n 's/^n//p'
	fi | while IFS= read -r p; do
		case ${p##*/} in rollout-*.jsonl) ;; *) continue ;; esac
		t=$(mtime "$p" 2>/dev/null) || continue
		id=${p##*/}
		id=${id%.jsonl}
		id=${id#rollout-????-??-??T??-??-??-}
		case $id in *[!0-9a-f-]*|'') id= ;; esac
		st=
		if [ -n "$id" ] && [ -f "$db" ]; then
			q="select status || ' ' || coalesce(started_at, '') from thread_turns"
			q="$q where thread_id = '$id' order by rollout_ordinal desc limit 1"
			st=$(sqlite3 -readonly "$db" "$q" </dev/null 2>/dev/null | head -n 1)
		fi
		marks=$(tail -c 1048576 "$p" 2>/dev/null | awk '
			/"payload":[{]"type":"(task_started|task_complete|turn_aborted)"/ { ev = substr($0, 1, 512) }
			/"type":"response_item"/ { ri = substr($0, 1, 512) }
			END { printf "%s\t%s", ev, ri }')
		l=$(head -n 1 "$p" 2>/dev/null)
		c=$(printf '%s\n' "$l" | grep -o '"cwd":"[^"]*"' | head -n 1)
		o=$(printf '%s\n' "$l" | grep -o '"originator":"[^"]*"' | head -n 1)
		printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$pid" "$et" "$t" "$st" "$marks" "$c" "$o" "$p"
	done
done
`

// collectStoppedScript lists the conversation logs and rollouts the stopped
// sessions are found from. It uses mtime, which collectLiveScript defines.
const collectStoppedScript = `echo '::section transcripts'
find "$HOME/.claude/projects" -mindepth 2 -maxdepth 2 -type f -name '*.jsonl' -mtime -7 2>/dev/null |
while IFS= read -r p; do
	t=$(mtime "$p" 2>/dev/null) && echo "$t $p"
done | sort -rn | head -n 100 | while read -r t s p; do
	c=$(grep -m 1 -o '"cwd":"[^"]*"' "$p" 2>/dev/null | head -n 1)
	printf '%s\t%s\t%s\t%s\n' "$t" "${p##*/}" "$c" "$s"
done
echo '::section codex-rollouts'
find "$HOME/.codex/sessions" -mindepth 4 -maxdepth 4 -type f -name 'rollout-*.jsonl' -mtime -7 2>/dev/null |
while IFS= read -r p; do
	t=$(mtime "$p" 2>/dev/null) && echo "$t $p"
done | sort -rn | while read -r t s p; do
	l=$(head -n 1 "$p" 2>/dev/null)
	o=$(printf '%s\n' "$l" | grep -o '"originator":"[^"]*"' | head -n 1)
	[ "$o" = '"originator":"codex-tui"' ] || continue
	c=$(printf '%s\n' "$l" | grep -o '"cwd":"[^"]*"' | head -n 1)
	printf '%s\t%s\t%s\t%s\t%s\n' "$t" "$s" "${p##*/}" "$c" "$o"
done | head -n 100
`

const collectEnd = `echo '::end'
exit 0
`

const (
	collectHeader    = "::panemux-tasks v1"
	directivePrefix  = "::"
	sectionState     = "state"
	sectionPS        = "ps"
	sectionTmux      = "tmux"
	sectionCWD       = "cwd"
	sectionEnv       = "env"
	sectionTranscrip = "transcripts"
	sectionCodexOpen = "codex-open"
	sectionCodexLogs = "codex-rollouts"
)

// rawSnapshot is one host's collection output, parsed but not interpreted.
type rawSnapshot struct {
	ProcessCWDs map[int]string
	// PaneIDs is the PANEMUX_PANE_ID each agent process was started with,
	// already limited to validPaneID.
	PaneIDs map[int]string
	// CodexAges is how long each codex process has run, in seconds.
	CodexAges   map[int]int64
	StateFiles  []stateFile
	Processes   []process
	TmuxPanes   []tmuxPane
	Transcripts []transcript
	// CodexOpen is every rollout a codex process holds open, and
	// CodexRollouts the rollouts under ~/.codex/sessions (issue #264).
	CodexOpen     []codexOpenRollout
	CodexRollouts []codexRollout
	Now           int64
}

type stateFile struct {
	Name string
	Data []byte
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

type transcript struct {
	SessionID string
	CWD       string
	ModTime   int64
	// Size is the log's size in bytes, 0 when the host did not report one.
	// With ModTime it tells whether the log changed since it was summarized.
	Size int64
}

// parseCollectOutput reads collectScript's output. It fails only when the
// output as a whole cannot be trusted — no header, no clock, no terminating
// "::end" (a dropped connection mid-run), or a directive it does not know.
// A malformed row inside a section is skipped: one odd `ps` line must not
// hide every task on the host.
func parseCollectOutput(out []byte) (rawSnapshot, error) {
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

	p := collectParser{raw: rawSnapshot{
		ProcessCWDs: map[int]string{},
		PaneIDs:     map[int]string{},
		CodexAges:   map[int]int64{},
	}}
	for _, line := range lines[start:] {
		var err error
		if strings.HasPrefix(line, directivePrefix) {
			err = p.directive(line)
		} else {
			err = p.raw.addRow(p.section, p.file, line)
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
	file     *stateFile
	section  string
	raw      rawSnapshot
	haveNow  bool
	complete bool
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
		switch arg {
		case sectionState, sectionPS, sectionTmux, sectionCWD, sectionEnv, sectionTranscrip,
			sectionCodexOpen, sectionCodexLogs:
			p.section = arg
		default:
			return fmt.Errorf("task collection output has unknown section %q", arg)
		}
	case "file":
		if p.section != sectionState {
			return errors.New("task collection output has a file outside the state section")
		}
		p.flush()
		p.file = &stateFile{Name: arg}
	case "end":
		p.flush()
		p.complete = true
	default:
		return fmt.Errorf("task collection output has unknown directive %q", line)
	}
	return nil
}

// flush closes the state file being read, if any.
func (p *collectParser) flush() {
	if p.file != nil {
		p.file.Data = []byte(strings.TrimSpace(string(p.file.Data)))
		p.raw.StateFiles = append(p.raw.StateFiles, *p.file)
		p.file = nil
	}
}

func (raw *rawSnapshot) addRow(section string, file *stateFile, line string) error {
	switch section {
	case sectionState:
		if file == nil {
			if strings.TrimSpace(line) == "" {
				return nil
			}
			return errors.New("task collection output has state data before a file name")
		}
		file.Data = append(file.Data, line...)
		file.Data = append(file.Data, '\n')
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
	case sectionTranscrip:
		if tr, ok := parseTranscriptRow(line); ok {
			raw.Transcripts = append(raw.Transcripts, tr)
		}
	case sectionCodexOpen:
		if pid, age, ok := parseCodexAgeRow(line); ok {
			raw.CodexAges[pid] = age
		} else if o, ok := parseCodexOpenRow(line); ok {
			raw.CodexOpen = append(raw.CodexOpen, o)
		}
	case sectionCodexLogs:
		if r, ok := parseCodexRolloutRow(line); ok {
			raw.CodexRollouts = append(raw.CodexRollouts, r)
		}
	default:
		if strings.TrimSpace(line) != "" {
			return errors.New("task collection output has data outside a section")
		}
	}
	return nil
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

func parseTranscriptRow(line string) (transcript, bool) {
	parts := strings.SplitN(line, "\t", 4)
	if len(parts) < 2 {
		return transcript{}, false
	}
	modTime, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return transcript{}, false
	}
	sessionID, ok := strings.CutSuffix(parts[1], ".jsonl")
	if !ok || !validSessionID.MatchString(sessionID) {
		return transcript{}, false
	}
	tr := transcript{ModTime: modTime, SessionID: sessionID}
	if len(parts) >= 3 {
		tr.CWD = transcriptCWD(parts[2])
	}
	// The cwd fragment is JSON, whose strings never hold a literal tab, so
	// a fourth field is always the size.
	if len(parts) == 4 {
		//mutation:exempt[CONDITIONALS_BOUNDARY] equivalent — a size of 0 is stored as 0 either way
		if size, err := strconv.ParseInt(parts[3], 10, 64); err == nil && size > 0 {
			tr.Size = size
		}
	}
	return tr, true
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
