package board

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type localRunCall struct {
	name string
	args []string
}

func fakeLocalRun(calls *[]localRunCall, outputs map[string][]byte, errs map[string]error) runLocalCommandFn {
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		key := strings.Join(append([]string{name}, args...), " ")
		*calls = append(*calls, localRunCall{name: name, args: args})
		if err, ok := errs[key]; ok {
			return outputs[key], err
		}
		if out, ok := outputs[key]; ok {
			return out, nil
		}
		return nil, fmt.Errorf("unexpected local command: %s", key)
	}
}

func TestLocalAgmsgClient_HostID(t *testing.T) {
	c := NewLocalAgmsgClient("/opt/agmsg")
	assert.Equal(t, "local", c.HostID())
}

func TestLocalAgmsgClient_Send_BuildsExpectedArgv(t *testing.T) {
	var calls []localRunCall
	sendScript := filepath.Join("/opt/agmsg", "scripts", "send.sh")
	c := &LocalAgmsgClient{
		agmsgPath: "/opt/agmsg",
		run: fakeLocalRun(&calls, map[string][]byte{
			sendScript + " team-a from-a to-a hello --force": []byte(""),
		}, nil),
	}

	err := c.Send(context.Background(), "team-a", "from-a", "to-a", "hello")
	require.NoError(t, err)
	require.Len(t, calls, 1)
	assert.Equal(t, sendScript, calls[0].name)
	assert.Equal(t, []string{"team-a", "from-a", "to-a", "hello", "--force"}, calls[0].args)
}

func TestLocalAgmsgClient_Send_MetacharacterBody_PassedAsSingleArgvElement(t *testing.T) {
	var calls []localRunCall
	body := `it's; $(evil) && ` + "`echo hi`"
	sendScript := filepath.Join("/opt/agmsg", "scripts", "send.sh")
	c := &LocalAgmsgClient{
		agmsgPath: "/opt/agmsg",
		run: fakeLocalRun(&calls, map[string][]byte{
			sendScript + " team from to " + body + " --force": []byte(""),
		}, nil),
	}

	err := c.Send(context.Background(), "team", "from", "to", body)
	require.NoError(t, err)
	require.Len(t, calls, 1)
	// The body arrives as ONE argv element, unmodified — exec.Command
	// passes discrete array elements with no intermediate shell, so
	// metacharacters inside it are inert data, never shell syntax.
	assert.Equal(t, body, calls[0].args[3])
}

func TestLocalAgmsgClient_Send_RunError_Wrapped(t *testing.T) {
	var calls []localRunCall
	sendScript := filepath.Join("/opt/agmsg", "scripts", "send.sh")
	wantErr := errors.New("exit status 1")
	c := &LocalAgmsgClient{
		agmsgPath: "/opt/agmsg",
		run: fakeLocalRun(&calls, nil, map[string]error{
			sendScript + " team from to body --force": wantErr,
		}),
	}

	err := c.Send(context.Background(), "team", "from", "to", "body")
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

func TestLocalAgmsgClient_Since_ParsesRowsAndFiltersByAfterID(t *testing.T) {
	var calls []localRunCall
	apiScript := filepath.Join("/opt/agmsg", "scripts", "api.sh")
	jsonl := `{"type":"message_sent","id":"1","team":"t","from":"a","to":"b","body":"first","at":"2026-01-01T00:00:00Z"}
{"type":"message_sent","id":"2","team":"t","from":"a","to":"b","body":"second","at":"2026-01-01T00:01:00Z"}
`
	c := &LocalAgmsgClient{
		agmsgPath: "/opt/agmsg",
		run: fakeLocalRun(&calls, map[string][]byte{
			apiScript + " get teams t messages --limit 100": []byte(jsonl),
		}, nil),
	}

	rows, err := c.Since(context.Background(), "t", "1", 100)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "second", rows[0].Body)
	assert.Equal(t, "local", rows[0].Host)
}

func TestLocalAgmsgClient_Since_EmptyAfterID_ReturnsAllRows(t *testing.T) {
	var calls []localRunCall
	apiScript := filepath.Join("/opt/agmsg", "scripts", "api.sh")
	jsonl := `{"type":"message_sent","id":"1","team":"t","from":"a","to":"b","body":"first","at":"2026-01-01T00:00:00Z"}
`
	c := &LocalAgmsgClient{
		agmsgPath: "/opt/agmsg",
		run: fakeLocalRun(&calls, map[string][]byte{
			apiScript + " get teams t messages --limit 50": []byte(jsonl),
		}, nil),
	}

	rows, err := c.Since(context.Background(), "t", "", 50)
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestLocalAgmsgClient_Since_RunError_Wrapped(t *testing.T) {
	var calls []localRunCall
	apiScript := filepath.Join("/opt/agmsg", "scripts", "api.sh")
	wantErr := errors.New("no such team")
	c := &LocalAgmsgClient{
		agmsgPath: "/opt/agmsg",
		run: fakeLocalRun(&calls, nil, map[string]error{
			apiScript + " get teams missing messages --limit 10": wantErr,
		}),
	}

	rows, err := c.Since(context.Background(), "missing", "", 10)
	require.Error(t, err)
	assert.Nil(t, rows)
	assert.ErrorIs(t, err, wantErr)
}
