package taskhook

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LaunchScope is evidence supplied by the trusted managed launcher, not claims
// accepted from PermissionRequest. A user turn's reviewer field alone cannot
// establish this scope. Unknown routing must leave these attestations false.
type LaunchScope struct {
	CodexVersion string `json:"codex_version"`
	NoDaemon     bool   `json:"no_daemon"`
	UserReviewer bool   `json:"user_reviewer"`
	NoStrict     bool   `json:"no_strict"`
	PassiveHooks bool   `json:"passive_hooks"`
}

func (s LaunchScope) Supported() bool {
	return s.CodexVersion == "0.160.0" && s.NoDaemon && s.UserReviewer && s.NoStrict && s.PassiveHooks
}

// Registration is immutable for a launch generation. ExecutableID names the
// native executable file identity checked by the launcher before exec. The
// sessions root is trusted host metadata, never a stdin transcript path.
type Registration struct {
	SessionsRoot string      `json:"sessions_root"`
	ExecutableID string      `json:"executable_id"`
	Scope        LaunchScope `json:"scope"`
	Owner        Owner       `json:"owner"`
	Version      int         `json:"version"`
}

type Registry struct{ store *Store }

func NewRegistry(root string) *Registry { return &Registry{store: NewStore(root)} }

func validRegistration(r Registration) bool {
	return r.Version == ProtocolVersion && validOwner(r.Owner) && opaque.MatchString(r.ExecutableID) &&
		opaque.MatchString(r.Scope.CodexVersion) && filepath.IsAbs(r.SessionsRoot) &&
		filepath.Clean(r.SessionsRoot) == r.SessionsRoot && len(r.SessionsRoot) <= 4096 &&
		!strings.ContainsAny(r.SessionsRoot, "\x00\r\n")
}

func readRegistration(d recordFiles, generation string) (Registration, error) {
	data, err := d.Read(generation+".owner.json", 8<<10)
	if errors.Is(err, os.ErrNotExist) {
		return Registration{}, os.ErrNotExist
	}
	if err != nil {
		return Registration{}, ErrStorage
	}
	var r Registration
	if !strictJSON(data, 8<<10, &r) || !validRegistration(r) || r.Owner.Generation != generation {
		return Registration{}, ErrCorrupt
	}
	return r, nil
}

func (r *Registry) Read(generation string) (Registration, error) {
	if !generationID.MatchString(generation) {
		return Registration{}, ErrInput
	}
	d, err := r.store.open(r.store.root)
	if err != nil {
		return Registration{}, ErrStorage
	}
	defer func() { _ = d.Close() }()
	return readRegistration(d, generation)
}

// Register neither creates roots nor repairs old records. The caller verifies
// the live launch UID/PID/birth, executable and scope before supplying them.
// Generation collisions, corrupt files and attempts to switch roots fail closed.
func (r *Registry) Register(ctx context.Context, entry Registration) error {
	if !validRegistration(entry) {
		return ErrInput
	}
	d, err := r.store.open(r.store.root)
	if err != nil {
		return ErrStorage
	}
	defer func() { _ = d.Close() }()
	release, err := d.Lock(ctx, 100*time.Millisecond)
	if err != nil {
		return ErrStorage
	}
	defer release()
	old, err := readRegistration(d, entry.Owner.Generation)
	if err == nil {
		if old == entry {
			return nil
		}
		return ErrConflict
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if ctx.Err() != nil {
		return ErrStorage
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return ErrInput
	}
	if d.Replace(entry.Owner.Generation+".owner.json", data) != nil || ctx.Err() != nil {
		return ErrStorage
	}
	return nil
}
