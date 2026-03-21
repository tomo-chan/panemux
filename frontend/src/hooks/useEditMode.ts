import { useState, useEffect, useCallback } from 'react'
import { EditModeResponseSchema } from '../schemas'

export function useEditMode() {
  const [editMode, setEditModeState] = useState(false)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetch('/api/edit-mode')
      .then(r => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`)
        return r.json()
      })
      .then(data => {
        setEditModeState(EditModeResponseSchema.parse(data).editMode)
      })
      .catch(e => setError((e as Error).message))
      .finally(() => setIsLoading(false))
  }, [])

  const setEditMode = useCallback(async (value: boolean) => {
    const prev = editMode
    setEditModeState(value)
    try {
      const r = await fetch('/api/edit-mode', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ editMode: value }),
      })
      if (!r.ok) throw new Error(`HTTP ${r.status}`)
      const data = EditModeResponseSchema.parse(await r.json())
      setEditModeState(data.editMode)
    } catch {
      setEditModeState(prev)
    }
  }, [editMode])

  const toggleEditMode = useCallback(() => setEditMode(!editMode), [editMode, setEditMode])

  return { editMode, isLoading, error, toggleEditMode, setEditMode }
}
