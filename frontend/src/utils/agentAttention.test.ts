import { describe, expect, it } from 'vitest'
import { createAgentAttentionDetector } from './agentAttention'

describe('createAgentAttentionDetector', () => {
  it('detects an agent confirmation prompt in terminal output', () => {
    const detector = createAgentAttentionDetector()

    expect(detector.feed('Codex needs confirmation before continuing. Approve?')).toBe(true)
  })

  it('detects prompts split across terminal frames after stripping ANSI escapes', () => {
    const detector = createAgentAttentionDetector()

    expect(detector.feed('\x1b[33mAgent is waiting for conf')).toBe(false)
    expect(detector.feed('irmation\x1b[0m: proceed?')).toBe(true)
  })

  it('does not match ordinary terminal output', () => {
    const detector = createAgentAttentionDetector()

    expect(detector.feed('npm test finished successfully')).toBe(false)
  })

  it('does not match generic non-agent confirmation prompts', () => {
    const detector = createAgentAttentionDetector()

    expect(detector.feed('Need to install the following packages: vite. Proceed? [y/N]')).toBe(false)
    expect(detector.feed('Allow Homebrew to make changes?')).toBe(false)
  })

  it('does not keep matching stale prompt text after it has emitted', () => {
    const detector = createAgentAttentionDetector()

    expect(detector.feed('Codex needs confirmation before continuing. Approve?')).toBe(true)
    expect(detector.feed('\nnormal command output after the prompt')).toBe(false)
  })
})
