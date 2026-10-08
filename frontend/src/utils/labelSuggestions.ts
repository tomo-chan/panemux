// Label suggestions (issue #310): the labels the task record file holds,
// offered under the New task form's Labels field and the detail panel's
// Add a label field as "Used before:" tags.

import { parseLabelInput } from './taskBoard'

/**
 * Case-insensitive alphabetical order. Labels are case-sensitive, so two that
 * differ only in case are both kept; between those the code point order
 * decides. Comparing by code point, not by UTF-16 code unit, matches the
 * server's byte order on UTF-8 (known_labels) beyond the Basic Multilingual
 * Plane too.
 */
export function compareLabels(a: string, b: string): number {
  return compareCodePoints(a.toLowerCase(), b.toLowerCase()) || compareCodePoints(a, b)
}

function compareCodePoints(a: string, b: string): number {
  const ca = Array.from(a)
  const cb = Array.from(b)
  for (let i = 0; i < Math.min(ca.length, cb.length); i++) {
    const d = (ca[i].codePointAt(0) ?? 0) - (cb[i].codePointAt(0) ?? 0)
    if (d !== 0) return d < 0 ? -1 : 1
  }
  return ca.length === cb.length ? 0 : ca.length < cb.length ? -1 : 1
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
 * or added if it is not; the input then ends with ", " so the next label can
 * be typed. Adding replaces what is being typed after the last comma, unless
 * that is a known label in full: that one is entered (shown ✓) and is kept.
 */
export function toggleLabelInput(text: string, label: string, known: string[]): string {
  const entered = parseLabelInput(text)
  let next: string[]
  if (entered.includes(label)) {
    next = entered.filter((l) => l !== label)
  } else {
    const typed = lastLabelToken(text)
    const before = typed === '' || known.includes(typed) ? text : text.split(',').slice(0, -1).join(',')
    next = [...parseLabelInput(before), label]
  }
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
