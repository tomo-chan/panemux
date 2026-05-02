const MAX_TAIL_LENGTH = 1200

const ATTENTION_PATTERNS = [
  /\b(codex|claude|agent)\b[\s\S]{0,160}\b(confirm|confirmation|approve|approval|permission|allow|proceed)\b/i,
  /\bYes,\s*proceed\s*\(y\)[\s\S]{0,160}\bNo,\s*and\s*tell\s+(Codex|Claude|agent)\s+what\s+to\s+do\s+differently\s*\(esc\)/i,
  /Do you want to proceed\?[\s\S]{0,120}\b1\.\s*Yes\b[\s\S]{0,80}\b2\.\s*No\b[\s\S]{0,160}Esc to cancel\s*·\s*Tab to amend\s*·\s*ctrl\+e to explain/i,
  /(確認|承認|許可)[\s\S]{0,120}(必要|してください|しますか|待ち)/,
]

export interface AgentAttentionDetector {
  feed: (chunk: string) => boolean
  reset: () => void
}

export function createAgentAttentionDetector(): AgentAttentionDetector {
  let tail = ''

  return {
    feed(chunk: string) {
      tail = (tail + stripAnsi(chunk)).slice(-MAX_TAIL_LENGTH)
      const matched = ATTENTION_PATTERNS.some((pattern) => pattern.test(tail))
      if (matched) {
        tail = ''
      }
      return matched
    },
    reset() {
      tail = ''
    },
  }
}

function stripAnsi(value: string): string {
  return value.replace(/\x1B(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1B\\))/g, '')
}
