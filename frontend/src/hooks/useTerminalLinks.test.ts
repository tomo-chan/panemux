import { describe, it, expect } from 'vitest'
import { Terminal } from '@xterm/xterm'
import type { ILink, ILinkProvider } from '@xterm/xterm'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { TERMINAL_URL_REGEX } from './useTerminal'

// Integration check against the real xterm.js terminal and the real web links addon.
// useTerminal.test.ts mocks both of those, so it can only assert which options panemux
// passes; this file verifies the addon actually honours TERMINAL_URL_REGEX and yields
// the link ranges we expect for CJK-adjacent urls.
async function detectLinks(line: string): Promise<ILink[]> {
  const term = new Terminal({ cols: 200, rows: 10, allowProposedApi: true })

  let provider: ILinkProvider | undefined
  const registerLinkProvider = term.registerLinkProvider.bind(term)
  term.registerLinkProvider = (candidate: ILinkProvider) => {
    provider = candidate
    return registerLinkProvider(candidate)
  }

  term.loadAddon(new WebLinksAddon(undefined, { urlRegex: TERMINAL_URL_REGEX }))
  await new Promise<void>((resolve) => term.write(line, resolve))

  const links = await new Promise<ILink[] | undefined>((resolve) => {
    provider?.provideLinks(1, resolve)
  })
  term.dispose()
  return links ?? []
}

describe('web links addon integration', () => {
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
})
