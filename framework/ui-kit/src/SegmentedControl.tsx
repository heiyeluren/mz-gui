import { useRef } from 'react'

import { cycleIndex, pageIndex, typeaheadIndex } from './focus'
import { voiceOverName, voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'

/** W116-G658：对照旧版 SegmentedControl / Tabs i18n 再对照。旧版无 i18n catalog，字号小中大硬编码。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export interface SegmentedControlOption<T extends string> {
  value: T
  label: string
  ariaLabel?: string
}

export interface SegmentedControlProps<T extends string> {
  ariaLabel: string
  value: T
  options: SegmentedControlOption<T>[]
  emptyLabel?: string
  disabled?: boolean
  busy?: boolean
  onChange: (value: T) => void
}

export function SegmentedControl<T extends string>({ ariaLabel, value, options, emptyLabel = voiceOverStateLabel('empty'), disabled = false, busy = false, onChange }: SegmentedControlProps<T>) {
  const groupRef = useRef<HTMLDivElement>(null)
  const spoken = ariaLabel.trim() === '' ? voiceOverStateLabel('empty') : ariaLabel
  const blocked = disabled || busy
  const valuetext = disabled
    ? voiceOverStateLabel('disabled')
    : busy
      ? voiceOverStateLabel('loading')
      : undefined
  if (options.length === 0) {
    return <div className="mzgui-segmented-control" role="status" aria-label={spoken} aria-roledescription={voiceOverRoleDescription('status')} aria-busy={busy || undefined} aria-disabled={blocked || undefined} aria-valuetext={valuetext ?? emptyLabel}>{valuetext ?? emptyLabel}</div>
  }
  const activate = (next: T) => {
    if (blocked) {
      return
    }
    onChange(next)
    queueMicrotask(() => {
      groupRef.current?.querySelector<HTMLElement>(`[data-segment-value="${next}"]`)?.focus()
    })
  }
  return <div
    ref={groupRef}
    className="mzgui-segmented-control"
    role="group"
    aria-label={spoken}
    aria-roledescription={voiceOverRoleDescription('group')}
    aria-busy={busy || undefined}
    aria-disabled={blocked || undefined}
    aria-valuetext={valuetext}
    onKeyDown={(event) => {
      if (blocked || options.length === 0) {
        return
      }
      const current = options.findIndex((option) => option.value === value)
      if (event.key === 'ArrowRight' || event.key === 'ArrowDown') {
        event.preventDefault()
        activate(options[cycleIndex(current, 1, options.length)].value)
      } else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') {
        event.preventDefault()
        activate(options[cycleIndex(current, -1, options.length)].value)
      } else if (event.key === 'Home') {
        event.preventDefault()
        activate(options[0].value)
      } else if (event.key === 'End') {
        event.preventDefault()
        activate(options[options.length - 1].value)
      } else if (event.key === 'PageDown' || event.key === 'PageUp') {
        event.preventDefault()
        const next = pageIndex(current, event.key === 'PageDown' ? 1 : -1, options.length)
        if (next >= 0) {
          activate(options[next].value)
        }
      } else {
        const next = typeaheadIndex(options.map((option) => option.label), current, event.key)
        if (next >= 0) {
          event.preventDefault()
          activate(options[next].value)
        }
      }
    }}
  >
    {options.map((option, index) => <button data-click-name="切换选项"
      key={option.value}
      type="button"
      data-segment-value={option.value}
      tabIndex={option.value === value ? 0 : -1}
      aria-label={voiceOverName(option.ariaLabel ?? option.label)}
      aria-posinset={index + 1}
      aria-setsize={options.length}
      aria-pressed={option.value === value}
      aria-busy={busy || undefined}
      aria-disabled={blocked || undefined}
      disabled={blocked}
      className={option.value === value ? 'active' : ''}
      onClick={() => {
        if (!blocked) {
          onChange(option.value)
        }
      }}
    >{voiceOverName(option.label)}</button>)}
  </div>
}
