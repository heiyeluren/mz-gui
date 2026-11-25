import { useEffect, useRef, useState } from 'react'
import './ResizeHandle.css'

export type ResizeHandleProps = {
  ariaLabel: string
  title?: string
  className?: string
  orientation?: 'vertical' | 'horizontal'
  value: number
  min: number
  max: number
  onChange: (value: number) => void
  onCommit?: (value: number) => void
  onReset?: () => void
}

/** 通用分隔线：纵线调整左侧宽度，横线调整上方高度；数值使用布局像素。 */
export function ResizeHandle({ ariaLabel, title, className = '', orientation = 'vertical', value, min, max, onChange, onCommit, onReset }: ResizeHandleProps) {
  const horizontal = orientation === 'horizontal'
  const drag = useRef<{ pointerID: number, startPosition: number, startValue: number, value: number, scale: number } | null>(null)
  const [dragging, setDragging] = useState(false)
  const callbacks = useRef({ onChange })
  callbacks.current = { onChange }
  const clamp = (next: number) => Math.round(Math.min(Math.max(min, max), Math.max(min, next)))

  const cancel = () => {
    const active = drag.current
    drag.current = null
    setDragging(false)
    if (active) callbacks.current.onChange(active.startValue)
  }

  useEffect(() => {
    const onBlur = () => { if (drag.current) cancel() }
    window.addEventListener('blur', onBlur)
    return () => { window.removeEventListener('blur', onBlur); drag.current = null }
  }, [])

  return <div role="separator" tabIndex={0} aria-label={ariaLabel} aria-orientation={orientation}
    aria-valuemin={min} aria-valuemax={Math.max(min, max)} aria-valuenow={clamp(value)}
    title={title ?? ariaLabel} className={`mzgui-resize-handle ${className}`} data-resizing={dragging || undefined}
    onPointerDown={(event) => {
      if (event.button !== 0 || event.isPrimary === false || drag.current) return
      event.preventDefault()
      const handle = event.currentTarget
      const size = horizontal ? handle.offsetHeight : handle.offsetWidth
      const rect = handle.getBoundingClientRect()
      const scale = size ? (horizontal ? rect.height : rect.width) / size : 1
      drag.current = { pointerID: event.pointerId, startPosition: horizontal ? event.clientY : event.clientX, startValue: value, value, scale: scale > 0 ? scale : 1 }
      handle.focus({ preventScroll: true })
      handle.setPointerCapture?.(event.pointerId)
      setDragging(true)
    }}
    onPointerMove={(event) => {
      const active = drag.current
      if (!active || event.pointerId !== active.pointerID) return
      const next = clamp(active.startValue + ((horizontal ? event.clientY : event.clientX) - active.startPosition) / active.scale)
      active.value = next
      onChange(next)
    }}
    onPointerUp={(event) => {
      const active = drag.current
      if (!active || event.pointerId !== active.pointerID) return
      drag.current = null
      setDragging(false)
      event.currentTarget.releasePointerCapture?.(event.pointerId)
      onCommit?.(clamp(active.value))
    }}
    onPointerCancel={cancel} onLostPointerCapture={cancel}
    onDoubleClick={() => onReset?.()}
    onKeyDown={(event) => {
      if (event.key === 'Escape' && drag.current) { event.preventDefault(); cancel(); return }
      if (drag.current) return
      const next = event.key === (horizontal ? 'ArrowUp' : 'ArrowLeft') ? value - 10 : event.key === (horizontal ? 'ArrowDown' : 'ArrowRight') ? value + 10
        : event.key === 'Home' ? min : event.key === 'End' ? max : undefined
      if (next === undefined) return
      event.preventDefault()
      onChange(clamp(next))
      onCommit?.(clamp(next))
    }} />
}
