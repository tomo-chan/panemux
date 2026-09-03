package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Issue #198. LayoutNodeSchema requires `direction` and `children`; both are
// `omitempty` on config.LayoutNode, and validateLayoutNode permits an empty
// direction and no children. So four distinct shapes that Go accepts and
// persists serialize to JSON the dashboard cannot parse — and because
// WorkspacesResponseSchema covers the whole response, that is the workspace
// list failing to load, not one key going missing.
//
// The issue named only the pane-only case. Measuring the others found three
// more of the same class, which is why normalization fills in both keys
// rather than only relocating a root pane.

// serializedKeys marshals a node the way an API response would and reports
// which of the two required keys survived.
func serializedKeys(t *testing.T, node LayoutNode) (raw string, hasDirection, hasChildren bool) {
	t.Helper()

	data, err := json.Marshal(node)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))

	direction, hasDirection := decoded["direction"]
	children, hasChildren := decoded["children"]
	// A null children is as unparseable as an absent one: the schema wants an
	// array. Same for a null direction against an enum.
	return string(data), hasDirection && direction != nil, hasChildren && children != nil
}

func onePane(id string) *PaneConfig { return &PaneConfig{ID: id, Type: "local"} }

// The property the dashboard depends on: whatever a config file contains,
// what reaches the browser always carries a direction and a children array.
func TestNormalizeLayoutNode_AlwaysSerializesDirectionAndChildren(t *testing.T) {
	for name, node := range map[string]LayoutNode{
		"pane-only root":                {Pane: onePane("main")},
		"children with no direction":    {Children: []LayoutChild{{Size: 100, Pane: onePane("main")}}},
		"direction with no children":    {Direction: directionHorizontal},
		"empty node":                    {},
		"pane-only root with direction": {Direction: directionVertical, Pane: onePane("main")},
		"already well formed": {
			Direction: directionVertical,
			Children:  []LayoutChild{{Size: 100, Pane: onePane("main")}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			raw, hasDirection, hasChildren := serializedKeys(t, normalizeLayoutNode(node))

			assert.True(t, hasDirection, "direction must survive serialization: %s", raw)
			assert.True(t, hasChildren, "children must survive serialization: %s", raw)
		})
	}
}

// The migration itself. A root pane is what an operator writes by hand for a
// single-pane workspace; nothing in frontend/src reads the root's own `pane`
// (every call site reads child.pane off LayoutChild), so it is moved into the
// one child it means rather than left in a position nothing renders.
func TestNormalizeLayoutNode_PaneOnlyRootBecomesItsSingleChild(t *testing.T) {
	got := normalizeLayoutNode(LayoutNode{Pane: onePane("main")})

	assert.Nil(t, got.Pane, "the root pane moves rather than being duplicated")
	require.Len(t, got.Children, 1)
	assert.Equal(t, onePane("main"), got.Children[0].Pane)
	assert.InDelta(t, 100, got.Children[0].Size, 0.001, "a lone child fills the workspace")
	assert.Equal(t, directionHorizontal, got.Direction)
}

// A direction already chosen for a pane-only root is kept: the operator said
// which way this workspace splits, and it is about to gain siblings.
func TestNormalizeLayoutNode_PaneOnlyRootKeepsAnExplicitDirection(t *testing.T) {
	got := normalizeLayoutNode(LayoutNode{Direction: directionVertical, Pane: onePane("main")})

	assert.Equal(t, directionVertical, got.Direction)
	require.Len(t, got.Children, 1)
}

// Normalization must not rewrite a tree that is already the shape everything
// expects — otherwise every read would churn the persisted layout.
func TestNormalizeLayoutNode_LeavesAWellFormedTreeAlone(t *testing.T) {
	original := LayoutNode{
		Direction: directionVertical,
		Children: []LayoutChild{
			{Size: 40, Pane: onePane("editor")},
			{Size: 60, Direction: directionHorizontal, Children: []LayoutChild{
				{Size: 100, Pane: onePane("build")},
			}},
		},
	}

	assert.Equal(t, original, normalizeLayoutNode(original))
}

// An invalid direction is left alone rather than silently corrected, so
// validateLayoutNode stays the thing that reports it. Correcting it here
// would turn a config error into a value the operator never wrote.
func TestNormalizeLayoutNode_DoesNotRepairAnInvalidDirection(t *testing.T) {
	got := normalizeLayoutNode(LayoutNode{
		Direction: "diagonal",
		Children:  []LayoutChild{{Size: 100, Pane: onePane("main")}},
	})

	assert.Equal(t, "diagonal", got.Direction)
	assert.Error(t, ValidateLayout(got), "validation, not normalization, owns this")
}

// ── The read paths ────────────────────────────────────────────────────────
//
// Normalizing the helper is worth nothing if a response can reach the browser
// without passing through it. These are the two accessors every LayoutNode in
// a JSON response comes from: WorkspacesView (GET /api/workspaces and every
// workspace mutation) and ActiveLayout (GET /api/layout).

