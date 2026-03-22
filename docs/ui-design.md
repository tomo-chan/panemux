# UI Design

This document describes the visual design decisions for the PaneMux frontend, covering edit mode, drag-and-drop, and the principles behind them.

## Design Principles

Three core principles guide the interactive UI:

**State Visibility** — the user must always know what mode the application is in and what action is in progress. When edit mode is active, every pane should look different from the normal operating state without requiring the user to look at a toggle button. When a drag is in flight, the source and destination must be unambiguous.

**Affordances** — visual cues should match physical intuition. Grab cursors indicate draggable elements. Faded, translucent panes feel "lifted away" from the surface. Bright outlines show where something can land.

**Feedback** — transitions between states are animated (0.15–0.2 s) to prevent abrupt jumps and to make cause-and-effect legible. Immediate opacity changes on drag start give instant confirmation that the drag has begun.

---

## Color Palette

| Role | Value | Usage |
|---|---|---|
| Interactive blue | `#569cd6` | drop-target outline, drag handle, toggle border |
| Edit mode blue-tint dark | `rgba(10, 20, 38, 0.54)` | terminal overlay in edit mode |
| Edit header background | `#1d2b3a` | pane header in edit mode |
| Edit header border | `#2a3f55` | header bottom border in edit mode |
| Normal header background | `#252526` | pane header in normal mode |
| Normal header border | `#333` | header bottom border in normal mode |
| Drag handle icon | `#4a7ea5` | ⠿ icon in edit mode header |
| Pane frame (edit mode) | `rgba(86, 156, 214, 0.18)` | inset box-shadow border |
| Drag source outline | `rgba(86, 156, 214, 0.7)` | dashed outline on pane being dragged |

---

## Edit Mode

Edit mode is toggled by the fixed button in the bottom-right corner. When active, the application switches from a "use terminals" context to a "rearrange layout" context. Terminal input is blocked for the duration.

### Why block terminal input

Allowing simultaneous terminal interaction and layout editing creates two problems: accidental keystrokes sent to a shell while trying to drag, and ambiguous gesture ownership (is the user clicking to focus, or clicking to start a drag?). Blocking input during edit mode eliminates both.

### Visual changes per pane

| Element | Normal | Edit mode |
|---|---|---|
| Header background | `#252526` | `#1d2b3a` (blue-gray) |
| Header border | `#333` | `#2a3f55` |
| Drag handle `⠿` | hidden | visible, color `#4a7ea5` |
| Header cursor | `default` | `grab` |
| Pane frame | none | `inset 0 0 0 1px rgba(86,156,214,0.18)` |
| Terminal overlay | none | `rgba(10,20,38,0.54)` blocking layer |

The header shifts toward a blue-gray tone (`#1d2b3a`) to signal the different mode without disrupting readability. A 0.2 s transition on `background-color` and `border-color` makes the switch feel intentional rather than abrupt.

The terminal overlay is a `position: absolute` layer over the xterm.js canvas. It serves two purposes: it intercepts all pointer events (preventing the terminal from gaining focus) and it visually dims the terminal content so users understand the pane is locked. The blue-tinted dark color (`rgba(10,20,38,0.54)`) is distinct from the session-ended overlay (`rgba(0,0,0,0.6)`) so the two states are not confused.

The inset `box-shadow` border marks every pane as part of an "edit zone" without affecting layout geometry. Using `box-shadow` rather than `border` or `outline` keeps the frame cosmetic and avoids shifting sibling sizes.

---

## Drag and Drop

Drag-and-drop pane reordering is active only in edit mode. The entire pane is the drag surface — not just the header — making it easy to initiate a drag on any visible part of the pane.

### States

**Normal (edit mode, not dragging)**

Panes show the edit-mode frame and overlay. The grab cursor is visible on the header.

**Drag source** (`isDragging: true` on the pane being dragged)

- `opacity: 0.35` — the pane becomes translucent, evoking a "picked up from the surface" metaphor
- `outline: 2px dashed rgba(86,156,214,0.7)` — a dashed border reinforces that this slot is now empty/occupied
- The browser generates a ghost image from the element for the pointer to carry; the ghost and the faded source together communicate "this pane is moving"
- 0.15 s `opacity` transition ensures the fade starts immediately on drag initiation

**Drop target** (`isDragOver: true` on the hovered pane)

- `outline: 2px solid #569cd6` — solid bright blue contrasts clearly with the dashed source outline
- Outline uses `outlineOffset: -2px` to render inside the element boundary, keeping the box model unchanged

**After drop / drag cancel**

Both states reset: `isDragging` and `isDragOver` return to `false`, opacity returns to 1, and outlines clear. The drag source and target revert to normal edit-mode appearance within one animation frame.

### Why entire-pane drag instead of header-only

A header spanning 22–24 px at 11 px font size is a narrow target, especially on high-density layouts with many small panes. Making the full pane draggable removes precision requirements and matches the grab-cursor affordance visible on the header. The edit-mode overlay already covers the terminal area and intercepts pointer events, so making it the drag initiator adds no new event conflicts.

### Drag handle icon

The `⠿` (braille pattern) icon in the header is a drag handle affordance adopted from editors such as VSCode and Notion. It communicates "this element can be moved" without consuming much space. In edit mode it is rendered in `#4a7ea5`, which is visible against the `#1d2b3a` header background while staying subdued enough not to compete with the session type label.

---

## Edit Mode Toggle

The toggle button (`EditModeToggle`) lives at `position: fixed; bottom: 12px; right: 12px; z-index: 1000`.

| State | Background | Border | Color | Opacity | Shadow |
|---|---|---|---|---|---|
| OFF | `#3f3f46` | `#52525b` | `#888` | 0.65 | `0 2px 8px rgba(0,0,0,0.5)` |
| ON | `#1a3040` | `#569cd6` | `#569cd6` | 1.0 | `0 0 0 1px #569cd6, 0 2px 8px rgba(0,0,0,0.6)` |

The reduced opacity (0.65) in OFF state pushes the button into the background so it does not compete with terminal content. The full opacity, blue border, and blue glow in ON state make the active edit context unmistakable from the corner of the eye. Transitions on `opacity`, `box-shadow`, `border-color`, and `color` run at 0.2 s.
