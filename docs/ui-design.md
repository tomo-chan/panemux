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

The workspace bar now also carries compact operational summaries for each workspace. Detailed pane status is integrated with the workspace tabs themselves instead of living in a separate persistent rail.

### Contents

The bar can contain:

- workspace tabs
- per-workspace summary text
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

For left/right bars, the bar width is user-resizable by dragging its inner edge. The chosen width is shared across workspaces and restored on reload, while the pane layout inside the remaining work area continues to use its own saved percentage splits.

Workspace creation uses a single `+` control because it is now the only creation action in the bar and does not need extra wording to distinguish it from terminal creation.

### Integrated workspace summaries

Each workspace tab can show a compact one-line summary of that workspace:

- pane count
- connected pane count
- disconnected pane count
- exited pane count
- pending pane count when session data has not arrived yet

Each summarized workspace tab also shows a compact pane-name strip so users can see what lives in that workspace without opening the full detail view.

### Workspace pane groups

Each workspace renders as a grouped strip anchored to its tab surface. The pane cards are not an independent dashboard panel; they are visually and interactively subordinate to the workspace tab they belong to.

Presentation depends on bar position:

- top/bottom bars: pane cards appear as a hover/focus overlay attached to the tab
- left/right bars: pane cards stay expanded inline under the tab

When the bar is on the left or right, the tab list and inline pane groups live in a dedicated scroll region. The fixed footer below them keeps the `+` action and all four tab-position buttons visible even when the workspace list is taller than the viewport.

Each pane group shows:

- workspace title and aggregate counts
- one card per pane
- pane type
- pane connection state
- repository, branch, and PR number when available
- attention badges for panes that need input
- a stronger selection treatment for the currently focused pane

Clicking a pane card switches to that workspace if needed, focuses the corresponding pane, and clears pane/workspace attention. This makes the summary surface a status-driven navigator rather than a passive label.

Pane cards are also drag sources for cross-workspace moves. The drag gesture starts from the overview card itself, not from the pane body, so terminal interaction and layout interaction remain separate.

---

## Pane Header

The pane header remains the compact command strip for each pane.

### Layout

From left to right, the header contains:

- drag handle `⠿`
- connection status dot
- pane type label
- optional pane title
- optional git information and linked PR shortcut when the current branch has a GitHub pull request
- reconnecting status text
- action buttons aligned to the right

The header background stays `#252526` with a `#333` bottom border so the new controls inherit the established terminal chrome instead of introducing a second visual language.

When panemux can derive a repository page URL from the pane's Git origin, the repository name is rendered as an inline text link using the same visual treatment as the PR shortcut.

The PR shortcut is rendered as an inline text link labeled `#<number>`. It does not use a pill, badge, or outlined capsule treatment. On hover, the link shifts to a slightly stronger blue and shows an underline so it reads like the rest of the header chrome instead of a separate button system.

### Multiple worktrees

When the pane's active agent has diverged into exactly one sibling worktree, the header shows that worktree's repo, branch, and PR shortcut inline, exactly as described above.

When two or more distinct worktrees are active at once (for example, several Claude Task subagents each working in a different sibling worktree), the header does not try to fit every branch and PR shortcut inline — the compact header would not have room and would become unreadable. Instead the header shows a single inline text trigger labeled `<N> worktrees` (e.g. `2 worktrees`), styled like the other inline text links rather than as a pill or badge. Clicking the trigger opens a small popover menu anchored below the header, listing every active worktree as its own `repo ⎇ branch #<number>` line using the same inline-link treatment as the single-worktree case. The menu closes on Escape or on clicking outside it.

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

## Transient Error Banner

Pane creation and moves are optimistic in the UI and then persisted. If persistence fails, the user needs immediate feedback because the visible layout can temporarily diverge from saved config.

The create/move error banner pattern:

- appears in the top-right of the workspace content area
- uses a destructive but subdued palette (`#2f1313`, `#7f1d1d`, `#fca5a5`)
- includes an explicit dismiss button
- remains separate from modal dialog errors because it belongs to an in-place interaction, not a form
- stacks vertically when both a create failure and a move failure are present at the same time

### Error state lifecycle

The banner state is:

- hidden by default
- visible when a pane-create or pane-move persistence request rejects
- hidden again when dismissed or when the next create/move attempt starts

This keeps the error noticeable without forcing it into a blocking dialog.

---

## Attention Indicators

Agent-attention highlighting remains visually distinct from layout-editing affordances.

- pane attention uses an animated gold frame
- workspace attention uses an animated gold tab background
- move targets use blue overlays instead of gold

