import { beforeEach, describe, expect, it, vi } from 'vitest'
import { renderMermaidPreviews, safeMermaidSource, sanitizeDiagram } from './mermaidPreview'

const render = vi.fn()
const initialize = vi.fn()
const engine = { render, initialize }

beforeEach(() => {
  document.body.innerHTML = ''
  render.mockReset().mockResolvedValue({ svg: '<svg xmlns="http://www.w3.org/2000/svg"><text>図</text></svg>' })
  initialize.mockReset()
})

function code(source: string, language = 'mermaid') {
  const pre = document.createElement('pre')
  const node = document.createElement('code')
  node.className = `language-${language}`
  node.textContent = source
  pre.append(node)
  document.body.append(pre)
  return node
}

describe('Mermaid preview', () => {
  it('renders only Mermaid, keeps source, and uses unique IDs', async () => {
    code('flowchart LR\nA-->B')
    code('sequenceDiagram\nA->>B: 図')
    code('const x = 1', 'js')
    await renderMermaidPreviews(document, engine)
    expect(document.querySelectorAll('.pm-mermaid svg')).toHaveLength(2)
    expect(document.querySelectorAll('details code')).toHaveLength(2)
    expect(document.querySelector('code.language-js')).toHaveTextContent('const x = 1')
    expect(render.mock.calls[0][0]).not.toEqual(render.mock.calls[1][0])
    expect(initialize).toHaveBeenCalledWith(expect.objectContaining({ securityLevel: 'strict', htmlLabels: false, startOnLoad: false }))
    expect(document.querySelector('svg')).toHaveAttribute('role', 'img')
  })

  it('contains a failing diagram and continues with the next one', async () => {
    code('invalid')
    code('flowchart LR\nA-->B')
    render.mockRejectedValueOnce(new Error('<img src=x onerror=alert(1)>'))
    await renderMermaidPreviews(document, engine)
    expect(document.querySelector('[role=alert]')).toHaveTextContent('<img')
    expect(document.querySelectorAll('img')).toHaveLength(0)
    expect(document.querySelectorAll('.pm-mermaid svg')).toHaveLength(1)
    expect(document.querySelector('details')).toHaveAttribute('open')
  })

  it.each([
    '%%{init: {"securityLevel":"loose"}}%%\nflowchart LR\nA-->B',
    '%%{ initialize: {} }%%\nflowchart LR\nA-->B',
    '---\nconfig:\n  themeCSS: evil\n---\nflowchart LR\nA-->B',
    '---\ntitle: Diagram\n---\nflowchart LR\nA-->B',
  ])('rejects document configuration before calling Mermaid: %s', async source => {
    code(source)
    await renderMermaidPreviews(document, engine)
    expect(render).not.toHaveBeenCalled()
    expect(document.querySelector('[role=alert]')).toHaveTextContent('configuration')
  })

  it('bounds source and diagram count before rendering', async () => {
    code('a'.repeat(50_001))
    for (let i = 0; i < 21; i++) code('flowchart LR\nA-->B')
    await renderMermaidPreviews(document, engine)
    expect(render).toHaveBeenCalledTimes(19)
    expect(document.querySelectorAll('[role=alert]')).toHaveLength(3)
  })

  it('is safe to run twice on the same document', async () => {
    code('flowchart LR\nA-->B')
    await renderMermaidPreviews(document, engine)
    await renderMermaidPreviews(document, engine)
    expect(render).toHaveBeenCalledTimes(1)
  })
})

it('rejects legacy directives even across whitespace and accepts regular diagram text', () => {
  expect(() => safeMermaidSource('\ufeff  ---\nconfig: {}\n---')).toThrow()
  expect(() => safeMermaidSource('%%\n{init: {}}%%\nflowchart LR')).toThrow()
  expect(safeMermaidSource('flowchart LR\nA[設定]-->B')).toContain('設定')
})

it('removes executable SVG, external references and CSS while keeping local markers', () => {
  const svg = sanitizeDiagram(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"><script>alert(1)</script><foreignObject><p>bad</p></foreignObject><a href="javascript:alert(1)"><text>link</text></a><image href="https://example.invalid/a"/><use href="https://example.invalid/b"/><style>@import 'https://example.invalid/c';</style><path marker-end="url(#arrow)"/><text style="fill:url(https://example.invalid/d)" onclick="alert(1)">safe</text></svg>`)
  expect(svg.querySelector('script,foreignObject,image,use,a')).toBeNull()
  expect(svg.querySelector('[onload],[onclick],[style]')).toBeNull()
  expect(svg.querySelector('path')?.getAttribute('marker-end')).toBe('url(#arrow)')
  expect(svg.textContent).toContain('safe')
})


it('keeps ordinary SVG styles while removing at-rules and external CSS', () => {
  const svg = sanitizeDiagram('<svg xmlns="http://www.w3.org/2000/svg"><style>@import "https://example.invalid/a";@keyframes dash{to{stroke-dashoffset:0}} .node rect{fill:#ececff;stroke:#333;background-image:image-set("https://example.invalid/b" 1x)} .edge{marker-end:url(#arrow)}</style><g transform="translate(10,20)"><rect/></g></svg>')
  expect(svg.querySelector('style')?.textContent).toContain('fill:#ececff')
  expect(svg.querySelector('style')?.textContent).toContain('url(#arrow)')
  expect(svg.querySelector('style')?.textContent).not.toMatch(/@|example.invalid/)
  expect(svg.querySelector('g')?.getAttribute('transform')).toBe('translate(10,20)')
})
