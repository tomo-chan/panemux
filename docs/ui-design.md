# UI Design

This document describes the visual design decisions for the PaneMux frontend, covering always-available layout editing, drag-and-drop, the workspace bar, and modal dialogs.

## Design Principles

Three core principles guide the interactive UI:

**State Visibility** — the user must always know what action is available and what action is in progress. Pane movement, pane insertion, transient errors, and modal editing should all be visible without requiring hidden modes.

**Affordances** — visual cues should match physical intuition. Grab cursors indicate draggable elements. Edge highlights and divider overlays show where a pane can land. Buttons that create or mutate layout should stay grouped with the structure they affect.

**Feedback** — transitions between states are animated (0.15–0.2 s) to prevent abrupt jumps and to make cause-and-effect legible. A ghosted drag source, grabbing cursors, and visible alerts on failed persistence keep the UI honest when optimistic updates are used.

---

## Color Palette

| Role | Value | Usage |
|---|---|---|
| Interactive blue | `#569cd6` | drop-target highlight, selected workspace-position control |
| Drag handle blue | `#4a7ea5` | header move handle `⠿` |
| Normal header background | `#252526` | pane header background |
| Normal header border | `#333` | pane header bottom border |
| Workspace bar background | `#202124` | workspace bar surface |
| Active workspace tab | `#2f3540` | active workspace tab background |
| Position-control selected | `#3a4350` | selected top/bottom/left/right tab-position button |
| Dialog background | `#252526` | modal dialog surface |
| Dialog border | `#444` | modal dialog outline |
| Error banner background | `#2f1313` | move-persistence failure alert |
| Error banner border | `#7f1d1d` | move-persistence failure alert border |
| Error banner text | `#fca5a5` | move-persistence failure alert text |

---

## Workspace Bar

The workspace bar is always available, even when there is only one workspace. This avoids hiding structure-management actions behind a special mode and keeps workspace-level actions in one place.

### Contents

The bar can contain:

- workspace tabs
- `+`
- workspace tab-position controls
- inline rename and delete controls on each tab

### Why keep the bar visible with one workspace

Workspace management still lives in the bar even when pane creation moved to the pane header. Keeping the bar visible preserves a stable location for workspace add and bar-position controls, and it avoids layout shifts when more workspaces are added later.

### Tab position controls

The top/bottom/left/right controls use compact directional buttons with `aria-pressed` on the active position. They are deliberately terse because they are structural controls, not primary content.

The four direction buttons are grouped inside a single tab-like cluster rather than being split across separate segments. In practice this is rendered as one tab surface containing a horizontal row of four compact buttons, so even on left/right bars it reads as one control group instead of four separate tabs. This keeps the workspace-position control readable as one concept instead of four unrelated actions.

The position cluster sits on the opposite end of the workspace tabs:

- top/bottom bars: tabs stay at the leading side, position controls stay at the far trailing side
- left/right bars: tabs stay near the top, position controls stay at the bottom

This separation keeps workspace navigation and workspace-bar relocation visually distinct.

Workspace creation uses a single `+` control because it is now the only creation action in the bar and does not need extra wording to distinguish it from terminal creation.

---

## Pane Header

The pane header remains the compact command strip for each pane.

### Layout

From left to right, the header contains:

- drag handle `⠿`
- connection status dot
- pane type label
- optional pane title
- optional git information
- reconnecting status text
- action buttons aligned to the right

The header background stays `#252526` with a `#333` bottom border so the new controls inherit the established terminal chrome instead of introducing a second visual language.

### Split vs quick-add

The header now carries two distinct expansion actions:

- split buttons, which inherit the current pane configuration and divide the current work context
- quick-add buttons, which create a default `local` pane immediately to the right or below

The quick-add buttons are intentionally colocated with split because they act on the current pane boundary, but they must remain visually distinct so users can tell apart "duplicate/split this context" from "add a fresh default pane here".

The action buttons use a small monochrome SVG icon set rather than relying on mixed Unicode glyphs. Split and quick-add both use a pane-outline motif so they read as related actions, while quick-add uses a plus marker to distinguish "new default pane" from "split current pane".

### Drag handle

Pane movement starts only from the `⠿` handle in the header. This preserves uninterrupted terminal interaction inside the pane body while still making re-layout possible.

The handle uses `#4a7ea5` and a `grab` cursor. While dragging it switches to `grabbing`, matching editor chrome that treats pane movement as a direct manipulation gesture. It is small enough not to crowd the header but distinct enough from the status dot and type badge to read as an affordance instead of decoration.

### Why header-only drag

The terminal body must stay available for text selection, focus, mouse reporting, and shell input. Restricting drag initiation to the header avoids gesture conflicts that would otherwise make terminal interaction unreliable.

---

## Drag and Drop

Drag-and-drop is always available from the pane header handle. There is no separate edit mode.

### States

**Normal**

- pane opacity is `1`
- no move target highlight is visible

**Drag source**

- the source pane fades and slightly scales down, approximating a light drag ghost without detaching the live terminal canvas
- the global cursor switches to `grabbing`
- the 0.15 s transition confirms the drag immediately

**Pane-edge drop target**

- the hovered half of the target pane becomes the drop region
- a translucent blue half-pane preview appears on the chosen side
- edge selection is resolved from pointer proximity to the nearest pane edge, so users do not have to hit a thin strip precisely

**Divider drop target**

- the divider keeps its resize role normally
- during drag, a blue overlay appears on the divider drop zone to show that insertion is possible there

**Workspace-edge drop target**

- the workspace edges become drop zones during drag
- dropping there creates a new outer split around the current layout

### Interaction model

- dropping on a workspace edge creates a new outer layout and moves the pane there
- dropping on a pane edge inserts the dragged pane beside the target pane
- dropping on a divider inserts relative to the adjacent subtree boundary

This model deliberately favors spatial predictability over hidden container selection widgets.

---

## Modal Dialogs

The frontend now uses modal dialogs for higher-friction configuration tasks, rather than trying to compress all editing into inline chrome.

---

## Transient Error Banner

Pane moves are optimistic in the UI and then persisted. If persistence fails, the user needs immediate feedback because the visible layout can temporarily diverge from saved config.

The move error banner:

- appears in the top-right of the workspace content area
- uses a destructive but subdued palette (`#2f1313`, `#7f1d1d`, `#fca5a5`)
- includes an explicit dismiss button
- remains separate from modal dialog errors because it belongs to an in-place interaction, not a form

### Error state lifecycle

The banner state is:

- hidden by default
- visible when a move persistence request rejects
- hidden again when dismissed or when the next move attempt starts

This keeps the error noticeable without forcing it into a blocking dialog.

---

## Attention Indicators

Agent-attention highlighting remains visually distinct from layout-editing affordances.

- pane attention uses an animated gold frame
- workspace attention uses an animated gold tab background
- move targets use blue overlays instead of gold

Using separate colors avoids mixing "this needs your attention" with "you can drop here".
