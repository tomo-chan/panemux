package board

// PaneRef is a minimal (pane ID, host) pair the relay needs to validate a
// row's `from` and resolve a row's `to`. Built by the caller (main.go/
// server wiring) from panemux's own config — never from a live agmsg call,
// per docs/agent-board.md's Cross-host relay section ("panemux resolves to
// to its owning pane and that pane's host via the already-known pane→
// session config").
type PaneRef struct {
	ID     string
	HostID string
}

// PaneResolver answers the relay's routing questions from panemux's own
// pane/host config.
type PaneResolver interface {
	// HostForPane resolves a pane ID to the host it lives on, and whether
	// that pane is known at all.
	HostForPane(paneID string) (hostID string, ok bool)
	// KnownPane reports whether paneID is a known pane on hostID.
	KnownPane(hostID, paneID string) bool
}

// StaticPaneResolver is a PaneResolver backed by a fixed snapshot of
// panemux's pane config.
type StaticPaneResolver struct {
	byID     map[string]string            // paneID -> hostID
	byHostID map[string]map[string]bool   // hostID -> set of paneIDs
}

// NewStaticPaneResolver builds a resolver from a flat list of pane refs.
func NewStaticPaneResolver(panes []PaneRef) *StaticPaneResolver {
	r := &StaticPaneResolver{
		byID:     make(map[string]string, len(panes)),
		byHostID: make(map[string]map[string]bool),
	}
	for _, p := range panes {
		r.byID[p.ID] = p.HostID
		if r.byHostID[p.HostID] == nil {
			r.byHostID[p.HostID] = make(map[string]bool)
		}
		r.byHostID[p.HostID][p.ID] = true
	}
	return r
}

func (r *StaticPaneResolver) HostForPane(paneID string) (string, bool) {
	host, ok := r.byID[paneID]
	return host, ok
}

func (r *StaticPaneResolver) KnownPane(hostID, paneID string) bool {
	return r.byHostID[hostID][paneID]
}
