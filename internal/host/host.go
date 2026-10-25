// Package host defines host identity without owning any I/O.
package host

import "context"

// Host is an ssh_connections key, or the empty name for the panemux host.
type Host string

const Local Host = ""

// BoardID preserves the board protocol identity, including aliases named local.
func (h Host) BoardID() string {
	if h == Local {
		return "local"
	}
	return "ssh:" + string(h)
}

// Runner executes fixed script text with data in positional arguments on one host.
type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

// Executor is the shared host execution port. Scripts are supplied by trusted application code.
type Executor interface {
	Run(context.Context, Host, string, ...string) ([]byte, error)
}

// Bind selects a host without allocating or owning a connection.
func Bind(e Executor, h Host) Runner { return bound{e, h} }

type bound struct {
	executor Executor
	host     Host
}

func (b bound) Run(ctx context.Context, script string, args ...string) ([]byte, error) {
	return b.executor.Run(ctx, b.host, script, args...) //nolint:wrapcheck // preserve execution errors
}
