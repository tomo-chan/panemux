/**
 * Marks the codex permission request a wait signature names as answered
 * (POST /api/tasks/input-given, issue #384), answering why it failed or null.
 * The Tasks detail, the Type in pane popup and a workspace pane (issue #404)
 * all send it here.
 */
export async function postInputGiven(signature: string): Promise<string | null> {
  try {
    const res = await fetch('/api/tasks/input-given', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ wait_signature: signature }),
    })
    if (!res.ok) return (await res.text()).trim() || `HTTP ${res.status}`
  } catch (err) {
    return err instanceof Error ? err.message : 'Failed to mark the input given'
  }
  return null
}
