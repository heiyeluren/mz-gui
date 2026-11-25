import { voiceOverName, voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'

/** W116-G664：对照旧版 FormFeedback 再对照。旧版无通用 FormFeedback 控件。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export type TextFieldProps = {
  id: string
  label: string
  value: string
  error?: string
  required?: boolean
  disabled?: boolean
  busy?: boolean
  onChange: (value: string) => void
}

export function TextField({
  id,
  label,
  value,
  error,
  required = false,
  disabled = false,
  busy = false,
  onChange,
}: TextFieldProps) {
  const errorID = `${id}-error`
  const spoken = voiceOverName(label)
  const blocked = disabled || busy
  const valuetext = disabled
    ? voiceOverStateLabel('disabled')
    : busy
      ? voiceOverStateLabel('loading')
      : required
        ? voiceOverStateLabel('required')
        : value.trim() === '' ? voiceOverStateLabel('empty') : undefined
  return (
    <div className="mzgui-text-field">
      <label className="mzgui-text-field-label" htmlFor={id}>{spoken}</label>
      <input
        id={id}
        className="mzgui-text-field-input"
        value={value}
        disabled={blocked}
        required={required}
        aria-label={spoken}
        aria-required={required || undefined}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? errorID : undefined}
        aria-roledescription={voiceOverRoleDescription('textbox')}
        aria-busy={busy || undefined}
        aria-disabled={blocked || undefined}
        aria-valuetext={valuetext}
        onChange={(event) => {
          if (!blocked) {
            onChange(event.target.value)
          }
        }}
      />
      {error ? <p id={errorID} className="mzgui-text-field-error" role="alert" aria-live="assertive">{voiceOverName(error)}</p> : null}
    </div>
  )
}
