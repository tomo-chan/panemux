const MAX_TAIL_LENGTH = 1200

const ATTENTION_PATTERNS = [
  /\b(codex|claude|agent)\b[\s\S]{0,160}\b(confirm|confirmation|approve|approval|permission|allow|proceed)\b/i,
  /\bYes,\s*proceed\s*\(y\)[\s\S]{0,160}\bNo,\s*and\s*tell\s+(Codex|Claude|agent)\s+what\s+to\s+do\s+differently\s*\(esc\)/i,
  /Do you want to proceed\?[\s\S]{0,120}\b1\.\s*Yes\b[\s\S]{0,80}\b2\.\s*No\b[\s\S]{0,160}Esc to cancel\s*·\s*Tab to amend\s*·\s*ctrl\+e to explain/i,
  /Allow [^\n]+\?[\s\S]{0,240}\b1\.\s*Allow\b[\s\S]{0,160}\b2\.\s*Allow for this session\b[\s\S]{0,160}\b3\.\s*Always allow\b[\s\S]{0,160}\b4\.\s*Cancel\b[\s\S]{0,120}enter to submit\s*\|\s*esc to cancel/i,
  /(確認|承認|許可)[\s\S]{0,120}(必要|してください|しますか|待ち)/,
]

export interface AgentAttentionMatch {
  signature: string
}

export interface AgentAttentionDetector {
  feed: (chunk: string) => AgentAttentionMatch | null
  reset: () => void
}

export function createAgentAttentionDetector(): AgentAttentionDetector {
  let tail = ''

  return {
    feed(chunk: string) {
      tail = (tail + stripAnsi(chunk)).slice(-MAX_TAIL_LENGTH)
      const match = findAttentionMatch(tail)
      if (match) {
        tail = ''
        return {
          signature: normalizeAttentionText(match),
        }
      }
      return null
    },
    reset() {
      tail = ''
    },
  }
}

function findAttentionMatch(value: string): string | null {
  for (const pattern of ATTENTION_PATTERNS) {
    const match = value.match(pattern)
    if (match) return match[0]
  }

  return null
}

function normalizeAttentionText(value: string): string {
  return stripAnsi(value)
    .replace(/\s+/g, ' ')
    .trim()
    .toLowerCase()
}

function stripAnsi(value: string): string {
  return value.replace(/\x1B(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1B\\))/g, '')
}
