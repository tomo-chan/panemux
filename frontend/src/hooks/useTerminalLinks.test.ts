import { describe, it, expect, vi } from 'vitest'
import { Terminal } from '@xterm/xterm'
import type { ILink } from '@xterm/xterm'
import { TERMINAL_URL_REGEX, __computePullRequestLinksForTests } from './useTerminal'
import { createUrlLinkProvider } from '../utils/terminalLinks'

// Integration check against the real xterm.js terminal. useTerminal.test.ts mocks
// the terminal and this provider, so it can only assert how panemux wires them
// together; this file verifies what the provider actually detects in a real
// buffer — TERMINAL_URL_REGEX's CJK handling, and the wrapped-URL behavior issue
// #175 is about.
async function detectLinksAt(
  input: string,
  { cols = 200, row = 1 }: { cols?: number; row?: number } = {},
): Promise<ILink[]> {
  const term = new Terminal({ cols, rows: 10, allowProposedApi: true })
  const provider = createUrlLinkProvider(term, TERMINAL_URL_REGEX, vi.fn())
  await new Promise<void>((resolve) => term.write(input, resolve))

  const links = await new Promise<ILink[] | undefined>((resolve) => {
    provider.provideLinks(row, resolve)
  })
  term.dispose()
  return links ?? []
}

const detectLinks = (line: string) => detectLinksAt(line)

describe('url link provider: single line', () => {
  it('excludes a trailing ideographic full stop from the link text and range', async () => {
    const links = await detectLinks('参照: https://example.com/docs。')

    expect(links).toHaveLength(1)
    expect(links[0].text).toBe('https://example.com/docs')
    // 1-based columns, right side including: '参照: ' occupies 6 cells (参 and 照 are wide).
    expect(links[0].range).toEqual({
      start: { x: 7, y: 1 },
      end: { x: 30, y: 1 },
    })
  })

  it('excludes enclosing fullwidth parentheses from the link', async () => {
    const links = await detectLinks('（https://example.com/docs）')

    expect(links).toHaveLength(1)
    expect(links[0].text).toBe('https://example.com/docs')
  })

  it('keeps a raw IRI path intact', async () => {
    const links = await detectLinks('https://ja.wikipedia.org/wiki/日本語。')

    expect(links).toHaveLength(1)
    expect(links[0].text).toBe('https://ja.wikipedia.org/wiki/日本語')
  })

  it('detects both urls on a line delimited only by CJK punctuation', async () => {
    const links = await detectLinks('（https://a.example.com/x）と「https://b.example.com/y」')

    expect(links.map((link) => link.text)).toEqual([
      'https://a.example.com/x',
      'https://b.example.com/y',
    ])
  })

  it('activates with the whole url', async () => {
    const term = new Terminal({ cols: 80, rows: 10, allowProposedApi: true })
    const onActivate = vi.fn()
    const provider = createUrlLinkProvider(term, TERMINAL_URL_REGEX, onActivate)
    await new Promise<void>((resolve) => term.write('see https://example.com/docs', resolve))

    const links = await new Promise<ILink[] | undefined>((resolve) => provider.provideLinks(1, resolve))
    links?.[0].activate(new MouseEvent('click'), links[0].text)

    expect(onActivate).toHaveBeenCalledWith('https://example.com/docs')
    term.dispose()
  })
})

