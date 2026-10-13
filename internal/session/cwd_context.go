package session

import (
	"context"
	"strings"
	"sync"
)

// CWDContextGetter lets capture cancellation reach the directory lookup without
// closing the pane's terminal connection.
type CWDContextGetter interface {
	GetCWDContext(context.Context) (string, error)
}

// SSHConfigGetter supplies the settings used to establish the live pane, with
// no ownership of its SSH client. The returned jump chain is an independent copy.
type SSHConfigGetter interface {
	SSHConfigSnapshot() SSHConfig
}

func cloneSSHConfig(cfg SSHConfig) SSHConfig {
	if cfg.JumpHost != nil {
		jump := cloneSSHConfig(*cfg.JumpHost)
		cfg.JumpHost = &jump
	}
	return cfg
}

// remoteCWD returns promptly even when channel creation or the exec reply is
// pending. The worker owns late results; cancellation closes only this channel.
func remoteCWD(ctx context.Context, newSession func() (sshSessionRunner, error), command string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	type result struct {
		err error
		cwd string
	}
	results := make(chan result)
	go func() {
		sess, err := newSession()
		var cwd string
		if err == nil {
			var once sync.Once
			closeSession := func() { once.Do(func() { _ = sess.Close() }) }
			defer closeSession()
			stop := context.AfterFunc(ctx, closeSession)
			defer stop()
			if err = ctx.Err(); err == nil {
				var out []byte
				out, err = sess.Output(command)
				cwd = strings.TrimSpace(string(out))
			}
		}
		select {
		case results <- result{err, cwd}:
		case <-ctx.Done():
		}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-results:
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return result.cwd, result.err
	}
}
