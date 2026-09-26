# UI Design

This document describes the visual design decisions for the PaneMux frontend, covering always-available layout editing, drag-and-drop, the workspace bar, modal dialogs, and the task dashboard.

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

- the `← Tasks` button to the task dashboard, always first (see [Task Dashboard](#task-dashboard))
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

### Keyboard behaviour, shared by every modal

`aria-modal="true"` promises that the rest of the page is inert, and nothing in the DOM makes that
true on its own. `useModalKeyboard` supplies the two behaviours that attribute implies, and every
surface that declares it uses the hook: `ConfirmDialog`, `AddSSHHostDialog`, `PaneSettingsDialog`,
`CommandPalette`, `CommandHistoryPanel` and `BoardDashboardPanel`.

- **Focus stays inside.** Tab and Shift+Tab cycle within the dialog, and a Tab arriving from outside
  is pulled back in — forwards to the first focusable element, backwards to the last, the order a
  browser would have used had the background been inert. Without it, a dialog that moves focus once
  on open lets the next Tab reach the very controls it is asking about.
- **Escape is heard.** The listener is on the capture phase, because a focused xterm terminal stops
  keydown propagation and a bubble-phase window listener never sees the key. That is not an edge
  case: it is the state a dialog opened by a keyboard shortcut starts in, and the state the focus
  trap above exists to prevent the operator from reaching later.
- A dialog that must not be dismissed — one with a save in flight — passes no Escape handler. The
  focus trap still applies, so "you cannot leave yet" does not become "you cannot see where you are".
- A nested modal wins: while `PaneSettingsDialog`'s directory browser is open, the trap follows it
  and the form behind stays out of reach.

### Confirmation dialog

Destructive actions ask in `ConfirmDialog`, the app's own dialog, rather than in `window.confirm`
(issue [#70](https://github.com/tomo-chan/panemux/issues/70)). The native dialog blocks the main
thread — every terminal in the page stops rendering while it is up — and cannot carry the rest of
the UI's styling. `ConfirmDialog` follows the same surface as the other dialogs here: `#252526`
panel, `#444` border, backdrop click and `Escape` to dismiss.

Cancelling is the easy path by design: the cancel button, the backdrop and `Escape` all cancel, and
only the confirm button confirms. The confirm button takes focus when the dialog opens, so both
answers are one keystroke away. A destructive confirm button uses the same subdued red as the error
banner below (`#5a1d1d`, `#7f1d1d`, `#fca5a5`).

Its keyboard behaviour is the shared one above: focus is trapped between its two buttons, and
`Escape` reaches it from the capture phase.

Its first user is workspace deletion, which is still offered only in edit mode; the delete request
is sent when the dialog is confirmed and never before.

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

## Pane URL Strip

Opening a URL out of a pane can need the operator's attention twice: before anything opens, when a
program inside the pane asked for it, and after, when the port forward it needed could not be
established.

Both use one strip pinned to the top of the pane's terminal area, inside that pane rather than at
the workspace level, because both belong to one pane's activity:

- the request state names the URL and offers `Open` and `Ignore`; it never opens anything on its own
- the failure state reuses the transient-error palette (`#f4a9a9` on the pane's own surface with a
  `#7f1d1d` edge) and offers `Dismiss`
- a pending request takes precedence over an older failure, so the operator is never asked to read
  two things before deciding one
- the URL is truncated with an ellipsis and carries the full value as a tooltip, so a long
  authorization URL cannot push the buttons out of a narrow pane

The request state is deliberately not a modal: it belongs to one pane, and the operator should be
able to keep working in other panes — or read the surrounding terminal output that explains what
asked for it — before deciding.

---

## Attention Indicators

Agent-attention highlighting remains visually distinct from layout-editing affordances.

- pane attention uses an animated gold frame
- workspace attention uses an animated gold tab background
- move targets use blue overlays instead of gold

Using separate colors avoids mixing "this needs your attention" with "you can drop here".

---

## Task Dashboard

The task dashboard ([behavior](behavior/tasks.md)) is a second layer over the workspaces rather than
an overlay panel: it covers the whole window, and the workspaces stay mounted beneath it so every
terminal keeps its connection and scrollback. While it is shown the workspace layer is `inert`, so a
terminal that had focus cannot receive what is typed into the dashboard.

- **Switching layers.** The workspace bar starts with a `← Tasks` button, which carries the number
  of tasks waiting for input from the last collection in a gold badge; its accessible name says the
  count is "when last checked", since nothing collects while the workspaces are shown. The dashboard's top bar has a
  `Workspaces` button back, which shows the shortcut beside its label. `Cmd/Ctrl+Shift+S` switches
  either way; the letter is `display.task_dashboard_shortcut`. Like the palette's and the board's
  shortcuts it is registered on the keydown capture phase, so it fires while a terminal pane has
  focus. panemux opens on the workspaces. While the dashboard is shown, `Cmd/Ctrl+Shift+K` and
  `Cmd/Ctrl+Shift+B` do nothing, and the palette, history panel and Agent Board close when it
  appears: they belong to the workspace layer, which is inert then.
- **Top bar.** The title, one chip per host (`Local` for the panemux host) with its running count, a
  red chip with the error and a `Reconnect` button for a host that failed, and `connecting…` for a
  host whose connection is still coming up; then when the board was last updated, `Refresh`,
  `New task` (in the interactive blue), and `Workspaces`. A long host error is truncated in the chip
  and shown in full as its tooltip.
- **New task.** A modal form over the dashboard: Host (every host on the board, an unreachable one
  marked "(unreachable)"), Working directory, Agent (`claude` is the only choice), Labels
  (comma-separated, optional) and First instruction. The working directory takes focus when it
  opens. An empty directory, a relative one, or an empty instruction is refused in the form, with
  the reason under the fields; anything the server or the host refuses is shown there as
  "Could not start: …", and the form keeps what was typed. While the task starts, `Start` reads
  `Starting…`, and neither it, `Cancel`, `Escape` nor a click outside dismisses the form. Once it has
  started the form closes, a blue notice says which tmux session was started on which host and that
  the task is selected once claude has started, and the task is selected when a collection lists
  it — unless another task was selected meanwhile. No pane is opened. Labels that could not be
  recorded for a task that did start are reported in a red alert.
- **Filter bar.** Text filter over directory, branch, PR number and session ID; rows split by none,
  host, label, or repository; a host filter; a label filter listing every label on the board; and a
  `Done column` checkbox, off by default.
- **Kanban.** Columns in the order a person should look at them: Waiting for input (highlighted in
  gold, "Needs you"), Working, Idle, Running / unknown, Stopped, and Done when the checkbox asks for
  it. A task marked done sits in Done only while it is stopped, so with the column hidden a finished
  task leaves the board rather than crowding Stopped; one that runs again stays in its state's
  column. Column headers stay visible while the board scrolls, and each column is split into the
  chosen rows, with the catch-all row ("Not in a Git repository", "No label") last. Split by label, a
  task with two labels appears in both rows. A catch-all row is keyed apart from every name, so a
  label or repository that happens to be called "No label" gets a row of its own.
- **Cards.** A left border in the state's color; host, agent and how long the task has been in its
  state; the working directory's last segment as the title (there is no summary yet) with the full
  directory under it; for a waiting task its reason and "open the pane to respond"; repository,
  branch and PR link (new tab); its labels as colored tags, each label always the same color; a
  green `Done` tag in the meta line for a task marked done that is running again; and at the bottom
  where the task runs with its `Open` / `Go to pane` button, or `Resume` for a stopped claude
  task. A task that cannot be opened shows why instead of a button. The card is a pointer target
  for selection, and its title is a button, so the card never nests its links and buttons inside
  another interactive element.
- **Detail panel.** Fixed to the window height at the right. Its head — state, host, agent, start,
  title, waiting reason, the open action or the reason there is none (a stopped claude task offers
  `Resume` instead), and `Mark done` or `Mark not done` — stays in place, and only the body below scrolls: state notes, links, labels, the
  chain from task to agent to tmux session to pane to workspace, and the directory and session ID.
  `Mark done` asks first, inside the panel, and says where the task will be afterwards: a running
  task stays in its column until it stops; a stopped one moves to Done, and the question adds that
  the column is hidden until `Done column` is checked when it is. `Mark not done` does not ask. Labels
  are removed with the `×` on each and added with a text box and `Add`. A save that fails shows its
  reason in the head and keeps what was typed. A save belongs to the task it was made for: selecting
  another task while it runs leaves that task's controls enabled, and the result is not shown there.
  A task without a session ID offers neither, and says so. At 1000px and narrower it slides over the board
  with a close button.
- **Resume.** `Resume` reads `Resuming…` and is disabled while its request runs; a refusal is shown in
  a red alert naming the task, and a resumed task is selected. Nothing else on the board waits for it.
- **After opening.** The dashboard closes, the pane takes focus, and it is outlined in the
  interactive blue for about two seconds (a steady outline with reduced motion).

State colors:

| State | Color |
|---|---|
| Waiting for input | `#e2b86b` |
| Working | `#4ec9b0` |
| Idle, Running | `#8fa6c4` |
| Unknown | `#c586c0` |
| Stopped | `#80858d` |
| Done | `#7fae6a` |

Label colors come from the mock's palette (`#569cd6`, `#4ec9b0`, `#9cdcfe`, `#d7a26b`, `#b48ead`,
`#c678dd`, `#8a9199`, `#e06c6c`), picked by a hash of the label so a label keeps its color.

---

## Agent Board UI

Agent Board reuses the existing modal, panel, color, status, and focus-restoration patterns.

### Dashboard

- Opens from the Agent Board button or `Cmd/Ctrl+Shift+B`; both are absent when the capability is
  disabled.
- Appears as a right-side overlay and closes by button, backdrop, or `Escape`.
- Lists the union of configured board panes and panes still reporting. A configured pane with no
  report remains visible as `not joined`; a removed pane with a report does not disappear silently.
- Shows pane ID, title, state, summary, last tool, and report age. It does not show self-reported
  repository, branch, or PR values because the pane header already provides panemux-derived values.
- Treats state as free text and maps known states to the existing status palette: `working` green,
  `idle` blue, `waiting` attention gold, and everything else neutral.
- Keeps identity and metadata on one line; summaries may wrap to four lines.
- Marks reports older than five minutes as `stale` and dims rather than hides them.

### Command center

- `Cmd/Ctrl+Shift+K` opens a focused command palette. Plain `Cmd/Ctrl+K` remains available to common
  shell/readline bindings.
- Each prompt creates one turn containing the prompt, streamed output, progress state, and any
  inline error. A failed turn does not close or disable the palette.
- Command history uses a right-side panel for longer reading alongside terminal work.
- Command-center entry points are absent when the capability is disabled.

Global shortcuts use capture phase so terminal focus does not swallow them. Closing any Agent Board
surface restores the previously focused element. Full data-flow rules are in
[Command center](agent-board/command-center.md#command-center).
