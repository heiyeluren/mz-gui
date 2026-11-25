import { voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'

/** W116-G671：对照旧版 Switch Checkbox 再对照。旧版设置页有开关。通用 Switch Checkbox 不作为独立产品入口。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export type SwitchProps = {
  id?: string
  label: string
  checked: boolean
  disabled?: boolean
  busy?: boolean
  onChange: (checked: boolean) => void
}

export function Switch({ id, label, checked, disabled = false, busy = false, onChange }: SwitchProps) {
  const switchID = id ?? 'mzgui-switch'
  const spoken = label.trim() === '' ? voiceOverStateLabel('empty') : label
  const blocked = disabled || busy
  const state = disabled ? 'disabled' : busy ? 'loading' : checked ? 'on' : 'off'
  return (
    <div className="mzgui-switch">
      <button data-click-name={checked ? '关闭开关' : '开启开关'}
        id={switchID}
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={spoken}
        aria-roledescription={voiceOverRoleDescription('switch')}
        aria-busy={busy || undefined}
        aria-disabled={blocked || undefined}
        aria-valuetext={voiceOverStateLabel(state)}
        disabled={blocked}
        onClick={() => {
          if (!blocked) {
            onChange(!checked)
          }
        }}
      />
      <label className="mzgui-switch-label" htmlFor={switchID}>{spoken}</label>
    </div>
  )
}
