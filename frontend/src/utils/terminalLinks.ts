import type { IBufferLine, IBufferRange, ILink, ILinkProvider, Terminal } from '@xterm/xterm'

// The web-links addon stops expanding a wrapped block at 2048 characters; the
// same ceiling is kept here so a pathological screen cannot turn one hover into
// an unbounded walk up and down the buffer.
const MAX_BLOCK_LENGTH = 2048

/** A buffer position, 0-based, as the buffer itself addresses cells. */
export interface CellRef {
  x: number
  y: number
}

export interface LineText {
  /** The line's text with trailing blanks removed. */
  text: string
  /** positions[i] is the cell that text[i] was read from. */
  positions: CellRef[]
  /** True when the line's last cell holds something, i.e. it runs to the pane edge. */
  reachesEdge: boolean
  /** True when the line is empty or starts with a blank. */
  startsBlank: boolean
}

function isBlank(chars: string): boolean {
  return chars === '' || chars === ' '
}

/**
 * readLine turns one buffer line into a string plus the cell each character came
 * from. It reads cells rather than calling translateToString because the
 * positions are the whole point: a wide character occupies two cells but one
 * string index, and a link's range is in cells.
 *
 * Exported as `readLineCells` for the pull-request link provider, which needs
 * the same mapping: it used to index `translateToString`'s output as if one
 * character were one cell, which is only true until a wide character appears.
 */
function readLine(term: Terminal, y: number): LineText | null {
  const line: IBufferLine | undefined = term.buffer.active.getLine(y)
  if (!line) return null

  const chars: string[] = []
  const positions: CellRef[] = []
  let reachesEdge = false
  let startsBlank = true

  for (let x = 0; x < line.length; x++) {
    const cell = line.getCell(x)
    if (!cell) continue
    const width = cell.getWidth()
    const cellChars = cell.getChars()

    if (x === 0) startsBlank = isBlank(cellChars)
    // Decided before the width check below, because a width-0 cell is the
    // trailing half of a wide character, and a wide character whose second
    // half sits in the last column does occupy the pane's edge. Reading this
    // after the `continue` left reachesEdge false for every row ending in a
    // CJK glyph or an emoji — which switched off both the row joining and the
    // cut-off-url suppression for exactly that content.
    if (x === line.length - 1) reachesEdge = width === 0 || !isBlank(cellChars)

    // Width 0 carries no text of its own; its character was recorded by the
    // cell before it.
    if (width === 0) continue

    const text = cellChars === '' ? ' ' : cellChars
    // One entry per UTF-16 code unit, not per code point: `text` is indexed in
    // code units downstream (the regex's match.index, and the slice below), so
    // an astral character — an emoji, a plane-2 ideograph — must contribute
    // two entries. Iterating with `for...of` walks code points and left
    // `positions` one short per astral character, which shifted every later
    // link's range and dropped the link outright once the drift ran off the
    // end. Joining single units back together reproduces the original string
    // exactly, surrogate pairs included.
    for (let i = 0; i < text.length; i++) {
      chars.push(text[i])
      positions.push({ x, y })
    }
  }

  let end = chars.length
  while (end > 0 && chars[end - 1] === ' ') end--

  return {
    text: chars.slice(0, end).join(''),
    positions: positions.slice(0, end),
    reachesEdge,
    startsBlank,
  }
}

/**
 * continuesOnNextLine decides whether `lower` carries on from `upper`.
 *
 * `isWrapped` alone — which is all the web-links addon consults — is only set
 * when the terminal wrapped the line itself. A program that prints its own
 * newline at the pane edge (issue #175: a bordered TUI, a formatted CLI, tmux
 * redrawing by line) produces a line that is visually wrapped and carries no
 * flag, and its URL was linked only as far as the first row.
 *
 * The heuristic is deliberately narrow: the upper line must run all the way to
 * the pane edge, and the lower line must start with something other than a
 * blank. It can still join two unrelated lines that happen to meet those
 * conditions, which is the trade this issue accepts — the alternative is a
 * fragment that stays clickable and opens a host the operator never saw.
 */
function continuesOnNextLine(upper: LineText, lower: LineText, lowerIsWrapped: boolean): boolean {
  if (lower.text === '') return false
  if (lowerIsWrapped) return true
  return upper.reachesEdge && !lower.startsBlank
}

