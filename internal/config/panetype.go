package config

// Pane type wire values. These are part of config.yaml's user-facing schema and
// of the API/WebSocket contract the frontend's Zod schemas parse, so they are
// fixed strings rather than an internal encoding that may be renamed.
//
// They live here, in the package that owns the schema, because the dependency
// only runs one way: internal/session imports internal/config, so session.Type
// derives its constants from these (see internal/session/session.go) rather
// than the other way round.
const (
	PaneTypeLocal   = "local"
	PaneTypeSSH     = "ssh"
	PaneTypeTmux    = "tmux"
	PaneTypeSSHTmux = "ssh_tmux"
)

// PaneTypes returns every valid value of PaneConfig.Type, in the order the
// validation error message lists them. It returns a fresh slice on each call so
// a caller cannot mutate the package's own list.
func PaneTypes() []string {
	return []string{PaneTypeLocal, PaneTypeSSH, PaneTypeTmux, PaneTypeSSHTmux}
}
