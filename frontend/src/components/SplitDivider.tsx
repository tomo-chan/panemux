import React, { useCallback, useRef } from 'react'

interface SplitDividerProps {
  direction: 'horizontal' | 'vertical'
  onDrag: (delta: number) => void
  onResizeStart?: () => void
  onResizeEnd?: () => void
}

export const SplitDivider: React.FC<SplitDividerProps> = ({ direction, onDrag, onResizeStart, onResizeEnd }) => {
  const startRef = useRef<number>(0)
  const onDragRef = useRef(onDrag)
  onDragRef.current = onDrag
  const onResizeStartRef = useRef(onResizeStart)
  onResizeStartRef.current = onResizeStart
  const onResizeEndRef = useRef(onResizeEnd)
  onResizeEndRef.current = onResizeEnd
  const isHorizontal = direction === 'horizontal'

  const handleMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault()
    startRef.current = isHorizontal ? e.clientX : e.clientY
    onResizeStartRef.current?.()

    const handleMouseMove = (moveEvent: MouseEvent) => {
      const current = isHorizontal ? moveEvent.clientX : moveEvent.clientY
      const delta = current - startRef.current
      startRef.current = current
      onDragRef.current(delta)
    }

    const handleMouseUp = () => {
      onResizeEndRef.current?.()
      document.removeEventListener('mousemove', handleMouseMove)
      document.removeEventListener('mouseup', handleMouseUp)
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
    }

    document.body.style.cursor = isHorizontal ? 'col-resize' : 'row-resize'
    document.body.style.userSelect = 'none'
    document.addEventListener('mousemove', handleMouseMove)
    document.addEventListener('mouseup', handleMouseUp)
  }, [isHorizontal])

  return (
    <div
      onMouseDown={handleMouseDown}
      style={{
        flexShrink: 0,
        width: isHorizontal ? '4px' : '100%',
        height: isHorizontal ? '100%' : '4px',
        backgroundColor: '#333',
        cursor: isHorizontal ? 'col-resize' : 'row-resize',
        transition: 'background-color 0.15s',
        position: 'relative',
        zIndex: 10,
      }}
      onMouseEnter={(e) => {
        (e.currentTarget as HTMLDivElement).style.backgroundColor = '#569cd6'
      }}
      onMouseLeave={(e) => {
        (e.currentTarget as HTMLDivElement).style.backgroundColor = '#333'
      }}
    />
  )
}
