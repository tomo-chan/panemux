import { describe, expect, it } from 'vitest'
import { containsAgentConfirmationPrompt } from './agentConfirmation'

describe('containsAgentConfirmationPrompt', () => {
  it.each([
    ['Do you want to run this command?'],
    ['Would you like to apply this change?'],
    ['Proceed?'],
    ['Continue?'],
    ['Allow?'],
    ['Approve this action?'],
    ['Run command? (y/n)'],
    ['この操作を承認してください'],
    ['確認待ちです'],
  ])('detects confirmation prompt: %s', (output) => {
    expect(containsAgentConfirmationPrompt(output)).toBe(true)
  })

  it('ignores ordinary terminal output', () => {
    expect(containsAgentConfirmationPrompt('npm test\nPASS src/App.test.tsx\n')).toBe(false)
  })

  it('ignores stale prompts outside the recent terminal tail', () => {
    const output = [
      'Do you want to continue?',
      'line 1',
      'line 2',
      'line 3',
      'line 4',
      'line 5',
      'line 6',
      'line 7',
    ].join('\n')

    expect(containsAgentConfirmationPrompt(output)).toBe(false)
  })

  it('strips ANSI escape sequences before matching', () => {
    expect(containsAgentConfirmationPrompt('\x1b[33mDo you want to proceed?\x1b[0m')).toBe(true)
  })
})
