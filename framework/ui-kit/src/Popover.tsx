import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode, type RefObject } from 'react'
import { createPortal } from 'react-dom'
import { trapFocus } from './focus'

export type PopoverProps = {
  open: boolean
  anchor: RefObject<HTMLElement>
  placementAnchor?: RefObject<HTMLElement>
  id: string
  title: string
  className?: string
  width?: number
  onDismiss: () => void
  children: ReactNode
}

/** 锚点弹层挂到 body，避免被滚动容器裁切；保留主题、焦点与视口边界。 */
export function Popover({ open, anchor, placementAnchor, id, title, className = '', width = 420, onDismiss, children }: PopoverProps) {
  const panel = useRef<HTMLDivElement>(null)
  const dismiss = useRef(onDismiss)
  dismiss.current = onDismiss
  const [position, setPosition] = useState<CSSProperties>({ visibility: 'hidden' })

  useLayoutEffect(() => {
    if (!open) return
    const place = () => {
      if (!anchor.current || !panel.current) return
      const rect = anchor.current.getBoundingClientRect()
      const edge = placementAnchor?.current?.getBoundingClientRect() ?? rect
      const margin = 12, gap = 10
      const panelWidth = Math.min(width, Math.max(0, window.innerWidth - margin * 2))
      const above = Math.max(0, edge.top - margin - gap)
      const below = Math.max(0, window.innerHeight - edge.bottom - margin - gap)
      const upward = above >= Math.min(240, panel.current.scrollHeight) || above >= below
      const maxHeight = Math.max(0, Math.min(520, upward ? above : below))
      const height = Math.min(panel.current.getBoundingClientRect().height, maxHeight)
      const theme = getComputedStyle(anchor.current)
      const custom: Record<string, string> = {}
      for (const name of Array.from(theme)) if (name.startsWith('--')) custom[name] = theme.getPropertyValue(name)
      setPosition({ ...custom, position: 'fixed', zIndex: 2000, visibility: 'visible', width: panelWidth, maxHeight,
        left: Math.max(margin, Math.min(rect.left, window.innerWidth - panelWidth - margin)),
        top: upward ? Math.max(margin, edge.top - height - gap) : edge.bottom + gap,
        fontFamily: theme.fontFamily,
      })
    }
    place()
    const resize = new ResizeObserver(place)
    if (panel.current) resize.observe(panel.current)
    if (anchor.current) resize.observe(anchor.current)
    if (placementAnchor?.current) resize.observe(placementAnchor.current)
    const scroll = (event: Event) => { if (!panel.current?.contains(event.target as Node)) place() }
    window.addEventListener('resize', place)
    window.addEventListener('scroll', scroll, true)
    return () => { resize.disconnect(); window.removeEventListener('resize', place); window.removeEventListener('scroll', scroll, true) }
  }, [open, anchor, placementAnchor, width])

  // 首次定位前处于隐藏态，真实浏览器不会让隐藏内容获得焦点。
  useLayoutEffect(() => {
    if (!open || position.visibility !== 'visible') return
    const first = panel.current?.querySelector<HTMLElement>('[data-autofocus], input, select, button:not([disabled])')
    ;(first ?? panel.current)?.focus()
  }, [open, position.visibility])

  useEffect(() => {
    if (!open) return
    const current = panel.current
    const trigger = anchor.current
    const outside = (event: Event) => {
      if (!current?.contains(event.target as Node) && !trigger?.contains(event.target as Node)) dismiss.current()
    }
    const keyboard = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !event.isComposing) { event.preventDefault(); event.stopPropagation(); dismiss.current() }
      else trapFocus(event, current)
    }
    document.addEventListener('pointerdown', outside)
    document.addEventListener('focusin', outside)
    document.addEventListener('keydown', keyboard)
    return () => {
      document.removeEventListener('pointerdown', outside)
      document.removeEventListener('focusin', outside)
      document.removeEventListener('keydown', keyboard)
      if (current?.contains(document.activeElement) || document.activeElement === document.body) trigger?.focus()
    }
  }, [open, anchor])

  return open ? createPortal(<div ref={panel} id={id} className={`mzgui-popover ${className}`} role="dialog" aria-modal="true" aria-label={title} tabIndex={-1} style={position}>{children}</div>, document.body) : null
}
