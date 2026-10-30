package agmsg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"panemux/internal/cachedir"
	"panemux/internal/hostexec"
)

const sourceURL = "https://github.com/fujibee/agmsg.git"

// Source keeps one bare repository per tested commit. Git checks object hashes;
// only an exact commit match may be archived. No checkout hooks or installer run
// on the panemux host during acquisition.
type Source struct {
	RunGit func(context.Context, ...string) ([]byte, []byte, error)
	mu     sync.Mutex
}

func (s *Source) Archive(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	root, err := cachedir.Dir()
	if err != nil {
		return nil, fmt.Errorf("agmsg source cache: %w", err)
	}
	repo := filepath.Join(root, "panemux", "agmsg", TestedAgmsgCommit)
	if err = os.MkdirAll(repo, 0700); err != nil {
		return nil, fmt.Errorf("agmsg source cache: %w", err)
	}
	run := s.RunGit
	if run == nil {
		run = runSourceGit
	}
	git := func(args ...string) ([]byte, error) {
		out, stderr, runErr := run(ctx, append([]string{"--git-dir=" + repo}, args...)...)
		if runErr != nil {
			return nil, fmt.Errorf("agmsg git %s: %w\n%s", args[0], runErr, strings.TrimSpace(string(stderr)))
		}
		return out, nil
	}

	commit, err := git("rev-parse", "--verify", TestedAgmsgCommit+"^{commit}")
	if err != nil {
		if _, err = git("init", "--bare"); err != nil {
			return nil, fmt.Errorf("initialize agmsg cache (git required): %w", err)
		}
		if _, err = git("fetch", "--depth", "1", sourceURL, TestedAgmsgCommit); err != nil {
			return nil, fmt.Errorf("fetch pinned agmsg from github.com: %w", err)
		}
		commit, err = git("rev-parse", "--verify", TestedAgmsgCommit+"^{commit}")
	}
	if err != nil {
		return nil, fmt.Errorf("verify agmsg commit: %w", err)
	}
	if strings.TrimSpace(string(commit)) != TestedAgmsgCommit {
		return nil, errors.New("agmsg source commit does not match the tested commit")
	}
	archive, err := git("archive", "--format=tar", TestedAgmsgCommit)
	if err != nil {
		return nil, fmt.Errorf("archive pinned agmsg: %w", err)
	}
	return archive, nil
}

func runSourceGit(ctx context.Context, args ...string) ([]byte, []byte, error) {
	//nolint:wrapcheck // Archive adds operation context and stderr
	return hostexec.RunLocalSplit(ctx, execArguments, nil, append([]string{"git"}, args...)...)
}