// The real bug: a URL the terminal itself wrapped is already handled, but one the
// program wrapped by printing its own newline at the pane edge was linked only as
// far as its first line — and that fragment stayed clickable, so a URL cut inside
// its hostname opened a different host.
// Two shapes the CJK cases above cannot reach, both found in review of #175's
// own implementation: an astral character occupies two UTF-16 code units but
// one code point, and a wide character can occupy the pane's last column with
// its trailing half.
describe('url link provider: characters that are not one unit per cell', () => {
  // The cell arithmetic below is one cell per emoji, which looks wrong until
  // you check which width table is in play: panemux does not load
  // @xterm/addon-unicode11, so xterm's default V6 provider is what decides,
  // and it gives these emoji width 1. The point of these cases is the *code
  // unit* mapping regardless of that — an emoji is one code point and two
  // UTF-16 code units, and the regex indexes the block in code units.
  it('keeps the range right when an astral character precedes the url', async () => {
    const links = await detectLinks('🎉 https://example.com/docs')

    expect(links).toHaveLength(1)
    expect(links[0].text).toBe('https://example.com/docs')
    // 1-based, right side including: the emoji holds cell 1, the space cell 2.
    expect(links[0].range).toEqual({
      start: { x: 3, y: 1 },
      end: { x: 26, y: 1 },
    })
  })

  it('still links a url that follows several astral characters', async () => {
    // Three emoji are three cells but six code units: a per-code-point map
    // drifts three places here, which is what used to drop the link.
    const links = await detectLinks('🎉🎉🎉 https://example.com/docs')

    expect(links).toHaveLength(1)
    expect(links[0].text).toBe('https://example.com/docs')
    expect(links[0].range.start).toEqual({ x: 5, y: 1 })
  })

  it('keeps the range right when the url itself ends in an astral character', async () => {
    const links = await detectLinks('https://example.com/emoji/🎉')

    expect(links).toHaveLength(1)
    expect(links[0].text).toBe('https://example.com/emoji/🎉')
    expect(links[0].range.start).toEqual({ x: 1, y: 1 })
    // The match ends on the emoji's low surrogate, which maps to the same
    // cell as its high surrogate — cell 27, one wide.
    expect(links[0].range.end).toEqual({ x: 27, y: 1 })
  })

  it('joins a hard-wrapped row whose last column holds the back half of a wide character', async () => {
    // 40 columns: 20 single-cell characters then 10 wide ones fills the row
    // exactly, so its last column is the trailing half of a wide glyph.
    const head = `https://example.com/${'参照'.repeat(5)}`
    const tail = '/more/path'
    const links = await detectLinksAt(`${head}\r\n${tail}`, { cols: 40 })

    expect(links).toHaveLength(1)
    expect(links[0].text).toBe(head + tail)
    expect(links[0].range.start).toEqual({ x: 1, y: 1 })
    expect(links[0].range.end).toEqual({ x: tail.length, y: 2 })
  })

  it('suppresses a cut-off url whose row ends in a wide character', async () => {
    const head = `https://example.com/${'参照'.repeat(5)}`
    const links = await detectLinksAt(`${head}\r\n  indented`, { cols: 40 })

    expect(links).toEqual([])
  })
})

describe('url link provider: wrapped urls', () => {
  const url = 'https://example.com/a/very/long/path/that/does/not/fit/in/one/row/at/all'

  it('detects a soft-wrapped url as one link across its rows', async () => {
    const links = await detectLinksAt(url, { cols: 40 })

    expect(links).toHaveLength(1)
    expect(links[0].text).toBe(url)
    expect(links[0].range.start).toEqual({ x: 1, y: 1 })
    expect(links[0].range.end.y).toBeGreaterThan(1)
  })

  it('detects a hard-wrapped url as one link across its rows', async () => {
    // 40 columns, broken by the program itself exactly at the pane edge.
    const links = await detectLinksAt(`${url.slice(0, 40)}\r\n${url.slice(40)}`, { cols: 40 })

    expect(links).toHaveLength(1)
    expect(links[0].text).toBe(url)
    expect(links[0].range.start).toEqual({ x: 1, y: 1 })
    expect(links[0].range.end).toEqual({ x: url.length - 40, y: 2 })
  })

  it('detects a url hard-wrapped across three rows', async () => {
    const long = `${url}/and/then/some/more/segments/after/that`
    const parts = [long.slice(0, 40), long.slice(40, 80), long.slice(80)]
    const links = await detectLinksAt(parts.join('\r\n'), { cols: 40 })

    expect(links).toHaveLength(1)
    expect(links[0].text).toBe(long)
    expect(links[0].range.end.y).toBe(3)
  })

  it('reports the same link from any row it covers', async () => {
    const input = `${url.slice(0, 40)}\r\n${url.slice(40)}`

    const fromSecondRow = await detectLinksAt(input, { cols: 40, row: 2 })

    expect(fromSecondRow).toHaveLength(1)
    expect(fromSecondRow[0].text).toBe(url)
  })

  it('stops at the end of the url when text follows it on the last row', async () => {
    const links = await detectLinksAt(`${url.slice(0, 40)}\r\n${url.slice(40)} done`, { cols: 40 })

    expect(links).toHaveLength(1)
    expect(links[0].text).toBe(url)
  })

  it.each([
    ['inside the path', 52],
    ['inside the hostname', 12],
  ])(
    'still links a fragment cut %s far short of the pane edge — the known limit of this fix',
    async (_name, at) => {
      const links = await detectLinksAt(`${url.slice(0, at)}\r\n${url.slice(at)}`, { cols: 200 })

      // Pinned rather than wished away: a line that stops 150 columns short of
      // the edge is indistinguishable from a line that simply ended there, so
      // neither joining nor suppressing it is safe. What the fix covers is the
      // break a wrapped url actually produces — one at the pane edge.
      expect(links[0].text).toBe(url.slice(0, at))
    },
  )

  it('links nothing when the break leaves only a bare scheme', async () => {
    const links = await detectLinksAt(`https://\r\n${url.slice(8)}`, { cols: 200 })

    expect(links.map((link) => link.text)).not.toContain('https://')
  })

  it('does not link a url that runs to the pane edge with nothing to continue it', async () => {
    // A fragment that fills the row is exactly the truncated case: opening it
    // would go somewhere the operator never saw.
    const links = await detectLinksAt(`${url.slice(0, 40)}\r\n  indented`, { cols: 40 })

    expect(links).toEqual([])
  })

  it('does not join a row that does not reach the pane edge', async () => {
    const links = await detectLinksAt('https://example.com/docs\r\nnotpartofit', { cols: 40 })

    expect(links).toHaveLength(1)
    expect(links[0].text).toBe('https://example.com/docs')
    expect(links[0].range.end.y).toBe(1)
  })

  it('does not join a next row that starts with a space, and suppresses the edge match', async () => {
    const filler = 'x'.repeat(40 - 24)
    const links = await detectLinksAt(`https://example.com/docs${filler}\r\n more`, { cols: 40 })

    // The row runs to the pane edge and nothing continues it: whether the url
    // really ended there is unknowable, so it is not offered as a link.
    expect(links).toEqual([])
  })

  it('does not join across a blank row', async () => {
    const links = await detectLinksAt(`${url.slice(0, 40)}\r\n\r\n${url.slice(40)}`, { cols: 40 })

    expect(links.map((link) => link.text)).not.toContain(url)
  })

  // efficacy:exempt pins pre-existing behavior — this test was already on main;
  // inserting the bordered-url describe after it only widened the diff hunk.
  it('keeps the columns right when a wide character sits before the wrap', async () => {
    // 参照: occupies 6 cells, so the url starts at column 7 and the row holds
    // 34 of its characters before the program's own newline at the pane edge.
    const head = `参照: ${url.slice(0, 34)}`
    const links = await detectLinksAt(`${head}\r\n${url.slice(34)}`, { cols: 40 })

    expect(links).toHaveLength(1)
    expect(links[0].text).toBe(url)
    expect(links[0].range.start).toEqual({ x: 7, y: 1 })
    expect(links[0].range.end).toEqual({ x: url.length - 34, y: 2 })
  })
})

