import { useRef, useState } from 'react'

import { cycleIndex, pageIndex, typeaheadIndex } from './focus'
import { voiceOverName, voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'

/** W116-G657：对照旧版右键 ContextMenu 再对照。旧版无通用右键入口。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export type ContextMenuItem = {
  id: string
  label: string
  disabled?: boolean
}

export type ContextMenuProps = {
  ariaLabel: string
  items: ContextMenuItem[]
  emptyLabel?: string
  busy?: boolean
  onSelect?: (id: string) => void
}

export function ContextMenu({ ariaLabel, items, emptyLabel = voiceOverStateLabel('empty'), busy = false, onSelect }: ContextMenuProps) {
  const menuRef = useRef<HTMLUListElement>(null)
  const spoken = voiceOverName(ariaLabel)
  const blocked = busy
  const enabled = blocked ? [] : items.filter((item) => !item.disabled)
  const valuetext = busy ? voiceOverStateLabel('loading') : undefined
  if (items.length === 0) {
    return (
      <div className="mzgui-context-menu" role="status" aria-label={spoken} aria-roledescription={voiceOverRoleDescription('status')} aria-busy={busy || undefined} aria-valuetext={valuetext ?? emptyLabel}>
        {valuetext ?? emptyLabel}
      </div>
    )
  }
  const [activeID, setActiveID] = useState(enabled[0]?.id ?? items[0]?.id ?? '')

  const focusItem = (id: string) => {
    if (blocked) {
      return
    }
    setActiveID(id)
    queueMicrotask(() => {
      menuRef.current?.querySelector<HTMLElement>(`[data-menu-id="${id}"]`)?.focus()
    })
  }

  return (
    <ul
      ref={menuRef}
      className="mzgui-context-menu"
      role="menu"
      tabIndex={0}
      aria-label={spoken}
      aria-roledescription={voiceOverRoleDescription('menu')}
      aria-busy={busy || undefined}
      aria-valuetext={valuetext}
      onKeyDown={(event) => {
        if (blocked || enabled.length === 0) {
          return
        }
        const current = enabled.findIndex((item) => item.id === activeID)
        if (event.key === 'ArrowDown') {
          event.preventDefault()
          focusItem(enabled[cycleIndex(current, 1, enabled.length)].id)
        } else if (event.key === 'ArrowUp') {
          event.preventDefault()
          focusItem(enabled[cycleIndex(current, -1, enabled.length)].id)
        } else if (event.key === 'Home') {
          event.preventDefault()
          focusItem(enabled[0].id)
        } else if (event.key === 'End') {
          event.preventDefault()
          focusItem(enabled[enabled.length - 1].id)
        } else if (event.key === 'PageDown' || event.key === 'PageUp') {
          event.preventDefault()
          const next = pageIndex(current, event.key === 'PageDown' ? 1 : -1, enabled.length)
          if (next >= 0) {
            focusItem(enabled[next].id)
          }
        } else if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault()
          if (activeID) {
            onSelect?.(activeID)
          }
        } else {
          const next = typeaheadIndex(enabled.map((item) => item.label), current, event.key)
          if (next >= 0) {
            event.preventDefault()
            focusItem(enabled[next].id)
          }
        }
      }}
    >
      {items.map((item, index) => (
        <li key={item.id} role="none">
          <button data-click-name="选择菜单项"
            type="button"
            role="menuitem"
            data-menu-id={item.id}
            tabIndex={item.id === activeID && !item.disabled && !blocked ? 0 : -1}
            disabled={item.disabled || blocked}
            aria-disabled={item.disabled || blocked || undefined}
            aria-busy={busy || undefined}
            aria-posinset={index + 1}
            aria-setsize={items.length}
            aria-label={voiceOverName(item.label)}
            aria-valuetext={item.disabled ? voiceOverStateLabel('disabled') : valuetext}
            onClick={() => {
              if (!item.disabled && !blocked) {
                setActiveID(item.id)
                onSelect?.(item.id)
              }
            }}
          >
            {voiceOverName(item.label)}
          </button>
        </li>
      ))}
    </ul>
  )
}
