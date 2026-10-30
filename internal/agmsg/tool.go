package agmsg

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"panemux/internal/host"
	"panemux/internal/hosttools"
)

const updateFlag = "--update"

const TestedAgmsgCommit = "3c537a51bb76a23e312e7c27770df4a58ba794ff"

// The marker matches v1.2.0 install.sh's own alternate-install discovery.
const toolProbe = `for d in "$HOME"/.agents/skills/*/; do
  if [ -f "${d}.agmsg" ] && [ "${d%/}" != "$HOME/.agents/skills/agmsg" ]; then
    printf 'alternate\n'; exit 0
  fi
done
if [ ! -f "$HOME/.agents/skills/agmsg/scripts/api.sh" ]; then
  printf 'missing\n'
else
  printf 'installed\n'
  cat "$HOME/.agents/skills/agmsg/VERSION" 2>/dev/null || true
fi`

const targetPrerequisites = `missing=0
for cmd in bash sqlite3 tar mktemp node; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    printf 'Missing %s; install it on this host.\n' "$cmd"
    missing=1
  fi
done
exit "$missing"`

// Only the mktemp directory created by this invocation is removed. Archive
// bytes arrive on stdin; bash reads install.sh and never consumes that stream.
const installerScript = `set -eu
case "$1" in ''|--update) ;; *) exit 2 ;; esac
for d in "$HOME"/.agents/skills/*/; do
  if [ -f "${d}.agmsg" ] && [ "${d%/}" != "$HOME/.agents/skills/agmsg" ]; then
    echo 'Another named agmsg installation exists; consolidate it before installing or updating.' >&2
    exit 1
  fi
done
tmp=$(mktemp -d)
trap 'rm -r -- "$tmp"' EXIT
trap 'exit 1' HUP INT TERM
tar -xf - -C "$tmp"
cd "$tmp"
if [ "$1" = '--update' ]; then
  bash ./install.sh --update </dev/null
else
  bash ./install.sh </dev/null
fi`

type ToolOptions struct {
	Execution host.Executor
	Input     host.InputExecutor
	Archive   func(context.Context) ([]byte, error)
}

type Tool struct{ opts ToolOptions }

func NewTool(opts ToolOptions) *Tool {
	if opts.Archive == nil {
		source := &Source{}
		opts.Archive = source.Archive
	}
	return &Tool{opts: opts}
}

func (t *Tool) Check(ctx context.Context, h host.Host) hosttools.State {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := t.opts.Execution.Run(ctx, h, toolProbe)
	if err != nil {
		return hosttools.State{Status: hosttools.Unknown, Error: err.Error()}
	}
	lines := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)
	switch lines[0] {
	case "alternate":
		return hosttools.State{Status: hosttools.Invalid,
			Error: "Another named agmsg installation exists; consolidate it before installing or updating."}
	case "missing":
		return hosttools.State{Status: hosttools.Missing}
	case "installed":
		version := ""
		if len(lines) == 2 {
			version = strings.TrimSpace(lines[1])
		}
		got, ok := parseAgmsgRelease(version)
		if !ok {
			return hosttools.State{Status: hosttools.Unknown, Version: version,
				Error: "Cannot compare the installed agmsg version; check its VERSION file."}
		}
		want, _ := parseAgmsgRelease(TestedAgmsgVersion)
		return hosttools.State{Status: toolReleaseStatus(got, want), Version: version}
	default:
		return hosttools.State{Status: hosttools.Unknown, Error: "Unrecognized agmsg probe response."}
	}
}

func (t *Tool) Prerequisites(h host.Host) []hosttools.Prerequisite {
	return []hosttools.Prerequisite{
		{Name: "target host", Check: func(ctx context.Context) error {
			out, err := t.opts.Input.RunInput(ctx, h, targetPrerequisites, nil)
			if err != nil {
				return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
			}
			return nil
		}},
		{Name: "pinned agmsg source (git / github.com or verified cache)", Check: func(ctx context.Context) error {
			_, err := t.opts.Archive(ctx)
			return err
		}},
	}
}

func (t *Tool) Install(ctx context.Context, h host.Host) (hosttools.State, error) {
	return t.install(ctx, h, false)
}

// Update uses the same installer path but cannot turn into a missing-copy Install.
func (t *Tool) Update(ctx context.Context, h host.Host) (hosttools.State, error) {
	return t.install(ctx, h, true)
}

func (t *Tool) install(ctx context.Context, h host.Host, updateOnly bool) (hosttools.State, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	state := t.Check(ctx, h)
	mode := ""
	switch state.Status {
	case hosttools.Missing:
		if updateOnly {
			return state, fmt.Errorf("agmsg cannot be updated (%s): installation is missing", state.Status)
		}
	case hosttools.Outdated:
		mode = updateFlag
	default:
		return state, fmt.Errorf("agmsg cannot be installed or updated (%s): %s", state.Status, state.Error)
	}
	archive, err := t.opts.Archive(ctx)
	if err != nil {
		return state, fmt.Errorf("get pinned agmsg source: %w", err)
	}
	out, err := t.opts.Input.RunInput(ctx, h, installerScript, bytes.NewReader(archive), mode)
	if err != nil {
		return state, fmt.Errorf("agmsg installer: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return t.Check(ctx, h), nil
}

func toolReleaseStatus(got, want agmsgRelease) hosttools.Status {
	status := hosttools.Installed
	if got.major < want.major ||
		got.major == want.major && (got.minor < want.minor || got.minor == want.minor && got.patch < want.patch) {

		status = hosttools.Outdated
	}
	if got.major > want.major ||
		got.major == want.major && (got.minor > want.minor || got.minor == want.minor && got.patch > want.patch) {

		status = hosttools.Newer
	}
	return status
}
