import { useEffect, useId, useRef, useState, type KeyboardEvent } from 'react'
import type { SelectProps } from './Select'
import './PopupSelect.css'

/** 随宿主主题呈现的单选菜单；焦点留在触发器，候选项通过 active descendant 导航。 */
export function PopupSelect<T extends string>({ id, label, value, options, disabled = false, busy = false, onChange }: SelectProps<T>) {
  const generatedID = useId()
  const listID = `${id ?? generatedID}-list`
  const root = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const list = useRef<HTMLUListElement>(null)
  const search = useRef({ text: '', at: 0 })
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)
  const [placement, setPlacement] = useState({ above: false, height: 320 })
  const blocked = disabled || busy || options.length === 0
  const expanded = open && !blocked
  const selected = options.find((option) => option.value === value)

  const show = () => {
    if (blocked) return
    const rect = trigger.current?.getBoundingClientRect()
    if (rect) {
      const below = window.innerHeight - rect.bottom - 16
      const above = rect.top - 16
      const up = below < Math.min(320, options.length * 38 + 12) && above > below
      setPlacement({ above: up, height: Math.max(38, Math.min(320, up ? above : below)) })
    }
    search.current = { text: '', at: 0 }
    setActive(Math.max(0, options.findIndex((option) => option.value === value)))
    setOpen(true)
  }

  useEffect(() => { if (blocked) setOpen(false) }, [blocked])
  useEffect(() => {
    if (!expanded) return
    const outside = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false)
    }
    const close = () => setOpen(false)
    document.addEventListener('pointerdown', outside)
    window.addEventListener('resize', close)
    return () => {
      document.removeEventListener('pointerdown', outside)
      window.removeEventListener('resize', close)
    }
  }, [expanded])
  useEffect(() => {
    if (expanded) list.current?.children[active]?.scrollIntoView?.({ block: 'nearest' })
  }, [active, expanded])

  const choose = (index: number) => {
    if (blocked || !options[index]) return
    onChange(options[index].value)
    setOpen(false)
    trigger.current?.focus()
  }
  const keyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    if (blocked) return
    if (event.key === 'Tab') { setOpen(false); return }
    if (event.key === 'Escape' && expanded) {
      event.preventDefault(); event.stopPropagation(); setOpen(false); return
    }
    if (['ArrowDown', 'ArrowUp', 'Home', 'End', 'Enter', ' '].includes(event.key)) {
      event.preventDefault()
      if (!expanded) { show(); return }
      if (event.key === 'Enter' || event.key === ' ') { choose(active); return }
      setActive((index) => event.key === 'Home' ? 0 : event.key === 'End' ? options.length - 1
        : Math.max(0, Math.min(options.length - 1, index + (event.key === 'ArrowDown' ? 1 : -1))))
    } else if (event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey) {
      event.preventDefault()
      if (!expanded) show()
      const now = Date.now()
      const text = (now - search.current.at < 600 ? search.current.text : '') + event.key.toLowerCase()
      search.current = { text, at: now }
      const index = options.findIndex((option) => option.label.toLowerCase().startsWith(text))
      if (index >= 0) setActive(index)
    }
  }

  return <div className="mzgui-popup-select" ref={root} onBlur={(event) => {
    if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setOpen(false)
  }}>
    <button data-click-name="打开选项列表" ref={trigger} id={id} type="button" className="mzgui-popup-select-trigger" role="combobox"
      aria-label={label} aria-expanded={expanded} aria-haspopup="listbox" aria-controls={expanded ? listID : undefined}
      aria-activedescendant={expanded && options[active] ? `${listID}-${active}` : undefined}
      aria-busy={busy || undefined} disabled={blocked} onKeyDown={keyDown}
      onClick={() => expanded ? setOpen(false) : show()}>
      <span>{selected?.label ?? value}</span>
      <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><path d="m4 6 4 4 4-4" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" /></svg>
    </button>
    {expanded && <ul ref={list} id={listID} role="listbox" aria-label={label} className="mzgui-popup-select-list"
      data-placement={placement.above ? 'top' : 'bottom'} style={{ maxHeight: placement.height }}
      onPointerDown={(event) => event.preventDefault()}>
      {options.map((option, index) => <li data-click-name="选择列表选项" key={option.value} id={`${listID}-${index}`} role="option"
        aria-selected={option.value === value} data-active={index === active || undefined}
        onPointerMove={() => setActive(index)} onClick={() => choose(index)}>
        <span>{option.label}</span>
        {option.value === value && <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><path d="m3 8 3 3 7-7" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" /></svg>}
      </li>)}
    </ul>}
  </div>
}
