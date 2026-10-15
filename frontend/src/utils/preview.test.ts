import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  PREVIEW_PATH_REGEX,
  PREVIEW_SPLIT_DEFAULT,
  PREVIEW_SPLIT_MAX,
  PREVIEW_SPLIT_MIN,
  clampPreviewSplit,
  loadPreviewSplit,
  paneSplitKey,
  previewHostLabel,
  requestPreview,
  savePreviewSplit,
  taskSplitKey,
} from './preview'

afterEach(() => {
  vi.restoreAllMocks()
  window.localStorage.clear()
})

function paths(text: string): string[] {
  const pattern = new RegExp(PREVIEW_PATH_REGEX.source, PREVIEW_PATH_REGEX.flags + 'g')
  return Array.from(text.matchAll(pattern), (m) => m[0])
}

describe('PREVIEW_PATH_REGEX', () => {
  it.each([
    ['./mockups/index.html', ['./mockups/index.html']],
    ['  docs/design.md', ['docs/design.md']],
    ['/workspace/user/project/README.md:12', ['/workspace/user/project/README.md']],
    ['See design.md.', ['design.md']],
    ['(docs/a.md)', ['docs/a.md']],
    ['「docs/設計.md」を確認', ['docs/設計.md']],
    ['notes.markdown, page.HTM and ../up/x.htm', ['notes.markdown', 'page.HTM', '../up/x.htm']],
    ['"quoted/file.html"', ['quoted/file.html']],
    ['`code/file.md`', ['code/file.md']],
  ])('%s', (text, want) => {
    expect(paths(text)).toEqual(want)
  })

  it.each([
    'https://example.com/docs/design.md',
    'http://localhost:5173/index.html',
    'design.md.bak',
    'index.htmlx',
    'archive.md5',
    '.md',
    'docs/.md',
    '~/notes/todo.md',
    'C:\\docs\\a.md',
    'design.mdx',
  ])('ignores %s', (text) => {
    expect(paths(text)).toEqual([])
  })
})

describe('clampPreviewSplit', () => {
  it.each([
    [50, 50],
    [PREVIEW_SPLIT_MIN, PREVIEW_SPLIT_MIN],
    [PREVIEW_SPLIT_MIN - 1, PREVIEW_SPLIT_MIN],
    [PREVIEW_SPLIT_MAX, PREVIEW_SPLIT_MAX],
    [PREVIEW_SPLIT_MAX + 1, PREVIEW_SPLIT_MAX],
    [Number.NaN, PREVIEW_SPLIT_DEFAULT],
    [Number.POSITIVE_INFINITY, PREVIEW_SPLIT_DEFAULT],
  ])('%s -> %s', (value, want) => {
    expect(clampPreviewSplit(value)).toBe(want)
  })

  it('bounds the split at 20:80 and 80:20, starting at 50:50', () => {
    expect([PREVIEW_SPLIT_MIN, PREVIEW_SPLIT_DEFAULT, PREVIEW_SPLIT_MAX]).toEqual([20, 50, 80])
  })
})

describe('preview split storage', () => {
  it('keeps one ratio per pane and per task', () => {
    savePreviewSplit(paneSplitKey('pane-1'), 30)
    savePreviewSplit(taskSplitKey('local:claude:7c21e0a4'), 70)

    expect(loadPreviewSplit(paneSplitKey('pane-1'))).toBe(30)
    expect(loadPreviewSplit(taskSplitKey('local:claude:7c21e0a4'))).toBe(70)
    expect(loadPreviewSplit(paneSplitKey('pane-2'))).toBe(PREVIEW_SPLIT_DEFAULT)
    expect(paneSplitKey('x')).not.toBe(taskSplitKey('x'))
  })

  it('clamps a stored value it did not write', () => {
    window.localStorage.setItem(paneSplitKey('pane-1'), '95')
    expect(loadPreviewSplit(paneSplitKey('pane-1'))).toBe(PREVIEW_SPLIT_MAX)
    window.localStorage.setItem(paneSplitKey('pane-1'), 'wide')
    expect(loadPreviewSplit(paneSplitKey('pane-1'))).toBe(PREVIEW_SPLIT_DEFAULT)
  })

  it('works without storage', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied')
    })
    expect(() => savePreviewSplit(paneSplitKey('pane-1'), 30)).not.toThrow()
    expect(loadPreviewSplit(paneSplitKey('pane-1'))).toBe(PREVIEW_SPLIT_DEFAULT)
  })
})

describe('previewHostLabel', () => {
  it('names the panemux host local', () => {
    expect(previewHostLabel('')).toBe('local')
    expect(previewHostLabel('build-box')).toBe('build-box')
  })
})

describe('requestPreview', () => {
  it('posts the clicked path and parses the answer', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ status: 'ready', url: '/preview/ab/index.html', path: '/w/index.html', host: '' })),
    )

    const resp = await requestPreview('pane/1', './index.html')

    expect(resp).toEqual({ status: 'ready', url: '/preview/ab/index.html', path: '/w/index.html', host: '' })
    expect(fetchMock).toHaveBeenCalledWith('/api/sessions/pane%2F1/preview', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: './index.html' }),
    })
  })

  it("rejects with the server's reason", async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('file not found: x.md\n', { status: 404 }))
    await expect(requestPreview('pane-1', 'x.md')).rejects.toThrow('file not found: x.md')
  })

  it('rejects with the status when the server gives no reason', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('', { status: 500 }))
    await expect(requestPreview('pane-1', 'x.md')).rejects.toThrow('preview failed (500)')
  })

  it('rejects an answer that does not match the schema', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ status: 'ready', url: 'https://example.com/', path: '/x', host: '' })),
    )
    await expect(requestPreview('pane-1', 'x.md')).rejects.toThrow()
  })
})