describe('url link provider: urls wrapped inside a drawn border', () => {
  const url = 'https://example.com/a/very/long/path/that/continues/across/the/drawn/frame'

  function borderedRows(
    value: string,
    contentWidth: number,
    {
      left = '│ ',
      right = ' │',
    }: { left?: string; right?: string } = {},
  ): { input: string; cols: number } {
    const rows: string[] = []
    for (let at = 0; at < value.length; at += contentWidth) {
      rows.push(`${left}${value.slice(at, at + contentWidth).padEnd(contentWidth)}${right}`)
    }
    return { input: rows.join('\r\n'), cols: left.length + contentWidth + right.length }
  }

  it('detects a url across two bordered rows', async () => {
    const framed = borderedRows(url, 40)

    const links = await detectLinksAt(framed.input, { cols: framed.cols })

    expect(links).toHaveLength(1)
    expect(links[0].text).toBe(url)
    expect(links[0].range).toEqual({
      start: { x: 3, y: 1 },
      end: { x: 2 + url.length - 40, y: 2 },
    })
  })

  it('detects a url across three or more bordered rows from its middle row', async () => {
    const long = `${url}/with/enough/additional/segments/to/reach/a/third/row`
    const framed = borderedRows(long, 32)

    const links = await detectLinksAt(framed.input, { cols: framed.cols, row: 2 })

    expect(links).toHaveLength(1)
    expect(links[0].text).toBe(long)
    expect(links[0].range.end.y).toBe(Math.ceil(long.length / 32))
  })

  it('allows asymmetric border characters and padding', async () => {
    const framed = borderedRows(url, 40, { left: '┃ ', right: '│' })

    const links = await detectLinksAt(framed.input, { cols: framed.cols })

    expect(links).toHaveLength(1)
    expect(links[0].text).toBe(url)
    expect(links[0].range.start).toEqual({ x: 3, y: 1 })
  })

  it('allows a frame with no inner padding', async () => {
    const framed = borderedRows(url, 40, { left: '│', right: '│' })

    const links = await detectLinksAt(framed.input, { cols: framed.cols })

    expect(links).toHaveLength(1)
    expect(links[0].text).toBe(url)
    expect(links[0].range.start).toEqual({ x: 2, y: 1 })
  })

  it('detects an indented frame using its actual border cell columns', async () => {
    const framed = borderedRows(url, 40)
    const indent = '   '
    const input = framed.input
      .split('\r\n')
      .map((line) => indent + line)
      .join('\r\n')

    const links = await detectLinksAt(input, { cols: framed.cols + indent.length + 5 })

    expect(links).toHaveLength(1)
    expect(links[0].text).toBe(url)
    expect(links[0].range.start).toEqual({ x: 6, y: 1 })
  })

  // efficacy:exempt guards the new border heuristic from widening to ASCII `|`;
  // main passes because it does not strip any border, which is the required negative baseline.
  it('does not treat ASCII pipes in ordinary output as a shared frame', async () => {
    const framed = borderedRows(url, 40, { left: '|', right: '|' })

    const links = await detectLinksAt(framed.input, { cols: framed.cols })

    expect(links.map((link) => link.text)).not.toContain(url)
  })

  // efficacy:exempt guards the new border heuristic's cross-row equality check;
  // main passes because it does not strip box-drawing borders at all.
  it('does not strip box-drawing characters that differ between adjacent rows', async () => {
    const first = `│${url.slice(0, 40)}│`
    const second = `┃${url.slice(40).padEnd(40)}┃`

    const links = await detectLinksAt(`${first}\r\n${second}`, { cols: 42 })

    expect(links.map((link) => link.text)).not.toContain(url)
  })

  it('keeps cell ranges aligned when wide and astral characters precede the url', async () => {
    const contentWidth = 40
    const prefix = '🎉参 '
    // With xterm's default width table the emoji is one cell and 参 is two,
    // so prefix + this fragment fills the 40-cell frame interior exactly.
    const firstUrlLength = contentWidth - 4
    const first = `│ ${prefix}${url.slice(0, firstUrlLength)} │`
    const second = `│ ${url.slice(firstUrlLength).padEnd(contentWidth)} │`

    const links = await detectLinksAt(`${first}\r\n${second}`, { cols: contentWidth + 4 })

    expect(links).toHaveLength(1)
    expect(links[0].text).toBe(url)
    expect(links[0].range).toEqual({
      start: { x: 7, y: 1 },
      end: { x: 2 + url.length - firstUrlLength, y: 2 },
    })
  })
})

