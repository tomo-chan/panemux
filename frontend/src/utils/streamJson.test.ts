import { describe, it, expect } from 'vitest'
import { summarizeStreamLines } from './streamJson'

// Fixtures mirror the shapes a real `claude -p --output-format=stream-json
// --verbose` run emits, captured from an actual command center query.
const assistantText = (text: string) => ({
  type: 'assistant',
  message: { role: 'assistant', content: [{ type: 'text', text }] },
})

const assistantToolUse = (name: string) => ({
  type: 'assistant',
  message: { role: 'assistant', content: [{ type: 'tool_use', name, input: {} }] },
})

const streamEvent = { type: 'stream_event', event: { type: 'content_block_delta' } }
const systemInit = { type: 'system', subtype: 'init', tools: ['Task', 'Bash'] }
const userToolResult = { type: 'user', message: { role: 'user', content: [{ type: 'tool_result' }] } }
const result = (text: string) => ({ type: 'result', subtype: 'success', result: text })

describe('summarizeStreamLines', () => {
  it('renders a panemux-recorded prompt as its own kind', () => {
    expect(summarizeStreamLines([{ type: 'panemux_prompt', text: 'which panes are blocked?' }])).toEqual([
      { kind: 'prompt', text: 'which panes are blocked?' },
    ])
  })

  it('keeps the prompt ahead of the answer it produced', () => {
    expect(
      summarizeStreamLines([
        { type: 'panemux_prompt', text: 'status?' },
        streamEvent,
        assistantText('Two panes are working.'),
      ]),
    ).toEqual([
      { kind: 'prompt', text: 'status?' },
      { kind: 'text', text: 'Two panes are working.' },
    ])
  })

  // A prompt is not assistant text, so it must not suppress the result
  // fallback for a turn that produced no assistant text of its own.
  it('still falls back to result when a turn only has a prompt', () => {
    expect(summarizeStreamLines([{ type: 'panemux_prompt', text: 'status?' }, result('Nobody home.')])).toEqual([
      { kind: 'prompt', text: 'status?' },
      { kind: 'text', text: 'Nobody home.' },
    ])
  })

  it('ignores a prompt frame with no usable text', () => {
    expect(summarizeStreamLines([{ type: 'panemux_prompt', text: '  ' }, { type: 'panemux_prompt' }])).toEqual([])
  })

  it('returns nothing for an empty input', () => {
    expect(summarizeStreamLines([])).toEqual([])
  })

  it('drops stream_event, system and user frames entirely', () => {
    expect(summarizeStreamLines([streamEvent, systemInit, userToolResult])).toEqual([])
  })

  it('extracts assistant text rather than labelling the frame by type', () => {
    expect(summarizeStreamLines([assistantText('Delivered to both panes.')])).toEqual([
      { kind: 'text', text: 'Delivered to both panes.' },
    ])
  })

  it('renders a tool call as a named marker', () => {
    expect(summarizeStreamLines([assistantToolUse('board_broadcast')])).toEqual([
      { kind: 'tool', text: 'board_broadcast' },
    ])
  })

  it('keeps assistant text and tool calls in arrival order, dropping the noise between them', () => {
    expect(
      summarizeStreamLines([
        systemInit,
        streamEvent,
        assistantText('Checking who is on the board.'),
        streamEvent,
        assistantToolUse('board_status'),
        userToolResult,
        assistantToolUse('board_broadcast'),
        streamEvent,
        assistantText('Delivered to both panes.'),
      ]),
    ).toEqual([
      { kind: 'text', text: 'Checking who is on the board.' },
      { kind: 'tool', text: 'board_status' },
      { kind: 'tool', text: 'board_broadcast' },
      { kind: 'text', text: 'Delivered to both panes.' },
    ])
  })

  it('handles several content blocks in one assistant frame', () => {
    expect(
      summarizeStreamLines([
        {
          type: 'assistant',
          message: {
            role: 'assistant',
            content: [
              { type: 'text', text: 'Sending now.' },
              { type: 'tool_use', name: 'board_broadcast', input: {} },
            ],
          },
        },
      ]),
    ).toEqual([
      { kind: 'text', text: 'Sending now.' },
      { kind: 'tool', text: 'board_broadcast' },
    ])
  })

  // The result frame repeats the final assistant message verbatim, so
  // rendering both would show the answer twice.
  it('drops the result frame when the same turn already produced assistant text', () => {
    expect(
      summarizeStreamLines([assistantText('Delivered to both panes.'), result('Delivered to both panes.')]),
    ).toEqual([{ kind: 'text', text: 'Delivered to both panes.' }])
  })

  it('falls back to the result frame when no assistant text was emitted', () => {
    expect(summarizeStreamLines([streamEvent, result('Done.')])).toEqual([{ kind: 'text', text: 'Done.' }])
  })

  it('still falls back to result when the turn only made tool calls', () => {
    expect(summarizeStreamLines([assistantToolUse('board_status'), result('Nobody is on the board.')])).toEqual([
      { kind: 'tool', text: 'board_status' },
      { kind: 'text', text: 'Nobody is on the board.' },
    ])
  })

  it('ignores blank and whitespace-only assistant text', () => {
    expect(summarizeStreamLines([assistantText(''), assistantText('   '), assistantText('Real.')])).toEqual([
      { kind: 'text', text: 'Real.' },
    ])
  })

  it('does not throw on malformed or unexpected shapes', () => {
    expect(
      summarizeStreamLines([
        null,
        undefined,
        'a bare string',
        42,
        {},
        { type: 'assistant' },
        { type: 'assistant', message: null },
        { type: 'assistant', message: { content: 'not an array' } },
        { type: 'assistant', message: { content: [{ type: 'text' }] } },
        { type: 'assistant', message: { content: [{ type: 'tool_use' }] } },
        { type: 'result', result: 123 },
      ]),
    ).toEqual([])
  })

  it('trims surrounding whitespace from extracted text', () => {
    expect(summarizeStreamLines([assistantText('  padded  ')])).toEqual([{ kind: 'text', text: 'padded' }])
  })
})
