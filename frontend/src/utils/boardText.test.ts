import { expect, it } from 'vitest'
import { boardText } from './boardText'

it('keeps readable multiline text while exposing terminal and bidirectional controls', () => {
  expect(boardText('line 1\r\nline 2\tend\r\u0000\u001b\u007f\u0085\u061c\u200e\u200f\u202a\u202e\u2066\u2069'))
    .toBe('line 1\nline 2 end\\u000d\\u0000\\u001b\\u007f\\u0085\\u061c\\u200e\\u200f\\u202a\\u202e\\u2066\\u2069')
})

it('preserves names and markup as text without trimming or interpreting them', () => {
  expect(boardText('  日本語 <script> & 🐈\n')).toBe('  日本語 <script> & 🐈\n')
})