// The pull-request provider is registered alongside the url one and xterm
// resolves an overlap between them by registration order — which only works if
// both report ranges in the same coordinate system. xterm's own
// IBufferCellPosition says 1-based, both axes.
describe('pull request link provider', () => {
  const REPO = 'https://github.com/example/panemux'

  async function computeLinks(line: string, row = 1) {
    const term = new Terminal({ cols: 200, rows: 10, allowProposedApi: true })
    await new Promise<void>((resolve) => term.write(line, resolve))
    const links = __computePullRequestLinksForTests(term, REPO, row)
    term.dispose()
    return links
  }

  it('reports 1-based coordinates, the same system the url provider uses', async () => {
    const links = await computeLinks('Reviewing #123 now')

    expect(links).toHaveLength(1)
    expect(links[0].text).toBe('#123')
    // 'Reviewing ' is 10 cells, so '#' is column 11 and '3' column 14.
    expect(links[0].range).toEqual({
      start: { x: 11, y: 1 },
      end: { x: 14, y: 1 },
    })
  })

  it('reports the row it was asked about', async () => {
    const links = await computeLinks('first\r\nReviewing #123 now', 2)

    expect(links).toHaveLength(1)
    expect(links[0].range.start.y).toBe(2)
    expect(links[0].range.end.y).toBe(2)
  })

  it('keeps the columns right when a wide character precedes the reference', async () => {
    const links = await computeLinks('参照 #123')

    expect(links).toHaveLength(1)
    // 参 and 照 are two cells each, then a space: '#' is column 6.
    expect(links[0].range).toEqual({
      start: { x: 6, y: 1 },
      end: { x: 9, y: 1 },
    })
  })

  it('links every reference on the line', async () => {
    const links = await computeLinks('#1 and #22')

    expect(links.map((link) => link.text)).toEqual(['#1', '#22'])
    expect(links[1].range).toEqual({
      start: { x: 8, y: 1 },
      end: { x: 10, y: 1 },
    })
  })

  it('reports nothing without repository metadata', async () => {
    const term = new Terminal({ cols: 80, rows: 10, allowProposedApi: true })
    await new Promise<void>((resolve) => term.write('Reviewing #123 now', resolve))

    expect(__computePullRequestLinksForTests(term, null, 1)).toEqual([])
    term.dispose()
  })
})
