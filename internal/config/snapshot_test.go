package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Snapshot/Restore exist for issue #204: every mutating API route changes the
// in-memory config before the step that can fail, and a route that answers 500
// has to be able to put the config back the way it was — otherwise the change
// the operator was told did not happen is still live, and the next successful
// write from any other route persists it to config.yaml.
//
// The tests below are the properties a rollback needs: it has to undo each
// mutation the API routes actually perform, it has to survive being taken
// before a mutation that reuses the same backing array, and a snapshot has to
// stay usable after it has been restored once.

func threeWorkspaceConfig() *Config {
	return &Config{
		Workspaces: WorkspacesConfig{
			Active:           "two",
			TabPosition:      "top",
			VerticalBarWidth: 280,
			Items: []WorkspaceConfig{
				{ID: "one", Title: "One", Layout: singlePaneLayout("one-main")},
				{ID: "two", Title: "Two", Layout: singlePaneLayout("two-main")},
				{ID: "three", Title: "Three", Layout: singlePaneLayout("three-main")},
			},
		},
		Layout: singlePaneLayout("two-main"),
	}
}

func workspaceIDs(cfg *Config) []string {
	ids := make([]string, 0, len(cfg.Workspaces.Items))
	for _, workspace := range cfg.Workspaces.Items {
		ids = append(ids, workspace.ID)
	}
	return ids
}

func paneIDs(node LayoutNode) []string {
	var ids []string
	var walk func([]LayoutChild)
	walk = func(children []LayoutChild) {
		for i := range children {
			if children[i].Pane != nil {
				ids = append(ids, children[i].Pane.ID)
			}
			walk(children[i].Children)
		}
	}
	walk(node.Children)
	return ids
}

// RemoveWorkspace shifts the surviving items down inside the slice's own
// backing array, so a snapshot that only copied the slice header would read
// back as ["one", "three", "three"]. This is the case that makes the copy
// mandatory rather than defensive.
func TestSnapshotRestore_UndoesRemoveWorkspace(t *testing.T) {
	cfg := threeWorkspaceConfig()
	snapshot := cfg.Snapshot()

	_, ok := cfg.RemoveWorkspace("two")
	require.True(t, ok)
	require.Equal(t, []string{"one", "three"}, workspaceIDs(cfg))

	cfg.Restore(snapshot)

	assert.Equal(t, []string{"one", "two", "three"}, workspaceIDs(cfg))
	assert.Equal(t, "two", cfg.Workspaces.Active)
	assert.Equal(t, []string{"two-main"}, paneIDs(cfg.Layout))
}

func TestSnapshotRestore_UndoesAddDefaultWorkspace(t *testing.T) {
	cfg := threeWorkspaceConfig()
	snapshot := cfg.Snapshot()

	added := cfg.AddDefaultWorkspace()
	require.Equal(t, "workspace-4", added.ID)
	require.Len(t, cfg.Workspaces.Items, 4)

	cfg.Restore(snapshot)

	assert.Equal(t, []string{"one", "two", "three"}, workspaceIDs(cfg))
	assert.Equal(t, "two", cfg.Workspaces.Active, "the added workspace had made itself active")
	assert.Equal(t, []string{"two-main"}, paneIDs(cfg.Layout))
}

func TestSnapshotRestore_UndoesRemovePaneFromLayout(t *testing.T) {
	nested := LayoutNode{
		Direction: "horizontal",
		Children: []LayoutChild{
			{Size: 50, Pane: &PaneConfig{ID: "one-main", Type: "local"}},
			{Size: 50, Direction: "vertical", Children: []LayoutChild{
				{Size: 50, Pane: &PaneConfig{ID: "one-b", Type: "local"}},
				{Size: 50, Pane: &PaneConfig{ID: "one-c", Type: "local"}},
			}},
		},
	}
	cfg := &Config{
		Workspaces: WorkspacesConfig{
			Active:      "one",
			TabPosition: "top",
			Items:       []WorkspaceConfig{{ID: "one", Title: "One", Layout: nested}},
		},
		Layout: nested,
	}
	snapshot := cfg.Snapshot()

	cfg.RemovePaneFromLayout("one-b")
	require.Equal(t, []string{"one-main", "one-c"}, paneIDs(cfg.Workspaces.Items[0].Layout))

	cfg.Restore(snapshot)

	assert.Equal(t, []string{"one-main", "one-b", "one-c"}, paneIDs(cfg.Workspaces.Items[0].Layout),
		"a nested pane has to come back too, so the snapshot cannot stop at the root's own children")
	assert.Equal(t, []string{"one-main", "one-b", "one-c"}, paneIDs(cfg.Layout))
}

