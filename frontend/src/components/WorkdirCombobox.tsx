import React, { useEffect, useId, useState } from 'react'
import { filterWorkdirs, highlightMatch, lastUsedLabel, type RecentWorkdir } from '../utils/workdirSuggestions'

// The New task form's Working directory field (issue #311): a text input with
// the directories the chosen host's tasks ran in, in a list under it. The list
// filters by what is typed; a path that is not in it can still be typed.
// Escape is the dialog's (useModalKeyboard listens before the input does), so
// the dialog closes the list through onOpenChange.

export interface WorkdirComboboxProps {
  value: string
  onChange: (value: string) => void
  /** The chosen host as the form names it, for the list's heading. */
  hostName: string
  suggestions: RecentWorkdir[]
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Whether a focus should open the list; the dialog's own focus on opening should not. */
  openOnFocus: () => boolean
  disabled?: boolean
  inputRef?: React.Ref<HTMLInputElement>
}

export const WorkdirCombobox: React.FC<WorkdirComboboxProps> = ({
  value,
  onChange,
  hostName,
  suggestions,
  open,
  onOpenChange,
  openOnFocus,
  disabled,
  inputRef,
}) => {
  const id = useId()
  const inputId = `${id}-input`
  const listId = `${id}-list`
  const optionId = (index: number) => `${id}-option-${index}`
  const [active, setActive] = useState(-1)

  const shown = filterWorkdirs(suggestions, value)
  const current = open && active < shown.length ? active : -1
  const listed = open && shown.length > 0

  useEffect(() => {
    if (current >= 0) document.getElementById(`${id}-option-${current}`)?.scrollIntoView?.({ block: 'nearest' })
  }, [current, id])

  const setOpen = (next: boolean) => {
    setActive(-1)
    onOpenChange(next)
  }

  const pick = (path: string) => {
    onChange(path)
    setOpen(false)
  }

  const onKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      if (!open) onOpenChange(true)
      if (shown.length > 0) setActive(current < 0 || current >= shown.length - 1 ? 0 : current + 1)
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      if (!open) onOpenChange(true)
      if (shown.length > 0) setActive(current <= 0 ? shown.length - 1 : current - 1)
    } else if (event.key === 'Enter' && current >= 0) {
      event.preventDefault()
      pick(shown[current].path)
    }
  }

  const now = Date.now()
  return (
    <div
      className="td-field td-workdir"
      onBlur={(event) => {
        if (open && !event.currentTarget.contains(event.relatedTarget as Node | null)) setOpen(false)
      }}
    >
      <label htmlFor={inputId}>Working directory</label>
      <div className="td-workdir-row">
        <input
          ref={inputRef}
          id={inputId}
          className="td-mono"
          value={value}
          placeholder="/workspace/user/project"
          role="combobox"
          aria-autocomplete="list"
          aria-expanded={listed}
          aria-controls={listed ? listId : undefined}
          aria-activedescendant={current >= 0 ? optionId(current) : undefined}
          onChange={(event) => {
            onChange(event.target.value)
            setActive(-1)
            if (!open) onOpenChange(true)
          }}
          onFocus={() => {
            if (!open && openOnFocus()) setOpen(true)
          }}
          onClick={() => {
            if (!open) setOpen(true)
          }}
          onKeyDown={onKeyDown}
          disabled={disabled}
          spellCheck={false}
          autoComplete="off"
        />
        <button
          type="button"
          className="td-workdir-toggle"
          aria-label="Show recent directories"
          aria-expanded={open}
          tabIndex={-1}
          disabled={disabled}
          onMouseDown={(event) => event.preventDefault()}
          onClick={() => setOpen(!open)}
        >
          <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true">
            <path d="M2.5 4.5 6 8l3.5-3.5" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </button>
      </div>
      {open && (
        <div className="td-workdir-popup">
          <div className="td-workdir-head">
            <span id={`${id}-head`}>Recent on {hostName}</span>
            <span aria-hidden="true">last used</span>
          </div>
          {listed ? (
            <ul id={listId} role="listbox" aria-labelledby={`${id}-head`} className="td-workdir-list">
              {shown.map((w, index) => {
                const when = lastUsedLabel(w, now)
                return (
                  <li
                    key={w.path}
                    id={optionId(index)}
                    role="option"
                    aria-selected={index === current}
                    className="td-workdir-option"
                    onMouseDown={(event) => event.preventDefault()}
                    onMouseEnter={() => setActive(index)}
                    onClick={() => pick(w.path)}
                  >
                    <span className="td-workdir-path td-mono">
                      {highlightMatch(w.path, value).map((part, i) =>
                        part.match ? <mark key={i}>{part.text}</mark> : <React.Fragment key={i}>{part.text}</React.Fragment>,
                      )}
                    </span>
                    <span className="td-workdir-meta">{when ? `${w.agent} · ${when}` : w.agent}</span>
                  </li>
                )
              })}
            </ul>
          ) : (
            <p className="td-workdir-empty">
              {suggestions.length === 0
                ? 'No directory used on this host yet. Type an absolute path.'
                : 'No recent directory matches. Starting will use the path as typed.'}
            </p>
          )}
        </div>
      )}
    </div>
  )
}