function isWrapped(term: Terminal, y: number): boolean {
  return term.buffer.active.getLine(y)?.isWrapped ?? false
}

interface LinkBlock {
  text: string
  positions: CellRef[]
  /** True when the block ends because nothing continued it, not because of the ceiling. */
  endsAtEdge: boolean
}

interface BorderDecoration {
  left: { char: string; x: number }
  right: { char: string; x: number }
}

interface BlockLine {
  raw: LineText
  content: LineText
  border: BorderDecoration | null
}

function isBoxDrawing(char: string): boolean {
  return char.length === 1 && char >= '\u2500' && char <= '\u257f'
}

/**
 * A drawn frame is deliberately narrower than "punctuation at both ends":
 * both ends must be Unicode box-drawing characters. In particular, ASCII `|`
 * is ordinary command output surprisingly often, so treating it as a border
 * would silently join unrelated lines.
 *
 * One inner blank on either side is treated as frame padding. The two sides do
 * not have to use the same character or padding, and a frame with no padding
 * works as well. Keeping the original positions alongside the slice is what
 * preserves xterm's cell coordinates after those decorations disappear.
 */
function readBlockLine(term: Terminal, y: number): BlockLine | null {
  const raw = readLine(term, y)
  if (!raw) return null

  let left = 0
  while (raw.text[left] === ' ') left++
  const last = raw.text.length - 1
  if (left >= last || !isBoxDrawing(raw.text[left]) || !isBoxDrawing(raw.text[last])) {
    return { raw, content: raw, border: null }
  }

  const leftPosition = raw.positions[left]
  const rightPosition = raw.positions[last]
  if (!leftPosition || !rightPosition || leftPosition.x >= rightPosition.x) {
    return { raw, content: raw, border: null }
  }

  let start = left + 1
  if (raw.text[start] === ' ') start++
  let end = last
  if (raw.text[end - 1] === ' ') end--

  const chars = raw.text.slice(start, end).split('')
  const positions = raw.positions.slice(start, end)
  const reachesEdge = chars.length > 0 && chars[chars.length - 1] !== ' '
  const startsBlank = chars.length === 0 || chars[0] === ' '

  let trimmedEnd = chars.length
  while (trimmedEnd > 0 && chars[trimmedEnd - 1] === ' ') trimmedEnd--

  return {
    raw,
    content: {
      text: chars.slice(0, trimmedEnd).join(''),
      positions: positions.slice(0, trimmedEnd),
      reachesEdge,
      startsBlank,
    },
    border: {
      left: { char: raw.text[left], x: leftPosition.x },
      right: { char: raw.text[last], x: rightPosition.x },
    },
  }
}

function sharesBorder(upper: BlockLine, lower: BlockLine): boolean {
  if (!upper.border || !lower.border) return false
  return (
    upper.border.left.char === lower.border.left.char &&
    upper.border.left.x === lower.border.left.x &&
    upper.border.right.char === lower.border.right.char &&
    upper.border.right.x === lower.border.right.x
  )
}

type BlockMode = 'raw' | 'border'

function continuationMode(
  upper: BlockLine,
  lower: BlockLine,
  lowerIsWrapped: boolean,
  mode: BlockMode | null,
): BlockMode | null {
  // A real terminal wrap is authoritative. It also prevents text that merely
  // starts and ends with box-drawing glyphs from being reinterpreted as a
  // frame when xterm says it is one physical logical line.
  if (mode !== 'border' && lowerIsWrapped) {
    return continuesOnNextLine(upper.raw, lower.raw, true) ? 'raw' : null
  }

  if (mode !== 'raw' && sharesBorder(upper, lower)) {
    return continuesOnNextLine(upper.content, lower.content, false) ? 'border' : null
  }

  // Once either row looks framed, do not fall back to the old edge heuristic:
  // its right border fills the last cell and its left border is non-blank,
  // which is precisely the corrupt `...││...` join this path replaces.
  if (mode === 'border' || upper.border || lower.border) return null
  return continuesOnNextLine(upper.raw, lower.raw, false) ? 'raw' : null
}

/**
 * collectLinkBlock joins the rows around `y` that read as one continuous line.
 *
 * endsAtEdge reports the case the safety net needs: the last row runs to the
 * pane edge and nothing continues it, so anything matching right up to that
 * edge is probably cut off rather than complete.
 */
