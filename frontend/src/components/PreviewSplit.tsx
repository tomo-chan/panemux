import React, { useCallback, useEffect, useRef, useState } from 'react'
import type { PreviewView } from '../hooks/usePreview'
import {
  PREVIEW_SIDE_MIN_PX,
  PREVIEW_SPLIT_DEFAULT,
  PREVIEW_SPLIT_MAX,
  PREVIEW_SPLIT_MIN,
  PREVIEW_SPLIT_STEP,
  clampPreviewSplit,
  loadPreviewSplit,
  previewHostLabel,
  savePreviewSplit,
} from '../utils/preview'
import { CloseIcon, OpenInNewTabIcon, ReloadIcon } from './PaneHeaderIcons'

// A terminal's body split into the terminal on the left and a preview on the
// right (issue #385), shared by the workspace pane and the task dashboard's
// Type in pane popup. The terminal stays the same element whether or not a
// preview is open, so its input, IME, selection and connection carry on; only
// its width changes, which resizes the PTY as any pane resize does.

interface PreviewSplitProps {
  /** Where this pane's or task's ratio is kept in the browser. */
  splitKey: string
  view: PreviewView | null
  terminal: React.ReactNode
  onChoose: (path: string) => void
  onReload: () => void
  onClose: () => void
}

export const PreviewSplit: React.FC<PreviewSplitProps> = ({ splitKey, view, terminal, onChoose, onReload, onClose }) => {
  const splitRef = useRef<HTMLDivElement>(null)
  const [ratio, setRatio] = useState(() => loadPreviewSplit(splitKey))
  const [dragPointer, setDragPointer] = useState<number | null>(null)
  const ratioRef = useRef(ratio)
  ratioRef.current = ratio

  useEffect(() => {
    setRatio(loadPreviewSplit(splitKey))
  }, [splitKey])

  const commit = useCallback((value: number) => {
    const next = clampPreviewSplit(value)
    setRatio(next)
    savePreviewSplit(splitKey, next)
  }, [splitKey])

  const ratioAt = (clientX: number): number | null => {
    const rect = splitRef.current?.getBoundingClientRect()
    if (!rect || rect.width <= 0) return null
    return clampPreviewSplit(Math.round(((clientX - rect.left) / rect.width) * 100))
  }

  const handlePointerDown = (event: React.PointerEvent<HTMLDivElement>) => {
    if (event.pointerType === 'mouse' && event.button !== 0) return
    event.preventDefault()
    // Captured, so the drag keeps reaching the divider over the preview's frame.
    event.currentTarget.setPointerCapture(event.pointerId)
    setDragPointer(event.pointerId)
  }

  const handlePointerMove = (event: React.PointerEvent<HTMLDivElement>) => {
    if (dragPointer !== event.pointerId) return
    const next = ratioAt(event.clientX)
    if (next !== null) setRatio(next)
  }

  const endDrag = (event: React.PointerEvent<HTMLDivElement>) => {
    if (dragPointer !== event.pointerId) return
    setDragPointer(null)
    event.currentTarget.releasePointerCapture?.(event.pointerId)
    savePreviewSplit(splitKey, ratioRef.current)
  }

  const handleKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    const delta = event.key === 'ArrowLeft' ? -PREVIEW_SPLIT_STEP : event.key === 'ArrowRight' ? PREVIEW_SPLIT_STEP : 0
    if (delta === 0) return
    event.preventDefault()
    commit(ratio + delta)
  }

  const minWidth = `${PREVIEW_SIDE_MIN_PX}px`

  return (
    <div ref={splitRef} className="pm-preview-split" data-dragging={dragPointer !== null ? 'true' : undefined}>
      {/* The terminal's share may shrink, down to its own minimum, so the
          preview keeps its 160 px when the ratio would leave it less. */}
      <div
        className="pm-preview-terminal"
        style={view ? { flex: `0 1 ${ratio}%`, minWidth } : { flex: '1 1 100%' }}
      >
        {terminal}
      </div>
      {view && (
        <>
          <div
            role="separator"
            aria-orientation="vertical"
            aria-label="Resize terminal and preview"
            aria-valuemin={PREVIEW_SPLIT_MIN}
            aria-valuemax={PREVIEW_SPLIT_MAX}
            aria-valuenow={ratio}
            tabIndex={0}
            title="Drag, or use the arrow keys, to resize. Double-click for 50:50."
            className="pm-preview-divider"
            onPointerDown={handlePointerDown}
            onPointerMove={handlePointerMove}
            onPointerUp={endDrag}
            onPointerCancel={endDrag}
            onKeyDown={handleKeyDown}
            onDoubleClick={() => commit(PREVIEW_SPLIT_DEFAULT)}
          />
          <PreviewPanel view={view} minWidth={minWidth} onChoose={onChoose} onReload={onReload} onClose={onClose} />
        </>
      )}
    </div>
  )
}

