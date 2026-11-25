import { useRef } from 'react'

import { cycleIndex, pageIndex, typeaheadIndex } from './focus'
import { voiceOverName, voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'

export type BreadcrumbItem = {
  id: string
  label: string
  href?: string
}

export type BreadcrumbProps = {
  ariaLabel: string
  items: BreadcrumbItem[]
  emptyLabel?: string
  disabled?: boolean
  busy?: boolean
  onSelect?: (id: string) => void
}

export function Breadcrumb({ ariaLabel, items, emptyLabel = voiceOverStateLabel('empty'), disabled = false, busy = false, onSelect }: BreadcrumbProps) {
  const navRef = useRef<HTMLElement>(null)
  const spoken = voiceOverName(ariaLabel)
  const blocked = disabled || busy
  const valuetext = disabled
    ? voiceOverStateLabel('disabled')
    : busy
      ? voiceOverStateLabel('loading')
      : undefined
  if (items.length === 0) {
    return (
      <div className="mzgui-breadcrumb" role="status" aria-label={spoken} aria-roledescription={voiceOverRoleDescription('status')} aria-busy={busy || undefined} aria-disabled={blocked || undefined} aria-valuetext={valuetext ?? emptyLabel}>
        {valuetext ?? emptyLabel}
      </div>
    )
  }
  const focusItem = (id: string) => {
    queueMicrotask(() => {
      navRef.current?.querySelector<HTMLElement>(`[data-crumb-id="${id}"]`)?.focus()
    })
  }
  return (
    <nav
      ref={navRef}
      className="mzgui-breadcrumb"
      aria-label={spoken}
      aria-roledescription={voiceOverRoleDescription('navigation')}
      aria-busy={busy || undefined}
      aria-disabled={blocked || undefined}
      aria-valuetext={valuetext}
      onKeyDown={(event) => {
        if (blocked) {
          return
        }
        const active = document.activeElement instanceof HTMLElement ? document.activeElement.dataset.crumbId : ''
        const current = items.findIndex((item) => item.id === active)
        if (event.key === 'ArrowRight' || event.key === 'ArrowDown') {
          event.preventDefault()
          focusItem(items[cycleIndex(current, 1, items.length)].id)
        } else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') {
          event.preventDefault()
          focusItem(items[cycleIndex(current, -1, items.length)].id)
        } else if (event.key === 'Home') {
          event.preventDefault()
          focusItem(items[0].id)
        } else if (event.key === 'End') {
          event.preventDefault()
          focusItem(items[items.length - 1].id)
        } else if (event.key === 'PageDown' || event.key === 'PageUp') {
          event.preventDefault()
          const next = pageIndex(current, event.key === 'PageDown' ? 1 : -1, items.length)
          if (next >= 0) {
            focusItem(items[next].id)
          }
        } else {
          const next = typeaheadIndex(items.map((item) => item.label), current, event.key)
          if (next >= 0) {
            event.preventDefault()
            focusItem(items[next].id)
          }
        }
      }}
    >
      <ol>
        {items.map((item, index) => {
          const current = index === items.length - 1
          return (
            <li key={item.id}>
              {current ? (
                <span data-crumb-id={item.id} aria-current="page" aria-posinset={index + 1} aria-setsize={items.length} tabIndex={-1}>{voiceOverName(item.label)}</span>
              ) : (
                <button data-click-name="打开上级页面" type="button" data-crumb-id={item.id} aria-label={voiceOverName(item.label)} aria-posinset={index + 1} aria-setsize={items.length} disabled={blocked} aria-disabled={blocked || undefined} onClick={() => {
                  if (!blocked) {
                    onSelect?.(item.id)
                  }
                }}>
                  {voiceOverName(item.label)}
                </button>
              )}
            </li>
          )
        })}
      </ol>
    </nav>
  )
}
