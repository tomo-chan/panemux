import { PreviewResponseSchema, type PreviewResponse } from '../schemas'

// The split preview (issue #385): a workspace pane or the task dashboard's
// Type in pane popup shows its terminal on the left and the HTML or Markdown
// file a clicked path names on the right.

// Characters a file path is made of, as a terminal prints it. Letters are any
// script's, so `docs/設計.md` is a path; CJK punctuation is not a letter, so
// `「docs/a.md」` ends where the path does.
const PATH_CHARS = '\\p{L}\\p{N}_\\-.@+=%/'

/**
 * Matches a local HTML or Markdown path in terminal output: absolute
 * (`/w/index.html`), dot-relative (`./mockups/index.html`) or bare
 * (`docs/design.md`). It never starts inside another path or a URL, or after
 * `~`, which the server would not expand; a trailing `:12` or `.` is left out.
 * Kept non-global, like TERMINAL_URL_REGEX.
 */
export const PREVIEW_PATH_REGEX = new RegExp(
  `(?<![${PATH_CHARS}:~\\\\])[${PATH_CHARS}]*[\\p{L}\\p{N}_\\-@+=%]\\.(?:html?|md|markdown)` +
    `(?![${PATH_CHARS.replace('.', '')}]|\\.[\\p{L}\\p{N}])`,
  'iu',
)

/** The terminal's share of the split, in percent. */
export const PREVIEW_SPLIT_MIN = 20
export const PREVIEW_SPLIT_MAX = 80
export const PREVIEW_SPLIT_DEFAULT = 50
/** Each arrow key press moves the divider this far. */
export const PREVIEW_SPLIT_STEP = 5
/** Neither side is drawn narrower than this, whatever the ratio. */
export const PREVIEW_SIDE_MIN_PX = 160
/** The divider's own width, as preview.css draws it. */
export const PREVIEW_DIVIDER_PX = 6

/**
 * The ratios a split `width` pixels wide can show: within 20 to 80, and
 * leaving the terminal and the preview (with the divider) 160 px each. Where
 * both cannot fit, the split stays at 50:50. An unknown width (0) leaves the
 * fixed range.
 */
export function previewSplitBounds(width: number): { min: number; max: number } {
  if (!(width > 0)) return { min: PREVIEW_SPLIT_MIN, max: PREVIEW_SPLIT_MAX }
  const min = Math.max(PREVIEW_SPLIT_MIN, Math.ceil((PREVIEW_SIDE_MIN_PX * 100) / width))
  const max = Math.min(PREVIEW_SPLIT_MAX, Math.floor(((width - PREVIEW_DIVIDER_PX - PREVIEW_SIDE_MIN_PX) * 100) / width))
  if (min > max) return { min: PREVIEW_SPLIT_DEFAULT, max: PREVIEW_SPLIT_DEFAULT }
  return { min, max }
}

export function clampPreviewSplit(value: number): number {
  if (!Number.isFinite(value)) return PREVIEW_SPLIT_DEFAULT
  return Math.min(PREVIEW_SPLIT_MAX, Math.max(PREVIEW_SPLIT_MIN, value))
}

export function paneSplitKey(paneId: string): string {
  return `panemux:preview-split:pane:${paneId}`
}

export function taskSplitKey(taskId: string): string {
  return `panemux:preview-split:task:${taskId}`
}

// The ratio is a per-browser display preference, kept in localStorage like
// the popup's maximized state; a browser that refuses storage starts at 50:50.
export function loadPreviewSplit(key: string): number {
  try {
    const stored = window.localStorage.getItem(key)
    return stored === null ? PREVIEW_SPLIT_DEFAULT : clampPreviewSplit(Number.parseFloat(stored))
  } catch {
    return PREVIEW_SPLIT_DEFAULT
  }
}

export function savePreviewSplit(key: string, value: number) {
  try {
    window.localStorage.setItem(key, String(value))
  } catch {
    // Storage is unavailable or full: the ratio lasts until the page reloads.
  }
}

export function previewHostLabel(host: string): string {
  return host === '' ? 'local' : host
}

/**
 * Asks the server to resolve a clicked path from the session's working
 * directory and register the file for the preview.
 */
export async function requestPreview(sessionId: string, path: string): Promise<PreviewResponse> {
  const res = await fetch(`/api/sessions/${encodeURIComponent(sessionId)}/preview`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path }),
  })
  if (!res.ok) {
    const message = (await res.text()).trim()
    throw new Error(message || `preview failed (${res.status})`)
  }
  return PreviewResponseSchema.parse(await res.json())
}
