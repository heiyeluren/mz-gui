import { useEffect, useRef } from 'react'

import { voiceOverName, voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'

/** W117-G706：对照旧版命令面板再对照。旧版无独立命令面板入口。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export type CommandPaletteItem = {
  id: string
  label: string
  disabled?: boolean
}

export type CommandPaletteProps = {
  open: boolean
  query: string
  items: CommandPaletteItem[]
  label?: string
  queryLabel?: string
  emptyLabel?: string
  disabled?: boolean
  busy?: boolean
  onQueryChange: (query: string) => void
  onSelect?: (id: string) => void
  onDismiss: () => void
}

export function CommandPalette({
  open,
  query,
  items,
  label = '命令',
  queryLabel = '筛选',
  emptyLabel = voiceOverStateLabel('empty'),
  disabled = false,
  busy = false,
  onQueryChange,
  onSelect,
  onDismiss,
}: CommandPaletteProps) {
  const queryRef = useRef<HTMLInputElement>(null)
  const blocked = disabled || busy
  const spoken = voiceOverName(label)
  const spokenQuery = voiceOverName(queryLabel)

  useEffect(() => {
    if (!open) {
      return
    }
    queryRef.current?.focus()
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        if (!blocked) {
          onDismiss()
        }
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => {
      window.removeEventListener('keydown', onKeyDown)
    }
  }, [open, blocked, onDismiss])

  if (!open) {
    return null
  }

  const empty = items.length === 0
  const valuetext = disabled
    ? voiceOverStateLabel('disabled')
    : busy
      ? voiceOverStateLabel('loading')
      : empty ? emptyLabel : undefined

  return (
    <div
      className="mzgui-command-palette"
      role="dialog"
      aria-modal="true"
      aria-label={spoken}
      aria-roledescription={voiceOverRoleDescription('dialog')}
      aria-busy={busy || undefined}
      aria-disabled={blocked || undefined}
      aria-valuetext={valuetext}
    >
      <input
        ref={queryRef}
        className="mzgui-command-palette-query"
        role="combobox"
        aria-label={spokenQuery}
        aria-expanded="true"
        aria-autocomplete="list"
        aria-busy={busy || undefined}
        aria-disabled={blocked || undefined}
        disabled={blocked}
        value={query}
        onChange={(event) => {
          if (!blocked) {
            onQueryChange(event.target.value)
          }
        }}
      />
      {empty ? (
        <div className="mzgui-command-palette-empty" role="status">{emptyLabel}</div>
      ) : (
        <ul className="mzgui-command-palette-list" role="listbox" aria-label={spoken}>
          {items.map((item) => (
            <li key={item.id} role="presentation">
              <button data-click-name="执行菜单命令"
                type="button"
                role="option"
                className="mzgui-command-palette-item"
                disabled={item.disabled || blocked}
                aria-disabled={item.disabled || blocked || undefined}
                onClick={() => {
                  if (!item.disabled && !blocked) {
                    onSelect?.(item.id)
                  }
                }}
              >
                {voiceOverName(item.label)}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
