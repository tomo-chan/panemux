const ANSI_PATTERN = /\x1b\[[0-?]*[ -/]*[@-~]/g

const CONFIRMATION_PATTERNS = [
  /\bdo you want to\b/i,
  /\bwould you like to\b/i,
  /\bproceed\?\s*$/i,
  /\bcontinue\?\s*$/i,
  /\bapprove\?\s*$/i,
  /\ballow\?\s*$/i,
  /\bconfirm\?\s*$/i,
  /\b(y\/n|yes\/no)\b/i,
  /\b(approve|allow|accept|reject|continue|proceed)\b.*\?/i,
  /確認.*(しますか|してください|待ち)/,
  /(承認|許可).*(しますか|してください|待ち)/,
]

export function containsAgentConfirmationPrompt(output: string): boolean {
  const normalized = output
    .replace(ANSI_PATTERN, '')
    .replace(/\r/g, '\n')
    .split('\n')
    .slice(-6)
    .join('\n')

  return CONFIRMATION_PATTERNS.some((pattern) => pattern.test(normalized))
}
