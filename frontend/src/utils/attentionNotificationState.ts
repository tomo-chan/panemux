const LAST_NOTIFIED_SIGNATURES_KEY = 'panemux:last-notified-attention-signatures'

let inMemoryFallback = new Map<string, string>()

export function getLastNotifiedAttentionSignature(paneId: string): string | null {
  return readSignatureMap().get(paneId) ?? null
}

export function setLastNotifiedAttentionSignature(paneId: string, signature: string) {
  const signatures = readSignatureMap()
  signatures.set(paneId, signature)
  writeSignatureMap(signatures)
}

function readSignatureMap(): Map<string, string> {
  const storage = getStorage()
  if (!storage) return new Map(inMemoryFallback)

  try {
    const raw = storage.getItem(LAST_NOTIFIED_SIGNATURES_KEY)
    if (!raw) return new Map()

    const parsed = JSON.parse(raw) as unknown
    if (!parsed || typeof parsed !== 'object') return new Map()

    return new Map(
      Object.entries(parsed).filter((entry): entry is [string, string] => typeof entry[0] === 'string' && typeof entry[1] === 'string'),
    )
  } catch {
    return new Map(inMemoryFallback)
  }
}

function writeSignatureMap(signatures: Map<string, string>) {
  const storage = getStorage()
  if (!storage) {
    inMemoryFallback = new Map(signatures)
    return
  }

  const value = JSON.stringify(Object.fromEntries(signatures))

  try {
    storage.setItem(LAST_NOTIFIED_SIGNATURES_KEY, value)
  } catch {
    inMemoryFallback = new Map(signatures)
  }
}

function getStorage(): Storage | null {
  try {
    return window.localStorage
  } catch {
    return null
  }
}
