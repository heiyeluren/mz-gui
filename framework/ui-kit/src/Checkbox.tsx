import { useEffect, useRef } from 'react'

import { voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'

/** W116-G671：对照旧版 Switch Checkbox 再对照。旧版设置页有开关。通用 Switch Checkbox 不作为独立产品入口。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export type CheckboxProps = {
  id?: string
  label: string
  checked: boolean
  disabled?: boolean
  busy?: boolean
  indeterminate?: boolean
  onChange: (checked: boolean) => void
}

export function Checkbox({ id, label, checked, disabled = false, busy = false, indeterminate = false, onChange }: CheckboxProps) {
  const checkboxID = id ?? 'mzgui-checkbox'
  const inputRef = useRef<HTMLInputElement>(null)
  const spoken = label.trim() === '' ? voiceOverStateLabel('empty') : label
  const blocked = disabled || busy
  const state = disabled ? 'disabled' : busy ? 'loading' : indeterminate ? 'mixed' : checked ? 'checked' : 'unchecked'
  useEffect(() => {
    if (inputRef.current) {
      inputRef.current.indeterminate = indeterminate
    }
  }, [indeterminate])
  return (
    <label className="mzgui-checkbox" htmlFor={checkboxID}>
      <input
        ref={inputRef}
        id={checkboxID}
        type="checkbox"
        checked={checked}
        disabled={blocked}
        aria-label={spoken}
        aria-checked={indeterminate ? 'mixed' : undefined}
        aria-roledescription={voiceOverRoleDescription('checkbox')}
        aria-busy={busy || undefined}
        aria-disabled={blocked || undefined}
        aria-valuetext={voiceOverStateLabel(state)}
        onChange={(event) => {
          if (!blocked) {
            onChange(event.target.checked)
          }
        }}
      />
      <span className="mzgui-checkbox-label">{spoken}</span>
    </label>
  )
}