func TestSnapshotRestore_UndoesRenameAndActiveSwitchAndSettings(t *testing.T) {
	cfg := threeWorkspaceConfig()
	snapshot := cfg.Snapshot()

	require.True(t, cfg.RenameWorkspace("one", "Renamed"))
	require.True(t, cfg.SetActiveWorkspace("three"))
	require.NoError(t, cfg.SetWorkspaceTabPosition("left"))
	require.NoError(t, cfg.SetWorkspaceVerticalBarWidth(320))

	cfg.Restore(snapshot)

	assert.Equal(t, "One", cfg.Workspaces.Items[0].Title)
	assert.Equal(t, "two", cfg.Workspaces.Active)
	assert.Equal(t, "top", cfg.Workspaces.TabPosition)
	assert.Equal(t, 280, cfg.Workspaces.VerticalBarWidth)
	assert.Equal(t, []string{"two-main"}, paneIDs(cfg.Layout))
}

func TestSnapshotRestore_UndoesUpdateWorkspaceLayout(t *testing.T) {
	cfg := threeWorkspaceConfig()
	snapshot := cfg.Snapshot()

	require.True(t, cfg.UpdateWorkspaceLayout("two", singlePaneLayout("replacement")))
	require.Equal(t, []string{"replacement"}, paneIDs(cfg.Workspaces.Items[1].Layout))

	cfg.Restore(snapshot)

	assert.Equal(t, []string{"two-main"}, paneIDs(cfg.Workspaces.Items[1].Layout))
	assert.Equal(t, []string{"two-main"}, paneIDs(cfg.Layout))
}

// A handler takes one snapshot and may restore it on either of two failure
// branches, and a route that rolled back once must be able to roll back again
// on the next request. Restoring must therefore hand the config a copy rather
// than the snapshot's own slices.
func TestSnapshot_SurvivesBeingRestoredMoreThanOnce(t *testing.T) {
	cfg := threeWorkspaceConfig()
	snapshot := cfg.Snapshot()

	for range 3 {
		_, ok := cfg.RemoveWorkspace("two")
		require.True(t, ok)
		cfg.Restore(snapshot)
		require.Equal(t, []string{"one", "two", "three"}, workspaceIDs(cfg))
	}
}

// The snapshot is taken before the mutation, so nothing the mutation does may
// reach back into it either.
func TestSnapshot_IsNotAliasedByLaterMutations(t *testing.T) {
	cfg := threeWorkspaceConfig()
	snapshot := cfg.Snapshot()

	require.True(t, cfg.RenameWorkspace("one", "Renamed"))
	cfg.Workspaces.Items[0].Layout.Children[0].Size = 42
	_, ok := cfg.RemoveWorkspace("three")
	require.True(t, ok)

	cfg.Restore(snapshot)

	assert.Equal(t, []string{"one", "two", "three"}, workspaceIDs(cfg))
	assert.Equal(t, "One", cfg.Workspaces.Items[0].Title)
	assert.InDelta(t, 100.0, cfg.Workspaces.Items[0].Layout.Children[0].Size, 0.001)
}

// Restoring must not resurrect a pane pointer identity the rest of the process
// no longer refers to: AllPanes hands callers pointers into the stored layout,
// and a restore that deep-copied PaneConfig would silently detach them.
func TestRestore_KeepsPaneIdentity(t *testing.T) {
	cfg := threeWorkspaceConfig()
	before := cfg.Workspaces.Items[1].Layout.Children[0].Pane
	snapshot := cfg.Snapshot()

	_, ok := cfg.RemoveWorkspace("two")
	require.True(t, ok)
	cfg.Restore(snapshot)

	assert.Same(t, before, cfg.Workspaces.Items[1].Layout.Children[0].Pane)
}

// Layouts nest, and the split tree a real workspace carries is edited in
// place by the dashboard as often as it is replaced wholesale. A snapshot that
// copied only the root's own children slice would hand back a tree still
// sharing every deeper one.
func TestSnapshot_CopiesNestedChildrenNotJustTheRoot(t *testing.T) {
	cfg := &Config{
		Workspaces: WorkspacesConfig{
			Active:      "one",
			TabPosition: "top",
			Items: []WorkspaceConfig{{ID: "one", Title: "One", Layout: LayoutNode{
				Direction: "horizontal",
				Children: []LayoutChild{
					{Size: 50, Pane: &PaneConfig{ID: "one-main", Type: "local"}},
					{Size: 50, Direction: "vertical", Children: []LayoutChild{
						{Size: 50, Pane: &PaneConfig{ID: "one-b", Type: "local"}},
						{Size: 50, Pane: &PaneConfig{ID: "one-c", Type: "local"}},
					}},
				},
			}}},
		},
	}
	snapshot := cfg.Snapshot()

	nested := cfg.Workspaces.Items[0].Layout.Children[1].Children
	nested[0].Size = 90
	nested[1].Size = 10

	cfg.Restore(snapshot)

	restored := cfg.Workspaces.Items[0].Layout.Children[1].Children
	assert.InDelta(t, 50.0, restored[0].Size, 0.001)
	assert.InDelta(t, 50.0, restored[1].Size, 0.001)
}
