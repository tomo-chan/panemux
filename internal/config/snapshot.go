package config

// Snapshot is the in-memory workspace state as it was at one moment, kept so a
// mutation that turns out to be unpersistable can be undone.
//
// It exists for issue #204. Every mutating API route changes the config in
// memory before the step that can fail — the config write itself, or a session
// that has to be created — and answering 500 while leaving the change in place
// is worse than it first looks: the operator is told the operation failed, and
// the next successful write from any other route persists the change they were
// told did not happen. Config.write() serializes the whole Config, so "save
// first, then mutate" is not available; taking a snapshot before the mutation
// and putting it back on failure is.
//
// The fields are unexported because a snapshot is an opaque undo token, not a
// second way to read or assemble config: the only thing a caller may do with
// one is hand it back to Restore.
type Snapshot struct {
	layout     LayoutNode
	workspaces WorkspacesConfig
}

// Snapshot captures the state Restore can put back: the workspace list with
// its layouts, plus the top-level Layout the active workspace mirrors.
//
// Nothing else a mutating route touches lives in Config — sessions belong to
// the session manager, and rolling those back is the caller's half of the
// same undo (see internal/api).
func (c *Config) Snapshot() Snapshot {
	return Snapshot{
		workspaces: cloneWorkspaces(c.Workspaces),
		layout:     cloneLayoutNode(c.Layout),
	}
}

// Restore puts back the state Snapshot captured.
//
// It copies out of the snapshot rather than assigning it, so the same snapshot
// can be restored more than once — a route with two failure branches takes one
// snapshot and may reach either — and so a later mutation of the restored
// config cannot reach back into the snapshot it came from.
func (c *Config) Restore(snapshot Snapshot) {
	c.Workspaces = cloneWorkspaces(snapshot.workspaces)
	c.Layout = cloneLayoutNode(snapshot.layout)
}

// cloneWorkspaces copies the slice and every layout tree in it.
//
// Copying the slice is not defensive: RemoveWorkspace shifts the surviving
// items down inside the same backing array, so a snapshot holding only the
// slice header would read the mutated contents back through it.
func cloneWorkspaces(workspaces WorkspacesConfig) WorkspacesConfig {
	out := workspaces
	if workspaces.Items != nil {
		out.Items = make([]WorkspaceConfig, len(workspaces.Items))
		for i, workspace := range workspaces.Items {
			workspace.Layout = cloneLayoutNode(workspace.Layout)
			out.Items[i] = workspace
		}
	}
	return out
}

func cloneLayoutNode(node LayoutNode) LayoutNode {
	node.Children = cloneLayoutChildren(node.Children)
	return node
}

// cloneLayoutChildren copies the split tree's own structure and deliberately
// shares each *PaneConfig rather than copying it.
//
// Pane identity is load-bearing: AllPanes hands out pointers into the stored
// layout and RestartSession works through one of them, so a restore that
// replaced them would leave every such pointer attached to a pane the config
// no longer contains. Sharing is safe because nothing mutates a stored
// PaneConfig in place after load — expandPaths runs once during finishLoad,
// and the layout PUT routes expand a freshly decoded tree whose panes the
// stored config has never seen.
func cloneLayoutChildren(children []LayoutChild) []LayoutChild {
	if children == nil {
		return nil
	}
	out := make([]LayoutChild, len(children))
	for i, child := range children {
		child.Children = cloneLayoutChildren(child.Children)
		out[i] = child
	}
	return out
}
