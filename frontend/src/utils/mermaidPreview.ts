import DOMPurify from 'dompurify'
import { generate, parse, walk, type CssNode, type ListItem, type List } from 'css-tree'

interface MermaidEngine {
  initialize: (config: Record<string, unknown>) => void
  render: (id: string, source: string, container?: Element) => Promise<{ svg: string }>
}

const MAX_DIAGRAMS = 20
const MAX_TEXT = 50_000

// Reject every legacy directive and all YAML frontmatter, not just known
// dangerous keys. Mermaid's preprocessing must never receive document config.
export function safeMermaidSource(source: string): string {
  if (/%%\s*\{/.test(source) || source.trimStart().startsWith('---')) {
    throw new Error('Document Mermaid configuration is not supported.')
  }
  if (source.length > MAX_TEXT) throw new Error('Diagram exceeds the 50,000 character limit.')
  return source
}

// CSS may contain only local url(#marker) references and arithmetic/color
// functions. Reject escapes and at-rules, including CSS-escaped URL schemes.
function safeCSS(value: string): boolean {
  if (/[@\\<>]/.test(value)) return false
  const local = value.replace(/url\(\s*["']?#[\w.-]+["']?\s*\)/gi, '')
  return !/[\w-]+\s*\(/g.test(local.replace(/\b(?:rgb|rgba|hsl|hsla|calc|min|max|clamp|translate|translateX|translateY|matrix|rotate|scale|skewX|skewY)\s*\(/gi, '('))
}

// Parse the stylesheet so refusing @import or animations does not discard
// the ordinary node/edge styles that share Mermaid's generated style tag.
function sanitizeStyleSheet(source: string): string {
  const tree = parse(source)
  walk(tree, {
    enter(node: CssNode, item: ListItem<CssNode>, list: List<CssNode>) {
      if (node.type === 'Atrule' || node.type === 'Raw' ||
        (node.type === 'Declaration' && !safeCSS(generate(node.value)))) {
        if (list) { list.remove(item); return walk.skip }
      }
    },
  })
  return generate(tree)
}

export function sanitizeDiagram(source: string): SVGElement {
  const fragment = DOMPurify.sanitize(source, {
    USE_PROFILES: { svg: true, svgFilters: true },
    FORBID_TAGS: ['foreignObject', 'image', 'use', 'a', 'script', 'animate', 'set', 'animateTransform', 'animateMotion'],
    RETURN_DOM_FRAGMENT: true,
  })
  const svg = fragment.firstElementChild
  if (!(svg instanceof SVGElement) || svg.localName !== 'svg') throw new Error('Invalid diagram output.')
  for (const element of [svg, ...svg.querySelectorAll('*')]) {
    for (const attr of Array.from(element.attributes)) {
      if (attr.localName === 'href' && !/^#[\w.-]+$/.test(attr.value)) element.removeAttributeNode(attr)
      else if (!safeCSS(attr.value)) element.removeAttributeNode(attr)
    }
    if (element.localName === 'style') element.textContent = sanitizeStyleSheet(element.textContent ?? '')
  }
  svg.setAttribute('role', 'img')
  svg.setAttribute('aria-label', 'Mermaid diagram')
  return svg
}

// All DOM and layout work stays inside the preview's opaque origin. No parent
// access, messages, external requests, or document-selected code are needed.
export async function renderMermaidPreviews(root: Document, engine: MermaidEngine): Promise<void> {
  const blocks = root.querySelectorAll<HTMLElement>('pre > code.language-mermaid')
  let index = 0
  for (const code of blocks) {
    if (code.closest('.pm-mermaid')) continue
    const ordinal = index++
    const pre = code.parentElement!
    const container = root.createElement('section')
    container.className = 'pm-mermaid'
    pre.replaceWith(container)
    const details = root.createElement('details')
    const summary = root.createElement('summary')
    summary.textContent = 'Show source'
    details.append(summary, pre)
    container.append(details)
    // render() measures SVG in a live DOM container; always remove it on
    // failure too, so Mermaid's error drawing cannot leak into the page.
    const drawing = root.createElement('div')
    drawing.className = 'pm-mermaid-drawing'
    container.prepend(drawing)
    try {
      if (ordinal >= MAX_DIAGRAMS) throw new Error('Only the first 20 diagrams are rendered.')
      const source = safeMermaidSource(code.textContent ?? '')
      engine.initialize({
        startOnLoad: false,
        securityLevel: 'strict',
        htmlLabels: false,
        maxTextSize: MAX_TEXT,
        maxEdges: 500,
        suppressErrorRendering: true,
        theme: 'default',
      })
      const result = await engine.render(`pm-mermaid-${ordinal}`, source, drawing)
      drawing.replaceChildren(sanitizeDiagram(result.svg))
    } catch (error) {
      drawing.replaceChildren()
      const message = root.createElement('p')
      message.setAttribute('role', 'alert')
      message.textContent = `Mermaid: ${error instanceof Error ? error.message : 'Rendering failed.'}`
      drawing.append(message)
      details.open = true
    }
  }
}
