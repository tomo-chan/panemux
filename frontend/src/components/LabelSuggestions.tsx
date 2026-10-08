import React, { useId, useState } from 'react'
import { foldLabelSuggestions } from '../utils/labelSuggestions'
import { labelColor } from '../utils/taskBoard'

/** How many suggestions show before the rest fold behind "+<n> more". */
export const LABEL_SUGGESTION_FOLD = 8

export interface LabelSuggestionsProps {
  /** The suggestions to offer, in the order to show them. */
  labels: string[]
  /**
   * Makes each tag a toggle: the labels given are entered, shown pressed (✓)
   * and never folded away. Without it each tag adds its label (+).
   */
  pressed?: string[]
  onPick: (label: string) => void
  /** Said after the tags, for when there is nothing to offer for what was typed. */
  message?: string | null
  disabled?: boolean
}

// The "Used before:" tags under a label input (issue #310).
export const LabelSuggestions: React.FC<LabelSuggestionsProps> = ({
  labels,
  pressed,
  onPick,
  message = null,
  disabled = false,
}) => {
  const id = useId()
  const [expanded, setExpanded] = useState(false)
  if (labels.length === 0 && !message) return null
  const { shown, folded } = foldLabelSuggestions(labels, LABEL_SUGGESTION_FOLD, { keep: pressed, expanded })

  return (
    <div className="td-suggest" role="group" id={`${id}-group`} aria-labelledby={`${id}-caption`}>
      <span id={`${id}-caption`} className="td-suggest-caption">
        Used before:
      </span>
      {shown.map((label) => {
        const on = pressed?.includes(label)
        return (
          <button
            key={label}
            type="button"
            className="td-suggest-tag"
            style={{ '--td-lc': labelColor(label) } as React.CSSProperties}
            aria-pressed={on}
            aria-label={pressed ? undefined : `Add label ${label}`}
            disabled={disabled}
            onClick={() => onPick(label)}
          >
            <span aria-hidden="true">{on ? '✓' : '+'}</span> {label}
          </button>
        )
      })}
      {message && <span className="td-suggest-note">{message}</span>}
      {folded > 0 && (
        <button
          type="button"
          className="td-suggest-more"
          aria-expanded={expanded}
          aria-controls={`${id}-group`}
          onClick={() => setExpanded((current) => !current)}
        >
          {expanded ? 'Show less' : `+${folded} more`}
        </button>
      )}
    </div>
  )
}