interface PreviewPanelProps {
  view: PreviewView
  minWidth: string
  onChoose: (path: string) => void
  onReload: () => void
  onClose: () => void
}

const PreviewPanel: React.FC<PreviewPanelProps> = ({ view, minWidth, onChoose, onReload, onClose }) => {
  const ready = view.kind === 'ready' ? view : null
  const frameRef = useRef<HTMLIFrameElement>(null)
  const pageFocused = usePageFocus(frameRef, view)
  const target = view.kind === 'ready' ? view.path : view.request
  const host = view.kind === 'ready' || view.kind === 'choose' ? previewHostLabel(view.host) : null

  return (
    <section className="pm-preview" aria-label="Preview" style={{ minWidth }}>
      <header className="pm-preview-head">
        <div className="pm-preview-target" title={target}>
          {host && <span className="pm-preview-host">{host}</span>}
          {host && <span aria-hidden="true"> · </span>}
          <span className="pm-preview-path">{target}</span>
        </div>
        <div className="pm-preview-actions">
          <button
            type="button"
            className="pm-preview-btn"
            aria-label="Reload preview"
            title="Reload"
            disabled={!ready}
            onClick={onReload}
          >
            <ReloadIcon />
          </button>
          <button
            type="button"
            className="pm-preview-btn pm-preview-btn-text"
            disabled={!ready}
            onClick={() => {
              if (ready) window.open(ready.url, '_blank', 'noopener,noreferrer')
            }}
          >
            <OpenInNewTabIcon />
            Open in new tab
          </button>
          <button type="button" className="pm-preview-btn" aria-label="Close preview" title="Close preview" onClick={onClose}>
            <CloseIcon />
          </button>
        </div>
      </header>
      {pageFocused && (
        <p role="status" className="pm-preview-keys">
          Keys go to the page. Click the terminal to use panemux&apos;s shortcuts.
        </p>
      )}
      <div className="pm-preview-body">
        {view.kind === 'ready' && (
          <iframe
            ref={frameRef}
            key={view.generation}
            className="pm-preview-frame"
            src={view.url}
            title={`Preview of ${view.path}`}
            // The server's policy sandboxes the page too; this keeps it out of
            // the dashboard's origin even if that header were lost.
            sandbox="allow-scripts"
            referrerPolicy="no-referrer"
          />
        )}
        {view.kind === 'loading' && <p className="pm-preview-note">Opening {view.request}…</p>}
        {view.kind === 'error' && (
          <p role="alert" className="pm-preview-note" data-tone="error">
            {view.message}
          </p>
        )}
        {view.kind === 'choose' && (
          <div className="pm-preview-note">
            <p>{view.request} is in more than one place. Choose one:</p>
            <ul className="pm-preview-choices">
              {view.candidates.map((candidate) => (
                <li key={candidate}>
                  <button type="button" className="pm-preview-choice" onClick={() => onChoose(candidate)}>
                    {candidate}
                  </button>
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
    </section>
  )
}

// Whether the previewed page holds the keyboard. Its keys never reach this
// window — it is sandboxed in another origin — so panemux's shortcuts do
// nothing until focus is back. The window loses focus as the page takes it,
// and regains it as focus returns; a reload replaces the frame, and its focus
// with it.
function usePageFocus(frameRef: React.RefObject<HTMLIFrameElement | null>, view: PreviewView): boolean {
  const [focused, setFocused] = useState(false)
  useEffect(() => {
    const check = () => setFocused(frameRef.current !== null && document.activeElement === frameRef.current)
    check()
    window.addEventListener('blur', check)
    window.addEventListener('focus', check)
    return () => {
      window.removeEventListener('blur', check)
      window.removeEventListener('focus', check)
    }
  }, [frameRef, view])
  return focused
}