func TestWorkspacesView_NormalizesEveryWorkspaceLayout(t *testing.T) {
	cfg := &Config{
		Workspaces: WorkspacesConfig{
			Active: "solo",
			Items: []WorkspaceConfig{
				{ID: "solo", Title: "Solo", Layout: LayoutNode{Pane: onePane("main")}},
				{ID: "bare", Title: "Bare", Layout: LayoutNode{}},
			},
		},
	}

	for _, workspace := range cfg.WorkspacesView().Items {
		raw, hasDirection, hasChildren := serializedKeys(t, workspace.Layout)
		assert.True(t, hasDirection, "%s: %s", workspace.ID, raw)
		assert.True(t, hasChildren, "%s: %s", workspace.ID, raw)
	}
}

func TestActiveLayout_NormalizesWhatItReturns(t *testing.T) {
	cfg := &Config{
		Workspaces: WorkspacesConfig{
			Active: "solo",
			Items:  []WorkspaceConfig{{ID: "solo", Title: "Solo", Layout: LayoutNode{Pane: onePane("main")}}},
		},
	}

	raw, hasDirection, hasChildren := serializedKeys(t, cfg.ActiveLayout())
	assert.True(t, hasDirection, raw)
	assert.True(t, hasChildren, raw)
}

// ActiveLayout falls back to the legacy top-level Layout when no workspace
// matches, and that path serializes too.
func TestActiveLayout_NormalizesTheLegacyTopLevelLayout(t *testing.T) {
	cfg := &Config{Layout: LayoutNode{Pane: onePane("main")}}

	raw, hasDirection, hasChildren := serializedKeys(t, cfg.ActiveLayout())
	assert.True(t, hasDirection, raw)
	assert.True(t, hasChildren, raw)
}

// The nil case, which normalizedWorkspaces itself cannot produce (it
// substitutes a default workspace whenever Items is empty) but which the
// helper must still answer safely: a nil slice marshals to null, and
// WorkspacesResponseSchema requires `items` to be an array. Returning nil
// here would reintroduce, one level up, the same null-for-an-array defect
// this file exists to remove from LayoutNode.
func TestNormalizeWorkspaceLayouts_NilBecomesAnEmptySlice(t *testing.T) {
	got := normalizeWorkspaceLayouts(nil)

	assert.NotNil(t, got, "nil marshals to null; an array schema rejects it")
	assert.Empty(t, got)

	data, err := json.Marshal(WorkspacesConfig{Items: got})
	require.NoError(t, err)
	assert.Contains(t, string(data), `"items":[]`)
}

// ── Issue #199 review findings ────────────────────────────────────────────

// The `{pane, children}` shape. The relocation guard deliberately excludes it
// — prepending the root pane to children that already sum to 100 would mean
// rescaling every sibling, silently changing proportions the operator wrote,
// and making a pane that has never rendered appear. So `pane` survives here,
// which is why LayoutNodeSchema must keep declaring it: an undeclared key is
// stripped by WorkspacesResponseSchema.parse, stored stripped by useLayout,
// and PUT back on the next split — deleting it from config.yaml for good.
// That is the failure mode the PaneConfigSchema comment records agent_board
// being lost to.
func TestNormalizeLayoutNode_KeepsARootPaneThatSitsBesideChildren(t *testing.T) {
	got := normalizeLayoutNode(LayoutNode{
		Pane:     onePane("root"),
		Children: []LayoutChild{{Size: 100, Pane: onePane("a")}},
	})

	require.NotNil(t, got.Pane, "dropping it here is the same data loss, just ours")
	assert.Equal(t, "root", got.Pane.ID)
	require.Len(t, got.Children, 1, "siblings are not rescaled to make room")
	assert.Equal(t, "a", got.Children[0].Pane.ID)

	raw, hasDirection, hasChildren := serializedKeys(t, got)
	assert.True(t, hasDirection, raw)
	assert.True(t, hasChildren, raw)
}

// Relocating a pane-only root moves it somewhere validatePane can see it.
// validateLayoutNode never inspected LayoutNode.Pane and collectPanes never
// walked it, so a root pane was previously validated by nothing — which is
// also why it rendered nothing. A root pane whose type is missing or whose
// ssh connection is undefined therefore stops a config that used to load,
// and that is deliberate: the same pane written as a child has always failed
// this way, and normalization does not invent values an operator never wrote
// (see TestNormalizeLayoutNode_DoesNotRepairAnInvalidDirection). The error
// names the pane and the fix; the previous behavior was an empty workspace
// and no explanation. docs/behavior.md records it.
func TestLoad_RelocatedRootPaneIsValidatedLikeAnyOther(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
server:
  port: 8080
  host: 127.0.0.1
workspaces:
  active: main
  items:
    - id: main
      title: Main
      layout:
        pane:
          id: main
          shell: /bin/sh
`), 0o600))

	_, err := Load(path)

	require.Error(t, err, "a root pane with no type used to load and render nothing")
	assert.Contains(t, err.Error(), `pane "main" has invalid type ""`)
}

// The same config with the type the operator meant loads, and the pane ends
// up where the dashboard renders it. Without this the test above would be
// satisfied by normalization breaking every root pane.
func TestLoad_WellFormedRootPaneRelocatesAndLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
server:
  port: 8080
  host: 127.0.0.1
workspaces:
  active: main
  items:
    - id: main
      title: Main
      layout:
        pane:
          id: main
          type: local
`), 0o600))

	cfg, err := Load(path)

	require.NoError(t, err)
	layout := cfg.ActiveLayout()
	assert.Nil(t, layout.Pane)
	require.Len(t, layout.Children, 1)
	assert.Equal(t, "main", layout.Children[0].Pane.ID)
}
