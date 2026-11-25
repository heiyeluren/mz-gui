import { useEffect, useRef, useState, type ReactNode } from 'react'

import { cycleIndex, pageIndex, typeaheadIndex } from './focus'
import { voiceOverName, voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'

export interface ModuleRailChoice {
  id: string
  label: string
  description?: string
  icon?: ReactNode
  disabled?: boolean
}

export interface ModuleRailItem {
  id: string
  label: string
  icon: ReactNode
  tooltip?: string
  disabled?: boolean
  badge?: string
  choices?: ModuleRailChoice[]
}

export interface ModuleRailGroup {
  items: ModuleRailItem[]
}

export interface ModuleRailProps {
  ariaLabel: string
  activeID: string
  groups: ModuleRailGroup[]
  footerItems?: ModuleRailItem[]
  emptyLabel?: string
  disabled?: boolean
  busy?: boolean
  onActivate?: (item: ModuleRailItem | ModuleRailChoice) => void
  onSelect: (id: string) => void
}

function flattenRailItems(groups: ModuleRailGroup[], footerItems: ModuleRailItem[]): ModuleRailItem[] {
  return [...groups.flatMap((group) => group.items), ...footerItems]
}

function RailItem({ item, activeID, expanded, tabIndex, posinset, setsize, blocked, onExpand, onLeave, onActivate, onSelect }: {
  item: ModuleRailItem
  activeID: string
  expanded: boolean
  tabIndex: number
  posinset: number
  setsize: number
  blocked: boolean
  onExpand: (id: string) => void
  onLeave: (id: string) => void
  onActivate?: (item: ModuleRailItem | ModuleRailChoice) => void
  onSelect: (id: string) => void
}) {
  const hasChoices = Boolean(item.choices?.length)
  const active = item.id === activeID || Boolean(item.choices?.some((choice) => choice.id === activeID))
  const spoken = voiceOverName(item.label)
  const itemBlocked = blocked || Boolean(item.disabled)
  return <div className="mzgui-module-rail-entry"
    onMouseEnter={() => hasChoices && !itemBlocked && onExpand(item.id)}
    onMouseLeave={() => expanded && onLeave(item.id)}>
    <button data-click-name="选择模块"
      type="button"
      className={`mzgui-module-rail-item ${active ? 'active' : ''}`}
      data-rail-id={item.id}
      tabIndex={itemBlocked ? -1 : tabIndex}
      disabled={itemBlocked}
      onClick={() => {
        if (itemBlocked) {
          return
        }
        onActivate?.(item)
        if (hasChoices) {
          onExpand(item.id)
          return
        }
        onExpand('')
        onSelect(item.id)
      }}
      title={item.tooltip ?? spoken}
      aria-label={spoken}
      aria-posinset={posinset}
      aria-setsize={setsize}
      aria-current={active ? 'page' : undefined}
      aria-haspopup={hasChoices ? 'menu' : undefined}
      aria-expanded={hasChoices ? expanded : undefined}
      aria-disabled={itemBlocked || undefined}
      aria-valuetext={item.disabled ? voiceOverStateLabel('disabled') : undefined}
    >
      <span className="mzgui-module-rail-icon" aria-hidden="true">{item.icon}</span>
      <span className="mzgui-module-rail-label">{spoken}</span>
      {item.badge && <span className="mzgui-module-rail-badge" aria-hidden="true">{item.badge}</span>}
    </button>
    {hasChoices && expanded && <div className="mzgui-module-rail-menu" role="menu" aria-label={`选择${spoken}模块`}>
      {item.choices!.map((choice, index) => <button data-click-name="选择子模块" key={choice.id} type="button" role="menuitem" disabled={choice.disabled}
        className={choice.id === activeID ? 'active' : ''}
        aria-posinset={index + 1}
        aria-setsize={item.choices!.length}
        aria-label={voiceOverName(choice.label)}
        onClick={() => {
          if (blocked || choice.disabled) {
            return
          }
          onActivate?.(choice)
          onSelect(choice.id)
          onExpand('')
        }}>
        {choice.icon && <span aria-hidden="true">{choice.icon}</span>}
        <span><strong>{voiceOverName(choice.label)}</strong>{choice.description && <small>{choice.description}</small>}</span>
      </button>)}
    </div>}
  </div>
}

export function ModuleRail({ ariaLabel, activeID, groups, footerItems = [], emptyLabel = voiceOverStateLabel('empty'), disabled = false, busy = false, onActivate, onSelect }: ModuleRailProps) {
  const [expandedID, setExpandedID] = useState('')
  const railRef = useRef<HTMLElement>(null)
  const closeTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const cancelClose = () => {
    if (closeTimerRef.current !== null) {
      clearTimeout(closeTimerRef.current)
      closeTimerRef.current = null
    }
  }
  const openExpanded = (id: string) => {
    cancelClose()
    setExpandedID(id)
  }
  const closeAfterLeave = (id: string) => {
    cancelClose()
    // 给鼠标跨越入口与菜单间隙留出时间；移入任一处即取消关闭。
    closeTimerRef.current = setTimeout(() => {
      closeTimerRef.current = null
      setExpandedID((current) => current === id ? '' : current)
    }, 150)
  }
  const blocked = disabled || busy
  const valuetext = disabled
    ? voiceOverStateLabel('disabled')
    : busy
      ? voiceOverStateLabel('loading')
      : undefined

  useEffect(() => {
    const closeOutside = (event: PointerEvent) => {
      if (!railRef.current?.contains(event.target as Node)) openExpanded('')
    }
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') openExpanded('')
    }
    document.addEventListener('pointerdown', closeOutside)
    document.addEventListener('keydown', closeOnEscape)
    return () => {
      cancelClose()
      document.removeEventListener('pointerdown', closeOutside)
      document.removeEventListener('keydown', closeOnEscape)
    }
  }, [])

  const items = flattenRailItems(groups, footerItems)
  const spoken = voiceOverName(ariaLabel)
  if (items.length === 0) {
    return <div className="mzgui-module-rail" role="status" aria-label={spoken} aria-roledescription={voiceOverRoleDescription('status')} aria-busy={busy || undefined} aria-disabled={blocked || undefined} aria-valuetext={valuetext ?? emptyLabel}>{valuetext ?? emptyLabel}</div>
  }
  const enabled = blocked ? [] : items.filter((item) => !item.disabled)
  const focusID = items.some((item) => item.id === activeID && !item.disabled)
    ? activeID
    : (enabled[0]?.id ?? '')
  const focusItem = (id: string) => {
    queueMicrotask(() => {
      railRef.current?.querySelector<HTMLElement>(`[data-rail-id="${id}"]`)?.focus()
    })
  }
  const railItem = (item: ModuleRailItem) => {
    const posinset = items.findIndex((entry) => entry.id === item.id) + 1
    return <RailItem key={item.id} item={item} activeID={activeID}
      expanded={!blocked && expandedID === item.id} tabIndex={item.id === focusID ? 0 : -1}
      posinset={posinset} setsize={items.length} blocked={blocked} onExpand={openExpanded} onLeave={closeAfterLeave} onActivate={onActivate} onSelect={onSelect} />
  }

  return <nav
    ref={railRef}
    className="mzgui-module-rail"
    aria-label={spoken}
    aria-roledescription={voiceOverRoleDescription('navigation')}
    aria-busy={busy || undefined}
    aria-disabled={blocked || undefined}
    aria-valuetext={valuetext}
    onKeyDown={(event) => {
      if (blocked || enabled.length === 0) {
        return
      }
      const current = enabled.findIndex((item) => item.id === (document.activeElement instanceof HTMLElement ? document.activeElement.dataset.railId : focusID))
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
      } else if (event.key === 'ArrowRight') {
        const item = enabled[Math.max(0, current)]
        if (item?.choices?.length) {
          event.preventDefault()
          openExpanded(item.id)
        }
      } else if (event.key === 'ArrowLeft') {
        event.preventDefault()
        openExpanded('')
      } else if (event.key === 'PageDown' || event.key === 'PageUp') {
        event.preventDefault()
        const next = pageIndex(current, event.key === 'PageDown' ? 1 : -1, enabled.length)
        if (next >= 0) {
          focusItem(enabled[next].id)
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
    {groups.map((group, groupIndex) => <div className="mzgui-module-rail-group" key={groupIndex}>
      {groupIndex > 0 && <div className="mzgui-module-rail-divider" aria-hidden="true" />}
      {group.items.map(railItem)}
    </div>)}
    {footerItems.length > 0 && <>
      <div className="mzgui-module-rail-spacer" />
      <div className="mzgui-module-rail-divider" aria-hidden="true" />
      <div className="mzgui-module-rail-group mzgui-module-rail-footer">
        {footerItems.map(railItem)}
      </div>
    </>}
  </nav>
}
