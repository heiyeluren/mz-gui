import { useRef, useState } from 'react'

import { cycleIndex, pageIndex, typeaheadIndex } from './focus'
import { speakVoiceOverSort, voiceOverCountText, voiceOverName, voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'

/** W116-G666：对照旧版 DataTable 再对照。旧版无通用 DataTable 入口。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export type DataTableColumn<K extends string> = {
  key: K
  header: string
  sortable?: boolean
}

export type DataTableProps<K extends string> = {
  caption: string
  columns: DataTableColumn<K>[]
  rows: Array<Record<K, string>>
  sortKey?: K
  sortDirection?: 'asc' | 'desc'
  disabled?: boolean
  busy?: boolean
  onSort?: (key: K) => void
}

export function DataTable<K extends string>({
  caption,
  columns,
  rows,
  sortKey,
  sortDirection = 'asc',
  disabled = false,
  busy = false,
  onSort,
}: DataTableProps<K>) {
  const tableRef = useRef<HTMLTableElement>(null)
  const spokenCaption = voiceOverName(caption)
  const blocked = disabled || busy
  const valuetext = disabled
    ? voiceOverStateLabel('disabled')
    : busy
      ? voiceOverStateLabel('loading')
      : rows.length === 0 ? voiceOverStateLabel('empty') : voiceOverCountText(rows.length)
  const [activeIndex, setActiveIndex] = useState(0)
  const focusRow = (index: number) => {
    if (blocked) {
      return
    }
    setActiveIndex(index)
    queueMicrotask(() => {
      tableRef.current?.querySelector<HTMLElement>(`[data-row-index="${index}"]`)?.focus()
    })
  }
  return (
    <table
      ref={tableRef}
      className="mzgui-data-table"
      aria-roledescription={voiceOverRoleDescription('native-table')}
      aria-busy={busy || undefined}
      aria-disabled={blocked || undefined}
      aria-valuetext={valuetext}
      onKeyDown={(event) => {
        if (blocked || rows.length === 0 || !(event.target instanceof HTMLElement) || !event.target.closest('tbody')) {
          return
        }
        if (event.key === 'ArrowDown') {
          event.preventDefault()
          focusRow(cycleIndex(activeIndex, 1, rows.length))
        } else if (event.key === 'ArrowUp') {
          event.preventDefault()
          focusRow(cycleIndex(activeIndex, -1, rows.length))
        } else if (event.key === 'Home') {
          event.preventDefault()
          focusRow(0)
        } else if (event.key === 'End') {
          event.preventDefault()
          focusRow(rows.length - 1)
        } else if (event.key === 'PageDown') {
          event.preventDefault()
          focusRow(pageIndex(activeIndex, 1, rows.length))
        } else if (event.key === 'PageUp') {
          event.preventDefault()
          focusRow(pageIndex(activeIndex, -1, rows.length))
        } else {
          const first = columns[0]?.key
          const next = first ? typeaheadIndex(rows.map((row) => row[first] ?? ''), activeIndex, event.key) : -1
          if (next >= 0) {
            event.preventDefault()
            focusRow(next)
          }
        }
      }}
    >
      <caption>{spokenCaption}</caption>
      <thead>
        <tr>
          {columns.map((column) => {
            const sorted = sortKey === column.key
            const ariaSort = sorted ? (sortDirection === 'desc' ? 'descending' : 'ascending') : 'none'
            return (
              <th key={column.key} scope="col" aria-sort={column.sortable ? ariaSort : undefined} aria-label={column.sortable ? speakVoiceOverSort(column.header, sorted ? sortDirection : undefined) : undefined}>
                {column.sortable && onSort ? (
                  <button data-click-name="表格排序"
                    type="button"
                    aria-label={speakVoiceOverSort(column.header, sorted ? sortDirection : undefined)}
                    aria-busy={busy || undefined}
                    aria-disabled={blocked || undefined}
                    disabled={blocked}
                    onClick={() => {
                      if (!blocked) {
                        onSort(column.key)
                      }
                    }}
                  >
                    {voiceOverName(column.header)}
                  </button>
                ) : (
                  voiceOverName(column.header)
                )}
              </th>
            )
          })}
        </tr>
      </thead>
      <tbody>
        {rows.length === 0 ? (
          <tr>
            <td colSpan={Math.max(columns.length, 1)}>{voiceOverStateLabel('empty')}</td>
          </tr>
        ) : rows.map((row, index) => (
          <tr key={index} data-row-index={index} tabIndex={index === activeIndex ? 0 : -1} aria-posinset={index + 1} aria-setsize={rows.length} aria-selected={index === activeIndex || undefined}>
            {columns.map((column) => (
              <td key={column.key}>{voiceOverName(row[column.key] ?? '')}</td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  )
}
