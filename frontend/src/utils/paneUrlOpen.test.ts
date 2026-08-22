import { describe, it, expect, vi, afterEach } from 'vitest'
import {
  BROWSER_OPEN_OSC_IDENT,
  isLoopbackUrl,
  isOpenableUrl,
  parseBrowserOpenOsc,
  requestPortForward,
} from './paneUrlOpen'

afterEach(() => {
  vi.restoreAllMocks()
})

describe('parseBrowserOpenOsc', () => {
  it('uses the OSC identifier the shim writes', () => {
    expect(BROWSER_OPEN_OSC_IDENT).toBe(7373)
  })

  it('extracts http and https URLs', () => {
    expect(parseBrowserOpenOsc('panemux-open;https://example.com/auth?a=1')).toBe('https://example.com/auth?a=1')
    expect(parseBrowserOpenOsc('panemux-open;http://localhost:8085/cb')).toBe('http://localhost:8085/cb')
  })

  it('rejects payloads that are not browser-open requests', () => {
    expect(parseBrowserOpenOsc('something-else;https://example.com/')).toBeNull()
    expect(parseBrowserOpenOsc('')).toBeNull()
    expect(parseBrowserOpenOsc('panemux-open;')).toBeNull()
  })

  it('rejects non-http schemes so a pane cannot drive file or script URLs', () => {
    expect(parseBrowserOpenOsc('panemux-open;file:///etc/passwd')).toBeNull()
    expect(parseBrowserOpenOsc('panemux-open;javascript:alert(1)')).toBeNull()
    expect(parseBrowserOpenOsc('panemux-open;not a url')).toBeNull()
  })
})

describe('isOpenableUrl', () => {
  it.each([
    ['https://example.com/', true],
    ['http://127.0.0.1:9000/', true],
    ['file:///etc/passwd', false],
    ['javascript:alert(1)', false],
    ['', false],
    ['//example.com', false],
  ])('%s -> %s', (url, want) => {
    expect(isOpenableUrl(url)).toBe(want)
  })
})

describe('isLoopbackUrl', () => {
  it.each([
    ['http://localhost:8085/cb', true],
    ['http://LOCALHOST:8085/cb', true],
    ['http://127.0.0.1:8085/cb', true],
    ['http://127.0.0.53:8085/cb', true],
    ['http://[::1]:8085/cb', true],
    ['https://example.com/auth?redirect_uri=http://localhost:8085/cb', false],
    ['not a url', false],
  ])('%s -> %s', (url, want) => {
    expect(isLoopbackUrl(url)).toBe(want)
  })
})

describe('requestPortForward', () => {
  it('posts the URL to the pane endpoint and returns the parsed response', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ url: 'https://example.com/', forwarded: true, port: 8085 }),
    } as Response)
    window.fetch = fetchMock

    const result = await requestPortForward('pane 1', 'https://example.com/')

    expect(result).toEqual({ url: 'https://example.com/', forwarded: true, port: 8085 })
    expect(fetchMock).toHaveBeenCalledWith('/api/sessions/pane%201/open-url', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url: 'https://example.com/' }),
    })
  })

  it('accepts a response that only reports why nothing was forwarded', async () => {
    window.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ url: 'https://example.com/', forwarded: false, reason: 'nothing to forward' }),
    } as Response)

    await expect(requestPortForward('pane1', 'https://example.com/')).resolves.toEqual({
      url: 'https://example.com/',
      forwarded: false,
      reason: 'nothing to forward',
    })
  })

  it('raises the server error text on failure', async () => {
    window.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      text: () => Promise.resolve('loopback port unavailable: port 8085 is already forwarded for pane other\n'),
    } as Response)

    await expect(requestPortForward('pane1', 'https://example.com/')).rejects.toThrow(
      'loopback port unavailable: port 8085 is already forwarded for pane other',
    )
  })

  it('falls back to the status code when the server sends no message', async () => {
    window.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      text: () => Promise.resolve('   '),
    } as Response)

    await expect(requestPortForward('pane1', 'https://example.com/')).rejects.toThrow('500')
  })

  it('rejects a response that does not match the schema', async () => {
    window.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ forwarded: 'yes' }),
    } as Response)

    await expect(requestPortForward('pane1', 'https://example.com/')).rejects.toThrow()
  })
})
