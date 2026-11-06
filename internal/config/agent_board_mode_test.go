package config

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentBoardModeWarnsOnceWhenLoaded(t *testing.T) {
	for _, mode := range []string{"monitor", "turn", "both", "off", "", "omitted"} {
		t.Run(mode, func(t *testing.T) {
			var output bytes.Buffer
			previous := log.Writer()
			log.SetOutput(&output)
			t.Cleanup(func() { log.SetOutput(previous) })
			input := "server:\n  port: 8080\nagent_board:\n  enabled: true\n"
			if mode != "omitted" {
				input += "  mode: \"" + mode + "\"\n"
			}
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(input), 0600))
			cfg, err := Load(path)
			require.NoError(t, err)
			require.NoError(t, cfg.Validate())
			want := 1
			if mode == "omitted" {
				want = 0
			}
			require.Equal(t, want, strings.Count(output.String(), "agent_board.mode is deprecated and ignored"))
		})
	}
}
