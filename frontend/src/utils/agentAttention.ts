const MAX_TAIL_LENGTH = 1200

const ATTENTION_PATTERNS = [
  /\b(codex|claude|agent)\b.{0,160}\b(confirm|confirmation|approve|approval|permission|allow|proceed)\b/i,
  /(確認|承認|許可).{0,120}(必要|してください|しますか|待ち)/,
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
