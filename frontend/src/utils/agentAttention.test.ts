import { describe, expect, it } from 'vitest'
import { createAgentAttentionDetector } from './agentAttention'

describe('createAgentAttentionDetector', () => {
  it('detects an agent confirmation prompt in terminal output', () => {
    const detector = createAgentAttentionDetector()

    expect(detector.feed('Codex needs confirmation before continuing. Approve?')).toMatchObject({
      signature: 'codex needs confirmation before continuing. approve',
    })
  })

  it('detects prompts split across terminal frames after stripping ANSI escapes', () => {
    const detector = createAgentAttentionDetector()

    expect(detector.feed('\x1b[33mAgent is waiting for conf')).toBeNull()
    expect(detector.feed('irmation\x1b[0m: proceed?')).toMatchObject({
      signature: 'agent is waiting for confirmation: proceed',
    })
  })

  it('detects agent confirmation prompts split across terminal lines', () => {
    const detector = createAgentAttentionDetector()

    expect(detector.feed('Codex is waiting for your\napproval before it can proceed.')).toMatchObject({
      signature: 'codex is waiting for your approval before it can proceed',
    })
  })

  it('detects Japanese approval prompts split across terminal lines', () => {
    const detector = createAgentAttentionDetector()

    expect(detector.feed('操作の承認\nが必要です')).toMatchObject({
      signature: '承認 が必要',
    })
  })

  it('detects Codex approval menus with yes and no options', () => {
    const detector = createAgentAttentionDetector()

    expect(
      detector.feed(' 1. Yes, proceed (y)\n 2. No, and tell Codex what to do differently (esc)'),
    ).toMatchObject({
      signature: 'yes, proceed (y) 2. no, and tell codex what to do differently (esc)',
    })
  })

  it('detects Claude approval menus with yes and no options', () => {
    const detector = createAgentAttentionDetector()

    expect(
      detector.feed(' 1. Yes, proceed (y)\n 2. No, and tell Claude what to do differently (esc)'),
    ).toMatchObject({
      signature: 'yes, proceed (y) 2. no, and tell claude what to do differently (esc)',
    })
  })

  it('detects Codex edit approval menus with a do-not-ask-again option', () => {
    const detector = createAgentAttentionDetector()

    expect(
      detector.feed(
        [
          'Would you like to make the following edits?',
          '',
          '› 1. Yes, proceed (y)',
          "  2. Yes, and don't ask again for these files (a)",
          '  3. No, and tell Codex what to do differently (esc)',
        ].join('\n'),
      ),
    ).toMatchObject({
      signature: "yes, proceed (y) 2. yes, and don't ask again for these files (a) 3. no, and tell codex what to do differently (esc)",
    })
  })

  it('detects Codex command approval menus with scoped do-not-ask-again options', () => {
    const detector = createAgentAttentionDetector()

    expect(
      detector.feed(
        [
          'Would you like to run the following command?',
          '',
          '  Reason: PR #74 にフォローアップコメントを投稿するため、GitHub API へのネットワークアクセス付きで gh pr comment を再実行してよいですか？',
          '',
          '  $ gh pr comment 74 --body-file /tmp/pr74-comment-8f80598.md',
          '',
          '› 1. Yes, proceed (y)',
          "  2. Yes, and don't ask again for commands that start with `gh pr comment` (p)",
          '  3. No, and tell Codex what to do differently (esc)',
        ].join('\n'),
      ),
    ).toMatchObject({
      signature: "yes, proceed (y) 2. yes, and don't ask again for commands that start with `gh pr comment` (p) 3. no, and tell codex what to do differently (esc)",
    })
  })

  it('detects Claude bash command approval menus', () => {
    const detector = createAgentAttentionDetector()

    expect(
      detector.feed(
        [
          'Bash command',
          '',
          '  gh api repos/tomo-chan/panemux/pulls/74/reviews \\',
          '    --method POST \\',
          '    --field event="COMMENT"',
          '  Post follow-up review for PR #74 at new head SHA',
          '',
          'Brace expansion',
          '',
          'Do you want to proceed?',
          '❯ 1. Yes',
          '  2. No',
          '',
          'Esc to cancel · Tab to amend · ctrl+e to explain',
        ].join('\n'),
      ),
    ).toMatchObject({
      signature: 'do you want to proceed? ❯ 1. yes 2. no esc to cancel · tab to amend · ctrl+e to explain',
    })
  })

  it('detects Codex MCP approval menus with allow options', () => {
    const detector = createAgentAttentionDetector()

    expect(
      detector.feed(
        [
          'Field 1/1',
          'Allow the maestro MCP server to run tool "query_docs"?',
          '',
          'question: Ignore this if not relevant.',
          '',
          '› 1. Allow                   Run the tool and continue.',
          '  2. Allow for this session  Run the tool and remember this choice for this session.',
          '  3. Always allow            Run the tool and remember this choice for future tool calls.',
          '  4. Cancel                  Cancel this tool call',
          'enter to submit | esc to cancel',
        ].join('\n'),
      ),
    ).toMatchObject({
      signature: 'allow the maestro mcp server to run tool "query_docs"? question: ignore this if not relevant. › 1. allow run the tool and continue. 2. allow for this session run the tool and remember this choice for this session. 3. always allow run the tool and remember this choice for future tool calls. 4. cancel cancel this tool call enter to submit | esc to cancel',
    })
  })

  it('detects Codex MCP approval menus for other servers and tools', () => {
    const detector = createAgentAttentionDetector()

    expect(
      detector.feed(
        [
          'Field 1/1',
          'Allow the github MCP server to run tool "list_pull_requests"?',
          '',
          'question: Ignore this if not relevant.',
          '',
          '› 1. Allow                   Run the tool and continue.',
          '  2. Allow for this session  Run the tool and remember this choice for this session.',
          '  3. Always allow            Run the tool and remember this choice for future tool calls.',
          '  4. Cancel                  Cancel this tool call',
          'enter to submit | esc to cancel',
        ].join('\n'),
      ),
    ).toMatchObject({
      signature: 'allow the github mcp server to run tool "list_pull_requests"? question: ignore this if not relevant. › 1. allow run the tool and continue. 2. allow for this session run the tool and remember this choice for this session. 3. always allow run the tool and remember this choice for future tool calls. 4. cancel cancel this tool call enter to submit | esc to cancel',
    })
  })

  it('detects non-MCP allow menus with the same Codex approval structure', () => {
    const detector = createAgentAttentionDetector()

    expect(
      detector.feed(
        [
          'Field 1/1',
          'Allow access to workspace "production"?',
          '',
          'question: Ignore this if not relevant.',
          '',
          '› 1. Allow                   Run the tool and continue.',
          '  2. Allow for this session  Run the tool and remember this choice for this session.',
          '  3. Always allow            Run the tool and remember this choice for future tool calls.',
          '  4. Cancel                  Cancel this tool call',
          'enter to submit | esc to cancel',
        ].join('\n'),
      ),
    ).toMatchObject({
      signature: 'allow access to workspace "production"? question: ignore this if not relevant. › 1. allow run the tool and continue. 2. allow for this session run the tool and remember this choice for this session. 3. always allow run the tool and remember this choice for future tool calls. 4. cancel cancel this tool call enter to submit | esc to cancel',
    })
  })

  it('does not match ordinary terminal output', () => {
    const detector = createAgentAttentionDetector()

    expect(detector.feed('npm test finished successfully')).toBeNull()
  })

  it('does not match generic non-agent confirmation prompts', () => {
    const detector = createAgentAttentionDetector()

    expect(detector.feed('Need to install the following packages: vite. Proceed? [y/N]')).toBeNull()
    expect(detector.feed('Allow Homebrew to make changes?')).toBeNull()
  })

  it('does not keep matching stale prompt text after it has emitted', () => {
    const detector = createAgentAttentionDetector()

    expect(detector.feed('Codex needs confirmation before continuing. Approve?')).toMatchObject({
      signature: 'codex needs confirmation before continuing. approve',
    })
    expect(detector.feed('\nnormal command output after the prompt')).toBeNull()
  })

  it('normalizes whitespace so repeated redraws produce the same signature', () => {
    const detector = createAgentAttentionDetector()

    const firstMatch = detector.feed('Codex needs confirmation before continuing.\n\nApprove?')
    const secondMatch = detector.feed('Codex needs confirmation before continuing.   Approve?')

    expect(firstMatch?.signature).toBe('codex needs confirmation before continuing. approve')
    expect(secondMatch?.signature).toBe('codex needs confirmation before continuing. approve')
  })
})