Using separate colors avoids mixing "this needs your attention" with "you can drop here".

---

## Agent Board UI

> **Status: implemented.** The command center palette, history panel, and the status dashboard
> (`BoardDashboardPanel.tsx`) all ship. Full design lives in [agent-board.md](agent-board.md).

Agent Board (see [agent-board.md](agent-board.md)) introduces two new UI surfaces, both layered on
top of the principles above rather than replacing them:

- A **dashboard** view of per-pane self-reported status (state, branch, PR, summary — see
  [agent-board.md's Status self-report](agent-board.md#status-self-report-and-message-flow)) —
  **implemented** as `BoardDashboardPanel.tsx`, a right-anchored overlay panel following the same
  structure and styling tokens as `CommandHistoryPanel.tsx` (dark `#252526` panel, `#444` border,
  420px wide, backdrop click and `Escape` to dismiss) rather than a new visual language. It opens via
  an "Agent Board" button next to the existing "Command History" button (shown only when
  `agent_board_enabled` is true and a token is available) or via `Cmd/Ctrl+Shift+B`, registered on
  the keydown capture phase the same way the palette's own shortcut is, so it still fires while a
  terminal pane has focus. It extends the existing workspace-bar/pane-card status vocabulary
  (**Integrated workspace summaries** and **Workspace pane groups**, above) rather than introduce a
  competing one: the same 8px status-dot-plus-`${color}33`-ring treatment, the same repo (`#9fcbff`)
  / branch (`#8f98a8`) / `PR #{n}` chrome as `WorkspaceTabs.tsx`. The self-reported `state` string is
  free text (an agent's own report, not a fixed enum), mapped to a dot color via
  `utils/boardStatusColors.ts`: `working` → `#7bd88f`, `idle` → `#7aa2f7`, `waiting` → `#f4bf4f`
  (deliberately reusing the existing attention-gold pill color, since "waiting" is the same kind of
  "needs a look" signal), and any other or missing value → a neutral `#4b5565` rather than a crash or
  blank dot. A status entry whose `updated_at` is older than 5 minutes gets a `stale` pill and its
  card rendered at 60% opacity — dimmed, not hidden, since a stale report is still the most recent
  information available for that pane. No new colors were introduced for the dashboard, matching the
  rest of Agent Board's UI (see below).
- A **Spotlight-style command palette** (`CommandPalette.tsx`) and **history panel**
  (`CommandHistoryPanel.tsx`) for the [command center](agent-board.md#command-center) —
  **implemented.** The palette follows this document's existing **Modal Dialogs** pattern (a
  higher-friction, focused interaction, not compressed into inline chrome): dark `#252526` panel,
  `#444` border, backdrop click and `Escape` to dismiss, matching `AddSSHHostDialog`'s own styling
  tokens rather than introducing new ones. The history panel follows the same overlay pattern as a
  right-anchored sliding panel rather than a centered modal, since it's meant to stay open alongside
  other work rather than demand focus the way the palette does. All three overlays (palette, history
  panel, dashboard) share one `useRestoreFocusOnClose` hook that returns keyboard focus to whatever
  element had it before the overlay opened, once the overlay closes or unmounts — so dismissing any
  of them (Escape, backdrop click, or the close button) never strands focus on a removed panel.

Concrete decisions this section originally deferred to implementation time:

- **Palette keybinding**: `Cmd/Ctrl+Shift+K`, not plain `Cmd/Ctrl+K` — the latter is already bound in
  many shells/readline setups a terminal pane might be running, and would be captured as literal pane
  input rather than reaching the browser as a global shortcut. Registered on the keydown capture
  phase specifically so it still fires while a terminal pane has focus.
  See [agent-board.md's UI subsection](agent-board.md#ui) for the full rationale.
- **Color treatment**: reuses this document's existing dark palette exactly (`#252526` panels, `#444`
  borders, `#d4d4d4` body text, `#4ec9b0` for the user's own prompt echo, `#f44747` for errors) — no
  new colors were introduced for Agent Board.
- **Streaming-response layout**: turn-based, not a single scrolling log — each submitted prompt opens
  a new turn block showing the prompt followed by its streamed `stream-json` lines as they arrive,
  an ellipsis while still in flight, and either nothing further (success) or a red error/busy line at
  the end.
- **Error presentation**: inline within the turn that failed (red text, same treatment as this
  document's existing form-validation error styling), not a separate banner or toast — an error is
  scoped to the one query that produced it, and the palette stays open and usable for the next prompt.
