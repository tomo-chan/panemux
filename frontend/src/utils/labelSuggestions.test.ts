import { describe, expect, it } from 'vitest'
import {
  compareLabels,
  foldLabelSuggestions,
  lastLabelToken,
  matchLabelSuggestions,
  mergeKnownLabels,
  toggleLabelInput,
} from './labelSuggestions'

const known = ['api', 'bug', 'Docs', 'docs', 'frontend', 'refactor', 'release-1.4', 'Research']

describe('compareLabels', () => {
  it('orders case-insensitively, and labels that differ only in case by byte order', () => {
    expect(['docs', 'Research', 'api', 'Docs', 'bug'].sort(compareLabels)).toEqual(['api', 'bug', 'Docs', 'docs', 'Research'])
  })

  it('orders by code point, as the server orders UTF-8 bytes, past the Basic Multilingual Plane too', () => {
    // U+FF61 sorts before U+1F600 by code point and by UTF-8 bytes, though
    // not by UTF-16 code units.
    expect(['\u{1F600}', '\u{FF61}', 'a'].sort(compareLabels)).toEqual(['a', '\u{FF61}', '\u{1F600}'])
  })
})

describe('mergeKnownLabels', () => {
  it.each([
    ['adds a new label in order', ['api', 'docs'], ['Bug'], ['api', 'Bug', 'docs']],
    ['keeps a known label once', ['api', 'docs'], ['docs', 'api'], ['api', 'docs']],
    ['keeps labels that differ only in case apart', ['docs'], ['Docs'], ['Docs', 'docs']],
    ['adds to none', [], ['b', 'a'], ['a', 'b']],
    ['leaves the list as it is for no labels', ['a'], [], ['a']],
  ])('%s', (_name, current, added, want) => {
    expect(mergeKnownLabels(current, added)).toEqual(want)
  })
})

describe('lastLabelToken', () => {
  it.each([
    ['', ''],
    ['  re ', 're'],
    ['bug, frontend, re', 're'],
    ['bug, frontend, ', ''],
    ['bug,', ''],
  ])('%j → %j', (text, want) => {
    expect(lastLabelToken(text)).toBe(want)
  })
})

describe('toggleLabelInput', () => {
  it.each([
    ['adds to an empty input', '', 'bug', 'bug, '],
    ['adds after the entered labels', 'bug, ', 'frontend', 'bug, frontend, '],
    ['replaces the text being typed after the last comma', 'bug, re', 'refactor', 'bug, refactor, '],
    ['replaces a lone partial label', 'fro', 'frontend', 'frontend, '],
    ['removes an entered label', 'bug, frontend, ', 'bug', 'frontend, '],
    ['removes the label typed in full after the last comma', 'bug, frontend', 'frontend', 'bug, '],
    ['removes the last label, leaving the input empty', 'bug, ', 'bug', ''],
    ['tidies spacing and empty entries', ' bug ,, api,', 'docs', 'bug, api, docs, '],
    ['matches the label exactly, case included', 'Docs, ', 'docs', 'Docs, docs, '],
    ['keeps a known label typed after the last comma when removing another', 'bug, frontend', 'bug', 'frontend, '],
    ['keeps a known label typed after the last comma when adding another', 'bug, frontend', 'docs', 'bug, frontend, docs, '],
    ['keeps a lone known label when adding another', 'bug', 'docs', 'bug, docs, '],
    ['replaces a typed label that differs from a known one only in case', 'bug, Frontend', 'frontend', 'bug, frontend, '],
    ['replaces only the text after the last comma when it repeats an earlier label', 're, bug, re', 'refactor', 're, bug, refactor, '],
  ])('%s', (_name, text, label, want) => {
    expect(toggleLabelInput(text, label, known)).toBe(want)
  })
})

describe('matchLabelSuggestions', () => {
  it('offers every label for an empty query', () => {
    expect(matchLabelSuggestions(known, '')).toEqual(known)
  })

  it('matches a part of the label, ignoring case', () => {
    expect(matchLabelSuggestions(known, 'RE')).toEqual(['refactor', 'release-1.4', 'Research'])
    expect(matchLabelSuggestions(known, 'oc')).toEqual(['Docs', 'docs'])
  })

  it('ignores the spaces around the query', () => {
    expect(matchLabelSuggestions(known, '  bug ')).toEqual(['bug'])
  })

  it('keeps the labels it is told to keep, whatever the query', () => {
    expect(matchLabelSuggestions(known, 're', { keep: ['bug'] })).toEqual(['bug', 'refactor', 'release-1.4', 'Research'])
  })

  it('leaves out the labels it is told to leave out', () => {
    expect(matchLabelSuggestions(known, '', { exclude: ['bug', 'docs'] })).toEqual(['api', 'Docs', 'frontend', 'refactor', 'release-1.4', 'Research'])
  })

  it('matches nothing when no label contains the query', () => {
    expect(matchLabelSuggestions(known, 'zzz')).toEqual([])
  })
})

describe('foldLabelSuggestions', () => {
  const labels = ['a', 'b', 'c', 'd', 'e']

  it.each([
    ['shows all at or under the fold', labels, 5, [], false, labels, 0],
    ['shows the first labels over the fold', labels, 3, [], false, ['a', 'b', 'c'], 2],
    ['shows all when expanded, still counting what folding hides', labels, 3, [], true, labels, 2],
    ['keeps a kept label past the fold', labels, 2, ['e'], false, ['a', 'b', 'e'], 2],
    ['hides nothing when every label past the fold is kept', labels, 3, ['d', 'e'], false, labels, 0],
    ['shows nothing for no labels', [], 3, [], false, [], 0],
  ])('%s', (_name, input, foldAt, keep, expanded, shown, folded) => {
    expect(foldLabelSuggestions(input, foldAt, { keep, expanded })).toEqual({ shown, folded })
  })
})
