package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// BrowserOpenOSCIdent is the private OSC identifier the browser shim uses to
// hand a URL back to the panemux frontend. The frontend consumes the sequence
// with xterm's parser.registerOscHandler, so it never reaches the screen.
// Keep it in sync with BROWSER_OPEN_OSC_IDENT in the frontend.
const BrowserOpenOSCIdent = 7373

const (
	browserShimPrimaryName = "panemux-open"
	// browserShimCacheSubdir is appended to the user cache directory
	// locally, and to $HOME/.cache remotely.
	browserShimCacheSubdir = "panemux/bin"
)

// browserShimAliases shadow the openers a CLI reaches for when it ignores
// $BROWSER. Both fall through to the real binary for anything that is not a
// bare http(s) URL, so `xdg-open file.pdf` and `open -a Safari …` behave
// exactly as they would without panemux.
var browserShimAliases = []string{"xdg-open", "open"}

// browserShimScript is a fixed, non-tainted POSIX shell script: no
// caller-supplied value is ever interpolated into it. It turns a
// browser-open request made on the pane's host into an OSC sequence written
// to the pane's terminal, which reaches panemux over the PTY stream that
// already exists. See docs/security.md "Browser-open interception".
const browserShimScript = `#!/bin/sh
# panemux browser shim. A browser-open request made on this host is handed to
# the browser showing the panemux dashboard instead of being opened here.
if [ "$#" -eq 1 ]; then
	case "$1" in
	http://*|https://*)
		if printf '\033]7373;panemux-open;%s\a' "$1" 2>/dev/null >"${PANEMUX_SHIM_TTY:-/dev/tty}"; then
			exit 0
		fi
		;;
	esac
fi

# Anything else must behave exactly as it would without panemux.
name=${0##*/}
if [ "$name" = "panemux-open" ]; then
	name=xdg-open
fi
if [ -n "${PANEMUX_SHIM_FALLBACK_PATH:-}" ]; then
	PATH=$PANEMUX_SHIM_FALLBACK_PATH
	export PATH
fi
target=$(command -v "$name" 2>/dev/null) || exit 0
[ -n "$target" ] || exit 0
case "$target" in
"${0%/*}"/*) exit 0 ;;
esac
exec "$target" "$@"
`

// browserShimEnabled gates the shim. It is process-wide because the session
// factory builds sessions from pane config alone; main.go sets it once from
// url_open.browser_shim. Default on.
var browserShimEnabled atomic.Bool

func init() {
	browserShimEnabled.Store(true)
}

// SetBrowserShimEnabled turns browser-open interception on or off for every
// session created afterwards.
func SetBrowserShimEnabled(enabled bool) {
	browserShimEnabled.Store(enabled)
}

// BrowserShimEnabled reports whether new sessions install the browser shim.
func BrowserShimEnabled() bool {
	return browserShimEnabled.Load()
}

var userCacheDirFn = os.UserCacheDir

// installLocalBrowserShim writes the shim and its aliases into the user cache
// directory and returns the directory holding them.
func installLocalBrowserShim() (string, error) {
	cacheDir, err := userCacheDirFn()
	if err != nil {
		return "", fmt.Errorf("resolving user cache directory: %w", err)
	}
	dir := filepath.Join(cacheDir, filepath.FromSlash(browserShimCacheSubdir))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating shim directory: %w", err)
	}
	primary := filepath.Join(dir, browserShimPrimaryName)
	if err := writeShimFile(primary); err != nil {
		return "", err
	}
	for _, alias := range browserShimAliases {
		if err := writeShimFile(filepath.Join(dir, alias)); err != nil {
			return "", err
		}
	}
	return dir, nil
}

func writeShimFile(path string) error {
	// Written restrictively first, then made executable for its owner only:
	// the shim has to be runnable, but no wider than the user's own cache
	// directory it lives in (created 0700 above).
	if err := os.WriteFile(path, []byte(browserShimScript), 0o600); err != nil {
		return fmt.Errorf("writing browser shim: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("making browser shim executable: %w", err)
	}
	return nil
}

// localBrowserShimEnv returns the environment entries that point a local
// pane's shell at the shim, keeping the pre-panemux PATH available so the
// shim can fall through to the real opener.
func localBrowserShimEnv(dir, currentPath string) []string {
	env := []string{
		"BROWSER=" + filepath.Join(dir, browserShimPrimaryName),
		"PANEMUX_SHIM_FALLBACK_PATH=" + currentPath,
	}
	if currentPath == "" {
		return append(env, "PATH="+dir)
	}
	return append(env, "PATH="+dir+string(os.PathListSeparator)+currentPath)
}

// browserShimEnvForLocalSession installs the shim and returns the environment
// entries for a local pane. Failures are not fatal: a pane must still start
// when the cache directory is unavailable, just without interception.
func browserShimEnvForLocalSession() ([]string, error) {
	dir, err := installLocalBrowserShim()
	if err != nil {
		return nil, err
	}
	return localBrowserShimEnv(dir, os.Getenv("PATH")), nil
}

// remoteBrowserShimSetup returns the shell snippet that installs the shim on
// an SSH host and exports the pointers to it. Every step is best-effort: a
// read-only home directory must leave the pane working, just without
// interception. The script text is a fixed literal, quoted with the same
// discipline every other remote argument uses.
func remoteBrowserShimSetup() string {
	dir := `"$HOME/.cache/` + browserShimCacheSubdir + `"`
	primary := `"$PANEMUX_SHIM_DIR/` + browserShimPrimaryName + `"`

	var b strings.Builder
	fmt.Fprintf(&b, "PANEMUX_SHIM_DIR=%s; ", dir)
	b.WriteString("{ mkdir -p \"$PANEMUX_SHIM_DIR\"")
	fmt.Fprintf(&b, " && printf %%s %s > %s", shellQuotePath(browserShimScript), primary)
	fmt.Fprintf(&b, " && chmod 700 %s", primary)
	for _, alias := range browserShimAliases {
		fmt.Fprintf(&b, " && ln -sf %s \"$PANEMUX_SHIM_DIR/%s\"", browserShimPrimaryName, alias)
	}
	b.WriteString("; } >/dev/null 2>&1; ")
	fmt.Fprintf(&b, "if [ -x %s ]; then ", primary)
	b.WriteString("PANEMUX_SHIM_FALLBACK_PATH=\"$PATH\"; export PANEMUX_SHIM_FALLBACK_PATH; ")
	fmt.Fprintf(&b, "BROWSER=%s; export BROWSER; ", primary)
	b.WriteString("PATH=\"$PANEMUX_SHIM_DIR:$PATH\"; export PATH; fi; ")
	return b.String()
}

// remoteLoginShellExec is the tail of a remote command that starts the user's
// login shell. `sess.Start` runs its command through `$SHELL -c`, which is
// neither interactive nor a login shell, so the profile files an interactive
// SSH login would source are skipped unless the replacement shell is started
// with -l. Only shells whose -l flag is unambiguous get it; anything else
// keeps the plain exec this codebase already uses for panes with a cwd.
const remoteLoginShellExec = `case "${SHELL##*/}" in
bash|zsh|fish) exec "$SHELL" -l ;;
"") exec /bin/sh ;;
*) exec "$SHELL" ;;
esac`
