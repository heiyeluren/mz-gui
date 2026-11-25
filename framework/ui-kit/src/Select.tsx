import { voiceOverName, voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'

/** W116-G682：对照旧版 Select 再对照。旧版无独立 ui-kit Select 入口。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export type SelectOption<T extends string> = {
  value: T
  label: string
}

export type SelectProps<T extends string> = {
  id?: string
  label: string
  value: T
  options: SelectOption<T>[]
  disabled?: boolean
  busy?: boolean
  onChange: (value: T) => void
}

export function Select<T extends string>({ id, label, value, options, disabled = false, busy = false, onChange }: SelectProps<T>) {
  const selectID = id ?? 'mzgui-select'
  const selected = options.find((option) => option.value === value)
  const spoken = label.trim() === '' ? voiceOverStateLabel('empty') : label
  const blocked = disabled || busy
  const valuetext = disabled
    ? voiceOverStateLabel('disabled')
    : busy
      ? voiceOverStateLabel('loading')
      : selected ? voiceOverName(selected.label) : voiceOverStateLabel('empty')
  return (
    <label className="mzgui-select" htmlFor={selectID}>
      <span className="mzgui-select-label">{spoken}</span>
      <select
        id={selectID}
        value={value}
        disabled={blocked}
        aria-label={spoken}
        aria-roledescription={voiceOverRoleDescription('native-select')}
        aria-busy={busy || undefined}
        aria-disabled={blocked || undefined}
        aria-valuetext={valuetext}
        onChange={(event) => {
          if (!blocked) {
            onChange(event.target.value as T)
          }
        }}
      >
        {options.map((option, index) => (
          <option key={option.value} value={option.value} aria-posinset={index + 1} aria-setsize={options.length}>
            {voiceOverName(option.label)}
          </option>
        ))}
      </select>
    </label>
  )
}
