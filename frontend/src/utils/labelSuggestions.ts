// Label suggestions (issue #310): the labels the task record file holds,
// offered under the New task form's Labels field and the detail panel's
// Add a label field as "Used before:" tags.

import { parseLabelInput } from './taskBoard'

/**
 * Case-insensitive alphabetical order. Labels are case-sensitive, so two that
 * differ only in case are both kept; between those the byte order decides,
 * as it does on the server (known_labels).
 */
export function compareLabels(a: string, b: string): number {
  const la = a.toLowerCase()
  const lb = b.toLowerCase()
  if (la !== lb) return la < lb ? -1 : 1
  if (a === b) return 0
  return a < b ? -1 : 1
}

/** The known labels with the given ones added, once each, in order. */
export function mergeKnownLabels(known: string[], added: string[]): string[] {
  return [...new Set([...known, ...added])].sort(compareLabels)
}

/** What is being typed in a comma-separated label input: the text after its last comma. */
export function lastLabelToken(text: string): string {
  const parts = text.split(',')
  return parts[parts.length - 1].trim()
}

/**
 * The comma-separated label input with the label taken out if it is entered,
 * or added if it is not. Adding replaces what is being typed after the last
 * comma, and the input ends with ", " so the next label can be typed.
 */
export function toggleLabelInput(text: string, label: string): string {
  const entered = parseLabelInput(text)
  const kept = parseLabelInput(text.split(',').slice(0, -1).join(','))
  const next = entered.includes(label) ? kept.filter((l) => l !== label) : [...kept, label]
  return next.length > 0 ? `${next.join(', ')}, ` : ''
}

/**
 * The known labels that contain the query, ignoring case; every one for an
 * empty query. Labels in keep are matched whatever the query, and labels in
 * exclude never are. The order is the known labels' own.
 */
export function matchLabelSuggestions(
  known: string[],
  query: string,
  { keep = [], exclude = [] }: { keep?: string[]; exclude?: string[] } = {},
): string[] {
  const q = query.trim().toLowerCase()
  return known.filter(
    (label) => !exclude.includes(label) && (keep.includes(label) || q === '' || label.toLowerCase().includes(q)),
  )
}

/**
 * The suggestions folded to the first foldAt, plus any label in keep past
 * them. folded is how many folding hides, whether or not it is expanded: the
 * fold button is offered only while it is above zero.
 */
export function foldLabelSuggestions(
  labels: string[],
  foldAt: number,
  { keep = [], expanded = false }: { keep?: string[]; expanded?: boolean } = {},
): { shown: string[]; folded: number } {
  const collapsed = labels.filter((label, i) => i < foldAt || keep.includes(label))
  return { shown: expanded ? labels : collapsed, folded: labels.length - collapsed.length }
}
