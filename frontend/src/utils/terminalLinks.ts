import type { IBufferLine, ILink, ILinkProvider, Terminal } from '@xterm/xterm'

// The web-links addon stops expanding a wrapped block at 2048 characters; the
// same ceiling is kept here so a pathological screen cannot turn one hover into
// an unbounded walk up and down the buffer.
const MAX_BLOCK_LENGTH = 2048

/** A buffer position, 0-based, as the buffer itself addresses cells. */
interface CellRef {
  x: number
  y: number
}

interface LineText {
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
    // Width 0 is the second cell of a wide character; it carries no text.
    if (width === 0) continue

    const cellChars = cell.getChars()
    if (x === 0) startsBlank = isBlank(cellChars)
    if (x === line.length - 1) reachesEdge = !isBlank(cellChars)

    const text = cellChars === '' ? ' ' : cellChars
    for (const char of text) {
      chars.push(char)
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

/**
 * collectLinkBlock joins the rows around `y` that read as one continuous line.
 *
 * endsAtEdge reports the case the safety net needs: the last row runs to the
 * pane edge and nothing continues it, so anything matching right up to that
 * edge is probably cut off rather than complete.
 */
function collectLinkBlock(term: Terminal, y: number): LinkBlock | null {
  const start = readLine(term, y)
  if (!start) return null

  const lines: LineText[] = [start]

  for (let above = y - 1; above >= 0; above--) {
    const line = readLine(term, above)
    if (!line) break
    if (!continuesOnNextLine(line, lines[0], isWrapped(term, above + 1))) break
    lines.unshift(line)
    if (lines.reduce((total, l) => total + l.text.length, 0) > MAX_BLOCK_LENGTH) break
  }

  let last = lines[lines.length - 1]
  let endsAtEdge = last.reachesEdge
  for (let below = y + 1; ; below++) {
    const line = readLine(term, below)
    if (!line) break
    if (!continuesOnNextLine(last, line, isWrapped(term, below))) break
    lines.push(line)
    last = line
    endsAtEdge = line.reachesEdge
    if (lines.reduce((total, l) => total + l.text.length, 0) > MAX_BLOCK_LENGTH) break
  }

  return {
    text: lines.map((line) => line.text).join(''),
    positions: lines.flatMap((line) => line.positions),
    endsAtEdge,
  }
}

function cellWidth(term: Terminal, position: CellRef): number {
  return term.buffer.active.getLine(position.y)?.getCell(position.x)?.getWidth() ?? 1
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
        const startCell = block.positions[startIndex]
        const endCell = block.positions[endIndex]
        if (!startCell || !endCell) continue

        // The safety net: a match that runs to the pane edge of the last row
        // in the block, with nothing continuing it, is very likely the front
        // of a URL whose rest is not on screen. Linking it would open a
        // different address than the one it appears to be.
        if (block.endsAtEdge && endIndex === block.text.length - 1) continue

        const text = match[0]
        links.push({
          range: {
            start: { x: startCell.x + 1, y: startCell.y + 1 },
            end: { x: endCell.x + cellWidth(term, endCell), y: endCell.y + 1 },
          },
          text,
          activate: () => onActivate(text),
        })
      }

      callback(links.length > 0 ? links : undefined)
    },
  }
}
