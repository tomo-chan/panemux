import { describe, it, expect, vi } from 'vitest'
import { Terminal } from '@xterm/xterm'
import type { ILink } from '@xterm/xterm'
import { TERMINAL_URL_REGEX } from './useTerminal'
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
