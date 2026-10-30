package agmsg

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"panemux/internal/cachedir"
	"panemux/internal/host"
)

type sourceExecution struct {
	failure  string
	commands []string
	cached   bool
	mismatch bool
	fail     bool
}

func (e *sourceExecution) RunGit(ctx context.Context, args ...string) ([]byte, []byte, error) {
	out, err := e.Run(ctx, host.Local, execArguments, append([]string{"git"}, args...)...)
	return out, nil, err
}

func (e *sourceExecution) Run(_ context.Context, h host.Host, script string, args ...string) ([]byte, error) {
	if h != host.Local || script != execArguments {
		return nil, errors.New("wrong execution boundary")
	}
	e.commands = append(e.commands, strings.Join(args, " "))
	if e.failure != "" && containsArg(args, e.failure) {
		return nil, errors.New("git failed")
	}
	switch {
	case containsArg(args, "rev-parse"):
		if !e.cached {
			return nil, errors.New("not fetched")
		}
		if e.mismatch {
			return []byte(strings.Repeat("0", 40)), nil
		}
		return []byte(TestedAgmsgCommit + "\n"), nil
	case containsArg(args, "fetch"):
		if e.fail {
			return nil, errors.New("offline")
		}
		e.cached = true
	case containsArg(args, "archive"):
		return []byte("tar contents"), nil
	}
	return nil, nil
}
func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func TestPinnedSourceFetchVerifyAndOfflineCache(t *testing.T) {
	cachedir.SetForTest(t, t.TempDir())
	e := &sourceExecution{}
	s := &Source{RunGit: e.RunGit}
	archive, err := s.Archive(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tar contents", string(archive))
	require.Contains(t, strings.Join(e.commands, "\n"),
		"fetch --depth 1 https://github.com/fujibee/agmsg.git "+TestedAgmsgCommit)
	e.commands, e.fail = nil, true
	_, err = s.Archive(context.Background())
	require.NoError(t, err)
	require.NotContains(t, strings.Join(e.commands, "\n"), "fetch")
	e.mismatch = true
	_, err = s.Archive(context.Background())
	require.ErrorContains(t, err, "commit")
	require.NotContains(t, e.commands[len(e.commands)-1], "archive")
}

func TestPinnedSourceFailures(t *testing.T) {
	for _, stage := range []string{"init", "fetch", "rev-parse", "archive"} {
		t.Run(stage, func(t *testing.T) {
			cachedir.SetForTest(t, t.TempDir())
			e := &sourceExecution{failure: stage, cached: stage == "archive"}
			_, err := (&Source{RunGit: e.RunGit}).Archive(context.Background())
			require.Error(t, err)
		})
	}
	cachedir.SetFailingForTest(t, errors.New("no cache"))
	_, err := (&Source{}).Archive(context.Background())
	require.ErrorContains(t, err, "no cache")
	file := t.TempDir() + "/file"
	require.NoError(t, os.WriteFile(file, nil, 0600))
	cachedir.SetForTest(t, file)
	_, err = (&Source{}).Archive(context.Background())
	require.Error(t, err)
}