function collectLinkBlock(term: Terminal, y: number): LinkBlock | null {
  const start = readBlockLine(term, y)
  if (!start) return null

  const lines: BlockLine[] = [start]
  let mode: BlockMode | null = null

  const selected = (line: BlockLine): LineText => (mode === 'border' ? line.content : line.raw)
  const length = (): number => lines.reduce((total, line) => total + selected(line).text.length, 0)

  for (let above = y - 1; above >= 0; above--) {
    const line = readBlockLine(term, above)
    if (!line) break
    const nextMode = continuationMode(line, lines[0], isWrapped(term, above + 1), mode)
    if (!nextMode) break
    mode = nextMode
    lines.unshift(line)
    if (length() > MAX_BLOCK_LENGTH) break
  }

  let last = lines[lines.length - 1]
  let endsAtEdge = selected(last).reachesEdge
  for (let below = y + 1; ; below++) {
    const line = readBlockLine(term, below)
    if (!line) break
    const nextMode = continuationMode(last, line, isWrapped(term, below), mode)
    if (!nextMode) break
    mode = nextMode
    lines.push(line)
    last = line
    endsAtEdge = selected(line).reachesEdge
    if (length() > MAX_BLOCK_LENGTH) break
  }

  return {
    text: lines.map((line) => selected(line).text).join(''),
    positions: lines.flatMap((line) => selected(line).positions),
    endsAtEdge,
  }
}

function cellWidth(term: Terminal, position: CellRef): number {
  return term.buffer.active.getLine(position.y)?.getCell(position.x)?.getWidth() ?? 1
}

export { readLine as readLineCells }

/**
 * bufferRangeFor converts a [startIndex, endIndex] span of a line's text into
 * the range xterm wants: 1-based on both axes, with the end column inclusive
 * of the last character's own width.
 *
 * Both link providers in this app go through it, because xterm decides which
 * of two overlapping links wins by comparing their ranges — a provider using a
 * different coordinate system does not lose that comparison, it never enters
 * it.
 */
export function bufferRangeFor(
  term: Terminal,
  positions: CellRef[],
  startIndex: number,
  endIndex: number,
): IBufferRange | null {
  const startCell = positions[startIndex]
  const endCell = positions[endIndex]
  if (!startCell || !endCell) return null

  return {
    start: { x: startCell.x + 1, y: startCell.y + 1 },
    end: { x: endCell.x + cellWidth(term, endCell), y: endCell.y + 1 },
  }
}

/**
 * createUrlLinkProvider replaces @xterm/addon-web-links for URL detection.
 *
 * The addon is not extensible where it matters: `_getWindowedLineStrings` joins
 * rows on `isWrapped` alone, and its `isUrl` check accepts a cut-off fragment
 * like `https://exam` as a perfectly good URL. Owning the provider is what lets
 * a hard-wrapped URL be linked whole and a truncated one not be linked at all
 * (issue #175).
 *
 * Register it before any other URL provider: xterm's linkifier keeps the link
 * from the earliest-registered provider when two intersect.
 */
export function createUrlLinkProvider(
  term: Terminal,
  urlRegex: RegExp,
  onActivate: (uri: string) => void,
): ILinkProvider {
  // A fresh global copy per call: exec with lastIndex must not be shared, and
  // the source regex is deliberately non-global (see TERMINAL_URL_REGEX).
  const pattern = new RegExp(urlRegex.source, urlRegex.flags.replace('g', '') + 'g')

  return {
    provideLinks(y, callback) {
      const block = collectLinkBlock(term, y - 1)
      if (!block || block.text === '') {
        callback(undefined)
        return
      }

      const links: ILink[] = []
      pattern.lastIndex = 0
      let match: RegExpExecArray | null
      while ((match = pattern.exec(block.text)) !== null) {
        const startIndex = match.index
        const endIndex = match.index + match[0].length - 1

        // The safety net: a match that runs to the pane edge of the last row
        // in the block, with nothing continuing it, is very likely the front
        // of a URL whose rest is not on screen. Linking it would open a
        // different address than the one it appears to be.
        if (block.endsAtEdge && endIndex === block.text.length - 1) continue

        const range = bufferRangeFor(term, block.positions, startIndex, endIndex)
        if (!range) continue

        const text = match[0]
        links.push({ range, text, activate: () => onActivate(text) })
      }

      callback(links.length > 0 ? links : undefined)
    },
  }
}
