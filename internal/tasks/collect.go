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
//	::section transcripts        "<mtime>\t<file name>\t<first "cwd":"..." in it>\t<size in bytes>"
//	::end                        the output is complete
//
// Transcripts are the conversation logs directly under ~/.claude/projects/*/
// modified in the last 7 days, newest first, at most 100 of them. The window
// and the count are the range decided for stopped sessions (issue #252); the
// Go side keeps the stopped ones and caps them at maxStoppedTasks.
const collectScript = `LC_ALL=C
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
echo '::section cwd'
ps -U "$uid" -o pid=,command= 2>/dev/null | awk '($2 " " $3) ~ /claude|codex/ { print $1 }' |
while read -r pid; do
	c=$(readlink "/proc/$pid/cwd" 2>/dev/null ||
		lsof -a -p "$pid" -d cwd -Fn 2>/dev/null | awk '/^n/ { print substr($0, 2); exit }')
	if [ -n "$c" ]; then echo "$pid $c"; fi
done
echo '::section transcripts'
if stat -c %Y / >/dev/null 2>&1; then
	mtime() { stat -c '%Y %s' "$1"; }
else
	mtime() { stat -f '%m %z' "$1"; }
fi
find "$HOME/.claude/projects" -mindepth 2 -maxdepth 2 -type f -name '*.jsonl' -mtime -7 2>/dev/null |
while IFS= read -r p; do
	t=$(mtime "$p" 2>/dev/null) && echo "$t $p"
done | sort -rn | head -n 100 | while read -r t s p; do
	c=$(grep -m 1 -o '"cwd":"[^"]*"' "$p" 2>/dev/null | head -n 1)
	printf '%s\t%s\t%s\t%s\n' "$t" "${p##*/}" "$c" "$s"
done
echo '::end'
exit 0
`

const (
	collectHeader    = "::panemux-tasks v1"
	directivePrefix  = "::"
	sectionState     = "state"
	sectionPS        = "ps"
	sectionTmux      = "tmux"
	sectionCWD       = "cwd"
	sectionTranscrip = "transcripts"
)

// rawSnapshot is one host's collection output, parsed but not interpreted.
type rawSnapshot struct {
	ProcessCWDs map[int]string
	StateFiles  []stateFile
	Processes   []process
	TmuxPanes   []tmuxPane
	Transcripts []transcript
	Now         int64
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

	p := collectParser{raw: rawSnapshot{ProcessCWDs: map[int]string{}}}
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
		case sectionState, sectionPS, sectionTmux, sectionCWD, sectionTranscrip:
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
	case sectionTranscrip:
		if tr, ok := parseTranscriptRow(line); ok {
			raw.Transcripts = append(raw.Transcripts, tr)
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
