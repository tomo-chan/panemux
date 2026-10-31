import { describe, expect, it } from 'vitest'
import { parseSessionLink, sessionLinkUrl, stripSessionLink } from './sessionLink'

describe('session links', () => {
  it('round trips opaque IDs and hosts without carrying credentials or other parameters', () => {
    const key = { host: 'gpu & +日本', agent: 'new-agent', session_id: 'a+b &=#%/日本' }
    const url = new URL(sessionLinkUrl('https://user:secret@example.com/app?token=secret#private', key)!)
    expect(url.origin + url.pathname).toBe('https://example.com/app')
    expect(url.username + url.password + url.hash).toBe('')
    expect(url.searchParams.has('token')).toBe(false)
    expect(parseSessionLink(url.search)).toEqual({ kind: 'valid', key })
  })

  it('keeps an explicitly empty local host and accepts non-UUID session IDs', () => {
    expect(parseSessionLink('?session_host=&session_agent=claude&session_id=abc')).toEqual({
      kind: 'valid', key: { host: '', agent: 'claude', session_id: 'abc' },
    })
    expect(sessionLinkUrl('https://example.com', { host: '', agent: 'codex' })).toBeNull()
  })

  it.each([
    '?session_id=x', '?session_host=&session_agent=claude&session_id=',
    '?session_host=&session_agent=Claude&session_id=x',
    '?session_host=&session_agent=claude&session_id=x&session_id=x',
    '?session_host=&session_agent=claude&session_id=%',
    '?session_host=&session_agent=claude&session_id=%C0%AF',
    '?session_host=&session_agent=claude&session_id=%FF',
  ])('rejects invalid keys: %s', (query) => {
    expect(parseSessionLink(query)).toEqual({ kind: 'invalid' })
  })

  it('ignores unrelated queries and strips only the three target keys', () => {
    expect(parseSessionLink('?other=%')).toEqual({ kind: 'none' })
    expect(stripSessionLink('https://example.com/?other=1&session_id=x#anchor')).toBe('https://example.com/?other=1#anchor')
  })
})
