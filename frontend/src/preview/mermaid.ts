import mermaid from 'mermaid'
import { renderMermaidPreviews } from '../utils/mermaidPreview'

// Mermaid measures live SVG text. Wait for fonts and a layout frame before
// measuring a document that has just appeared in the split preview.
void (async () => {
  await document.fonts.ready
  await new Promise<void>(resolve => requestAnimationFrame(() => resolve()))
  await renderMermaidPreviews(document, mermaid)
})()
