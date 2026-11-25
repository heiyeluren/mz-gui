import { useState } from 'react'

import { pageIndex, typeaheadIndex } from './focus'
import { voiceOverCountText, voiceOverName, voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'

/** W116-G672：对照旧版 VirtualList 再对照。旧版列表不是独立 ui-kit VirtualList 入口。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export type VirtualListItem = {
  id: string
  label: string
}

export type VirtualListProps = {
  ariaLabel: string
  items: VirtualListItem[]
  selectedID?: string
  height: number
  itemHeight: number
  overscan?: number
  emptyLabel?: string
  disabled?: boolean
  busy?: boolean
  onSelect?: (id: string) => void
}

export function VirtualList({
  ariaLabel,
  items,
  selectedID,
  height,
  itemHeight,
  overscan = 2,
  emptyLabel = voiceOverStateLabel('empty'),
  disabled = false,
  busy = false,
  onSelect,
}: VirtualListProps) {
  const spoken = voiceOverName(ariaLabel)
  const blocked = disabled || busy
  const valuetext = disabled
    ? voiceOverStateLabel('disabled')
    : busy
      ? voiceOverStateLabel('loading')
      : undefined
  const [scrollTop, setScrollTop] = useState(0)
  const rowHeight = itemHeight > 0 ? itemHeight : 32
  const viewport = height > 0 ? height : rowHeight
  const start = Math.max(0, Math.floor(scrollTop / rowHeight) - overscan)
  const visibleCount = Math.ceil(viewport / rowHeight) + overscan * 2
  const end = Math.min(items.length, start + visibleCount)
  const visible = items.slice(start, end)
  const offsetY = start * rowHeight

  const moveSelection = (delta: number) => {
    if (blocked || items.length === 0) {
      return
    }
    const current = items.findIndex((item) => item.id === selectedID)
    const next = Math.min(items.length - 1, Math.max(0, (current < 0 ? 0 : current) + delta))
    onSelect?.(items[next].id)
  }

  if (items.length === 0) {
    return (
      <div className="mzgui-virtual-list" role="status" aria-label={spoken} aria-roledescription={voiceOverRoleDescription('status')} aria-busy={busy || undefined} aria-disabled={blocked || undefined} aria-valuetext={valuetext ?? emptyLabel}>
        {valuetext ?? emptyLabel}
      </div>
    )
  }

  return (
    <div
      className="mzgui-virtual-list"
      role="listbox"
      aria-label={spoken}
      aria-roledescription={voiceOverRoleDescription('listbox')}
      aria-valuetext={valuetext ?? (selectedID ? voiceOverName(items.find((item) => item.id === selectedID)?.label ?? '') : voiceOverCountText(items.length))}
      aria-busy={busy || undefined}
      aria-disabled={blocked || undefined}
      aria-activedescendant={selectedID ? `mzgui-virtual-option-${selectedID}` : undefined}
      tabIndex={0}
      style={{ height: viewport }}
      onScroll={(event) => setScrollTop(event.currentTarget.scrollTop)}
      onKeyDown={(event) => {
        if (blocked) {
          return
        }
        if (event.key === 'ArrowDown') {
          event.preventDefault()
          moveSelection(1)
        } else if (event.key === 'ArrowUp') {
          event.preventDefault()
          moveSelection(-1)
        } else if (event.key === 'Home') {
          event.preventDefault()
          onSelect?.(items[0].id)
        } else if (event.key === 'End') {
          event.preventDefault()
          onSelect?.(items[items.length - 1].id)
        } else if (event.key === 'PageDown' || event.key === 'PageUp') {
          event.preventDefault()
          const current = items.findIndex((item) => item.id === selectedID)
          const next = pageIndex(current, event.key === 'PageDown' ? 1 : -1, items.length, Math.floor(viewport / rowHeight))
          if (next >= 0) {
            onSelect?.(items[next].id)
          }
        } else {
          const selected = items.findIndex((item) => item.id === selectedID)
          const next = typeaheadIndex(items.map((item) => item.label), selected, event.key)
          if (next >= 0) {
            event.preventDefault()
            onSelect?.(items[next].id)
          }
        }
      }}
    >
      <div className="mzgui-virtual-list-spacer" style={{ height: items.length * rowHeight }}>
        <div className="mzgui-virtual-list-window" style={{ transform: `translateY(${offsetY}px)` }}>
          {visible.map((item, offset) => (
            <div data-click-name="选择列表项"
              key={item.id}
              id={`mzgui-virtual-option-${item.id}`}
              role="option"
              aria-label={voiceOverName(item.label)}
              aria-posinset={start + offset + 1}
              aria-setsize={items.length}
              aria-selected={item.id === selectedID}
              style={{ height: rowHeight }}
              onClick={() => {
                if (!blocked) {
                  onSelect?.(item.id)
                }
              }}
            >
              {voiceOverName(item.label)}
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
