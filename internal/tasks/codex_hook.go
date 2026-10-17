package tasks

import (
	"bytes"
	"context"
	_ "embed" // codexPermissionHook
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The codex permission hook a host's menu installs (issue #396): the script
// is embedded in panemux and written to ~/.local/bin, and one
// PermissionRequest entry for it is added to ~/.codex/hooks.json. Codex
// trusts a hook only after the person reviews it in its /hooks, which
// panemux neither does nor checks.

//go:embed codex_permission_hook.sh
var codexPermissionHook string

// codexHookFile is where the hook is installed, under the host's HOME.
const codexHookFile = ".local/bin/panemux-codex-permission-hook.sh"

// CodexHookStatus is whether a host's codex has panemux's permission hook.
type CodexHookStatus string

// Codex hook statuses. Absent is a host without codex, where there is
// nothing to install; Unknown is a host that could not be checked, which is
// never taken to mean absent.
const (
	CodexHookAbsent    CodexHookStatus = "absent"
	CodexHookMissing   CodexHookStatus = "missing"
	CodexHookInstalled CodexHookStatus = "installed"
	CodexHookOutdated  CodexHookStatus = "outdated"
	CodexHookInvalid   CodexHookStatus = "invalid"
	CodexHookUnknown   CodexHookStatus = "unknown"
)

// CodexHook is a host's codex hook status, and why for Invalid and Unknown.
type CodexHook struct {
	Status CodexHookStatus `json:"status"`
	Error  string          `json:"error,omitempty"`
}

// codexHookReadScript prints the host's HOME, the hook file and hooks.json,
// each as `::file <bytes> <x or ->` and its content, `::large <bytes>` for one
// over codexHookMaxRead, or `::none`.
const codexHookReadScript = `if [ ! -d "$HOME/.codex" ] && ! command -v codex >/dev/null 2>&1; then
	echo '::codex none'
	exit 0
fi
printf '::home %s\n' "$HOME"
for f in "$HOME/` + codexHookFile + `" "$HOME/.codex/hooks.json"; do
	if [ -f "$f" ]; then
		x=-
		[ -x "$f" ] && x=x
		n=$(wc -c <"$f" | tr -d ' ')
		if [ "$n" -gt 1048576 ]; then
			printf '::large %s\n' "$n"
		else
			printf '::file %s %s\n' "$n" "$x"
			cat "$f"
			echo
		fi
	else
		echo '::none'
	fi
done
echo '` + codexHookEnd + `'
`

const codexHookEnd = "::end"

// codexHookMaxRead is the largest file the check prints; a larger one is
// reported by its size alone. codexHookReadScript spells it out.
const codexHookMaxRead = 1 << 20

// codexHookWriteScript writes the hook, and hooks.json when %[2]s is "yes",
// each from a heredoc whose quoted terminator carries the random tag %[1]s,
// and ends with `::status <exit status>` after any error the writes printed.
// hooks.json is written to a temporary file first and copied over only once
// that succeeded, so a failed write leaves it as it was; it is copied in
// place, so a hooks.json that is a link keeps being one, with its mode.
const codexHookWriteScript = `tmp="$HOME/.codex/.hooks.json.panemux-%[1]s"
(
	set -e
	mkdir -p "$HOME/.local/bin"
	cat >"$HOME/` + codexHookFile + `" <<'PANEMUX_HOOK_%[1]s'
%[3]sPANEMUX_HOOK_%[1]s
	chmod 755 "$HOME/` + codexHookFile + `"
	if [ %[2]s = yes ]; then
		mkdir -p "$HOME/.codex"
		(umask 077 && cat >"$tmp") <<'PANEMUX_HOOKS_%[1]s'
%[4]sPANEMUX_HOOKS_%[1]s
		cat "$tmp" >"$HOME/.codex/hooks.json"
	fi
) 2>&1
status=$?
rm -f "$tmp"
echo "` + codexHookStatusLine + `$status"
`

const codexHookStatusLine = "::status "

// codexHookState is what a host's files say, before deciding a status.
type codexHookState struct {
	// err says why for Invalid and Unknown.
	err    error
	status CodexHookStatus
	// merged is hooks.json with the hook registered; registered is whether
	// it already was.
	merged     []byte
	registered bool
}

// CodexHook reports whether the host's codex has the permission hook. The
// only error is ErrUnknownHost; a host that cannot be checked is Unknown.
func (s *Service) CodexHook(ctx context.Context, host string) (CodexHook, error) {
	if err := s.checkHost(host); err != nil {
		return CodexHook{}, err
	}
	state := s.readCodexHook(ctx, host)
	got := CodexHook{Status: state.status}
	if state.err != nil {
		got.Error = state.err.Error()
	}
	return got, nil
}

// InstallCodexHook installs or updates the host's permission hook and
// returns the status read back afterwards. A host without codex, or whose
// hooks.json cannot be read as expected, is left untouched.
func (s *Service) InstallCodexHook(ctx context.Context, host string) (CodexHook, error) {
	if err := s.checkHost(host); err != nil {
		return CodexHook{}, err
	}
	state := s.readCodexHook(ctx, host)
	switch {
	case state.err != nil:
		return CodexHook{}, state.err
	case state.status == CodexHookAbsent:
		return CodexHook{}, fmt.Errorf("no codex on %s", hostName(host))
	case state.status == CodexHookInstalled:
		return CodexHook{Status: CodexHookInstalled}, nil
	}
	tag, err := newHeredocTag(s.opts.Rand)
	if err != nil {
		return CodexHook{}, err
	}
	// A registered hook leaves hooks.json alone, so the host's own file
	// never goes into a heredoc: what does is registerCodexHook's indented
	// JSON, in which no line can be a bare terminator.
	writeJSON, hooksJSON := "yes", state.merged
	if state.registered {
		writeJSON, hooksJSON = "no", nil
	}
	script := fmt.Sprintf(codexHookWriteScript, tag, writeJSON, codexPermissionHook, hooksJSON)
	runCtx, cancel := context.WithTimeout(ctx, s.opts.HostTimeout)
	defer cancel()
	out, err := s.runHostScript(runCtx, host, script, "codex hook install")
	if err != nil {
		return CodexHook{}, err
	}
	if err := codexHookWriteError(out); err != nil {
		return CodexHook{}, fmt.Errorf("install the codex hook on %s: %w", hostName(host), err)
	}
	return s.CodexHook(ctx, host)
}

// codexHookWriteError reads codexHookWriteScript's output: nil when it
// exited 0, otherwise what the failed write printed.
func codexHookWriteError(out []byte) error {
	text := strings.TrimRight(string(out), "\n")
	i := strings.LastIndex(text, codexHookStatusLine)
	if i < 0 || (i > 0 && text[i-1] != '\n') {
		return errors.New("its output ended early")
	}
	status := text[i+len(codexHookStatusLine):]
	if status == "0" {
		return nil
	}
	if reason := strings.TrimSpace(text[:i]); reason != "" {
		return errors.New(reason)
	}
	return fmt.Errorf("exit status %s", status)
}

func (s *Service) readCodexHook(ctx context.Context, host string) codexHookState {
	ctx, cancel := context.WithTimeout(ctx, s.opts.HostTimeout)
	defer cancel()
	out, err := s.runHostScript(ctx, host, codexHookReadScript, "codex hook check")
	if errors.Is(err, errConnecting) {
		err = fmt.Errorf("%s is still connecting", hostName(host))
	}
	if err != nil {
		return codexHookState{status: CodexHookUnknown, err: err}
	}
	home, files, err := parseCodexHookOutput(out)
	if err != nil {
		return codexHookState{status: CodexHookUnknown, err: err}
	}
	if files == nil {
		return codexHookState{status: CodexHookAbsent}
	}
	hook, hooksJSON := files[0], files[1]
	if hooksJSON.large {
		return codexHookState{status: CodexHookInvalid, err: errors.New("~/.codex/hooks.json is larger than 1 MiB")}
	}
	merged, registered, err := registerCodexHook(hooksJSON.data, home+"/"+codexHookFile)
	if err != nil {
		return codexHookState{status: CodexHookInvalid, err: fmt.Errorf("~/.codex/hooks.json %w", err)}
	}
	state := codexHookState{merged: merged, registered: registered}
	switch {
	case hook.data == nil && !hook.large && !registered:
		state.status = CodexHookMissing
	case string(hook.data) == codexPermissionHook && hook.executable && registered:
		state.status = CodexHookInstalled
	default:
		state.status = CodexHookOutdated
	}
	return state
}

// codexHookFileContent is one file codexHookReadScript printed; data is nil
// for a file that does not exist or is large.
type codexHookFileContent struct {
	data       []byte
	executable bool
	large      bool
}

var errCodexHookOutput = errors.New("codex hook check output ended early")

// parseCodexHookOutput reads codexHookReadScript's output: nil files for a
// host without codex, otherwise the hook file and hooks.json. Anything a
// login shell printed before the first marker is skipped.
func parseCodexHookOutput(out []byte) (string, []codexHookFileContent, error) {
	for !bytes.HasPrefix(out, []byte("::codex none\n")) && !bytes.HasPrefix(out, []byte("::home ")) {
		var ok bool
		if _, out, ok = bytes.Cut(out, []byte("\n")); !ok {
			return "", nil, errCodexHookOutput
		}
	}
	if bytes.HasPrefix(out, []byte("::codex none\n")) {
		return "", nil, nil
	}
	line, out, ok := bytes.Cut(out, []byte("\n"))
	if !ok {
		return "", nil, errCodexHookOutput
	}
	home := strings.TrimPrefix(string(line), "::home ")
	files := make([]codexHookFileContent, 0, 2)
	for range 2 {
		line, out, ok = bytes.Cut(out, []byte("\n"))
		if !ok {
			return "", nil, errCodexHookOutput
		}
		if string(line) == "::none" {
			files = append(files, codexHookFileContent{})
			continue
		}
		if bytes.HasPrefix(line, []byte("::large ")) {
			files = append(files, codexHookFileContent{large: true})
			continue
		}
		size, mode, _ := strings.Cut(strings.TrimPrefix(string(line), "::file "), " ")
		n, err := strconv.Atoi(size)
		if err != nil || n < 0 || len(out) <= n || out[n] != '\n' {
			return "", nil, errCodexHookOutput
		}
		files = append(files, codexHookFileContent{data: out[:n:n], executable: mode == "x"})
		out = out[n+1:]
	}
	if string(bytes.TrimSpace(out)) != codexHookEnd {
		return "", nil, errCodexHookOutput
	}
	return home, files, nil
}

// registerCodexHook returns hooks.json (nil for none) with a
// PermissionRequest entry for command, and whether one was there already.
// Every other key and entry is kept as it was written; only the layout
// changes. A file that is not the shape codex reads is an error.
func registerCodexHook(data []byte, command string) ([]byte, bool, error) {
	var top map[string]json.RawMessage
	if data != nil {
		if err := json.Unmarshal(data, &top); err != nil {
			var syntax *json.SyntaxError
			if errors.As(err, &syntax) {
				return nil, false, errors.New("is not valid JSON")
			}
			return nil, false, errors.New("is not a JSON object")
		}
	}
	var hooks map[string]json.RawMessage
	if raw, ok := top["hooks"]; ok && json.Unmarshal(raw, &hooks) != nil {
		return nil, false, errors.New(`is not as codex reads it: "hooks" is not an object`)
	}
	var groups []json.RawMessage
	if raw, ok := hooks["PermissionRequest"]; ok && json.Unmarshal(raw, &groups) != nil {
		return nil, false, errors.New(`is not as codex reads it: "hooks.PermissionRequest" is not a list`)
	}
	if codexHookRegistered(groups, command) {
		return data, true, nil
	}
	type commandHook struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	type hookGroup struct {
		Hooks []commandHook `json:"hooks"`
	}
	permission := make([]any, 0, len(groups)+1)
	for _, raw := range groups {
		permission = append(permission, raw)
	}
	permission = append(permission, hookGroup{[]commandHook{{Type: "command", Command: command, Timeout: 1}}})
	hooksOut := map[string]any{"PermissionRequest": permission}
	for key, raw := range hooks {
		if key != "PermissionRequest" {
			hooksOut[key] = raw
		}
	}
	topOut := map[string]any{"hooks": hooksOut}
	for key, raw := range top {
		if key != "hooks" {
			topOut[key] = raw
		}
	}
	// Encode, not MarshalIndent, so that a command's & < > stay as written.
	var merged bytes.Buffer
	enc := json.NewEncoder(&merged)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	//coverage:exempt every value is JSON that json.Unmarshal accepted, or strings and a number
	if err := enc.Encode(topOut); err != nil {
		return nil, false, fmt.Errorf("encode hooks.json: %w", err)
	}
	return merged.Bytes(), false, nil
}

// codexHookRegistered reports whether a PermissionRequest group runs
// command. A group of another shape is not panemux's and is skipped.
func codexHookRegistered(groups []json.RawMessage, command string) bool {
	for _, raw := range groups {
		var group struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		}
		if json.Unmarshal(raw, &group) != nil {
			continue
		}
		for _, h := range group.Hooks {
			if h.Command == command {
				return true
			}
		}
	}
	return false
}
