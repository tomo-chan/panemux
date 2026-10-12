package taskhook

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"panemux/internal/fileops"
)

const ContextDomain = "panemux.task-hooks.context/v1"

var machineIdentity = regexp.MustCompile(`^(linux-machine-id:[0-9a-f]{32}|darwin-platform-uuid:[0-9a-f-]{36})$`)

// TargetIdentity is install.json's immutable target schema. AccountHome locates
// the fixed base; aliases and XDG overrides are not identity inputs. The fixed
// launcher obtains UID/home from the account database and machine from the OS.
type TargetIdentity struct {
	Context     string `json:"context_id"`
	Machine     string `json:"machine_identity"`
	AccountHome string `json:"account_home"`
	CodexHome   string `json:"codex_home"`
	Version     int    `json:"version"`
	UID         int    `json:"uid"`
}

func identityPath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && len(path) <= 4096 && utf8.ValidString(path) &&
		!strings.ContainsAny(path, "\x00\r\n")
}

// ContextID hashes four UTF-8 fields, each followed by NUL: domain, machine
// identity, canonical decimal effective UID and canonical CODEX_HOME. Full
// SHA256 is retained. Consumers also compare the full target to reject a
// collision or a mismatched schema rather than trusting the directory name.
func ContextID(t TargetIdentity) (string, error) {
	if t.Version != 1 || t.UID < 0 || !machineIdentity.MatchString(t.Machine) ||
		!identityPath(t.AccountHome) || !identityPath(t.CodexHome) {

		return "", ErrInput
	}
	sum := sha256.Sum256([]byte(ContextDomain + "\x00" + t.Machine + "\x00" +
		strconv.Itoa(t.UID) + "\x00" + t.CodexHome + "\x00"))
	return "ctx1-" + hex.EncodeToString(sum[:]), nil
}

type TargetPaths struct{ Context, State, Helper string }

func ContextPaths(t TargetIdentity) (TargetPaths, error) {
	id, err := ContextID(t)
	if err != nil {
		return TargetPaths{}, err
	}
	root := filepath.Join(t.AccountHome, ".local", "state", "panemux", "task-hooks", "contexts", id)
	return TargetPaths{Context: root, State: filepath.Join(root, "state"),
		Helper: filepath.Join(root, "bin", "panemux-task-hook")}, nil
}

func validateInstalledTarget(root string, t TargetIdentity) error {
	paths, err := ContextPaths(t)
	if err != nil || paths.State != root {
		return ErrInput
	}
	id, _ := ContextID(t)
	t.Context = id
	// Ancestors and the helper itself are checked by the fixed host launcher
	// before execution. Opening context/state repeats private FD checks here.
	d, err := fileops.OpenPrivateDir(paths.Context)
	if err != nil {
		return ErrStorage
	}
	defer func() { _ = d.Close() }()
	data, err := d.Read("install.json", 32<<10)
	if err != nil {
		return ErrStorage
	}
	var fields map[string]json.RawMessage
	if !strictJSON(data, 32<<10, &fields) {
		return ErrCorrupt
	}
	var installed TargetIdentity
	var protocol int
	if !strictJSON(fields["target"], 8<<10, &installed) || installed != t ||
		json.Unmarshal(fields["helper_protocol"], &protocol) != nil || protocol != ProtocolVersion {

		return ErrConflict
	}
	s, err := fileops.OpenPrivateDir(root)
	if err != nil {
		return ErrStorage
	}
	_ = s.Close()
	return nil
}
