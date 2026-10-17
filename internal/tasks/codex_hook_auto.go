package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"panemux/internal/fileops"
	"panemux/internal/homedir"
)

// Updating a host's codex permission hook by itself (issue #402). On a host
// whose menu turned it on, a check that finds the hook differs from the one
// this panemux embeds (Update) runs Update: the check when the host's menu
// opens, and one made after panemux starts, on the host's first successful
// collection. A host without the hook is never installed to, and each host
// is updated by itself at most once per process, whatever came of it;
// Update in the menu still works.

const (
	codexHookAutoFileName    = "codex-hook.json"
	codexHookAutoFileVersion = 1
	codexHookAutoFileMode    = 0o600
)

// CodexHookAutoStore keeps the hosts whose hook is updated by itself, in
// ~/.config/panemux/codex-hook.json. It reads the file on every use; a file
// it cannot read is an error and is never written over.
type CodexHookAutoStore struct {
	path string
	mu   sync.Mutex
}

type codexHookAutoFile struct {
	AutoUpdate []string `json:"auto_update"`
	Version    int      `json:"version"`
}

// NewCodexHookAutoStore returns a store for the file at path, or for
// ~/.config/panemux/codex-hook.json when path is "".
func NewCodexHookAutoStore(path string) *CodexHookAutoStore {
	return &CodexHookAutoStore{path: path}
}

// Enabled reports whether host's hook is updated by itself.
func (s *CodexHookAutoStore) Enabled(host string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hosts, err := s.readLocked()
	return slices.Contains(hosts, host), err
}

// Set turns updating host's hook by itself on or off.
func (s *CodexHookAutoStore) Set(host string, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	hosts, err := s.readLocked()
	if err != nil {
		return err
	}
	hosts = slices.DeleteFunc(hosts, func(h string) bool { return h == host })
	if on {
		hosts = append(hosts, host)
	}
	slices.Sort(hosts)
	file := codexHookAutoFile{Version: codexHookAutoFileVersion, AutoUpdate: append([]string{}, hosts...)}
	data, err := json.Marshal(file)
	//coverage:exempt a version and a list of strings always marshal
	if err != nil {
		return fmt.Errorf("encoding codex hook settings file: %w", err)
	}
	return fileops.AtomicWrite(resolveRecordsWriteTarget(s.path), data, codexHookAutoFileMode, "codex hook settings file")
}

func (s *CodexHookAutoStore) readLocked() ([]string, error) {
	if s.path == "" {
		home, err := homedir.Dir()
		if err != nil {
			return nil, fmt.Errorf("getting home directory: %w", err)
		}
		s.path = filepath.Join(home, ".config", "panemux", codexHookAutoFileName)
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading codex hook settings file: %w", err)
	}
	var file codexHookAutoFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing codex hook settings file: %w", err)
	}
	if file.Version != codexHookAutoFileVersion {
		return nil, fmt.Errorf("unsupported codex hook settings file version %d", file.Version)
	}
	return file.AutoUpdate, nil
}

// codexHookAuto is what this process did by itself about one host's hook;
// Service.mu guards it.
type codexHookAuto struct {
	// err is why the update failed; updated is that it succeeded.
	err string
	// checked is that the check after start was made; tried that an update
	// was.
	checked, tried, updated bool
}

// SetCodexHookAuto turns updating host's hook by itself on or off.
func (s *Service) SetCodexHookAuto(host string, on bool) error {
	if err := s.checkHost(host); err != nil {
		return err
	}
	if s.opts.CodexHookAuto == nil {
		return errors.New("codex hook settings are not kept")
	}
	return s.opts.CodexHookAuto.Set(host, on)
}

// withCodexHookAuto adds to got whether the host's hook is updated by itself
// and what came of doing so.
func (s *Service) withCodexHookAuto(host string, got CodexHook) CodexHook {
	if s.opts.CodexHookAuto == nil {
		return got
	}
	on, err := s.opts.CodexHookAuto.Enabled(host)
	if err != nil {
		got.AutoError = err.Error()
		return got
	}
	s.mu.Lock()
	auto := s.codexHookAuto[host]
	s.mu.Unlock()
	got.AutoUpdate, got.AutoUpdated, got.AutoError = on, auto.updated, auto.err
	return got
}

// autoUpdateCodexHook updates the hook got describes when the host has it
// turned on, it shows Update, and this process has not tried already.
func (s *Service) autoUpdateCodexHook(ctx context.Context, host string, got CodexHook) CodexHook {
	got = s.withCodexHookAuto(host, got)
	if !got.AutoUpdate || got.Status != CodexHookOutdated {
		return got
	}
	s.mu.Lock()
	auto := s.codexHookAuto[host]
	if auto.tried {
		s.mu.Unlock()
		return got
	}
	auto.tried = true
	s.codexHookAuto[host] = auto
	s.mu.Unlock()

	updated, err := s.installCodexHook(ctx, host)
	s.mu.Lock()
	auto = s.codexHookAuto[host]
	auto.updated, auto.err = err == nil, ""
	if err != nil {
		auto.err = err.Error()
		s.opts.Logf("codex hook: updating it by itself on %s failed: %v", hostName(host), err)
	} else {
		got = updated
		s.opts.Logf("codex hook: updated it by itself on %s", hostName(host))
	}
	s.codexHookAuto[host] = auto
	s.mu.Unlock()
	return s.withCodexHookAuto(host, got)
}

// forgetCodexHookAuto drops what came of updating host's hook by itself,
// once it has been updated by hand.
func (s *Service) forgetCodexHookAuto(host string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	auto := s.codexHookAuto[host]
	auto.updated, auto.err = false, ""
	s.codexHookAuto[host] = auto
}

// checkCodexHookAfterStart makes, in the background, the check after start
// on a host that has updating by itself turned on: once per host, after its
// first successful collection.
func (s *Service) checkCodexHookAfterStart(host string) {
	if s.opts.CodexHookAuto == nil {
		return
	}
	s.mu.Lock()
	auto := s.codexHookAuto[host]
	first := !auto.checked
	auto.checked = true
	s.codexHookAuto[host] = auto
	s.mu.Unlock()
	if !first {
		return
	}
	if on, err := s.opts.CodexHookAuto.Enabled(host); err != nil || !on {
		return
	}
	s.summaryWG.Add(1)
	go func() {
		defer s.summaryWG.Done()
		s.autoUpdateCodexHook(s.summaryCtx, host, s.checkCodexHook(s.summaryCtx, host))
	}()
}
