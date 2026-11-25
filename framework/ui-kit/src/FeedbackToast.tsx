import { voiceOverName, voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'

/** W116-G664：对照旧版 FormFeedback 再对照。旧版无通用 FormFeedback 控件。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
/** W117-G702：对照旧版 Toast 通知条再对照。旧版无独立 Toast 入口。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export type FeedbackTone = 'info' | 'success' | 'warning' | 'error'

export type FeedbackToastProps = {
  tone: FeedbackTone
  message: string
  dismissLabel?: string
  disabled?: boolean
  busy?: boolean
  onDismiss?: () => void
}

export function FeedbackToast({ tone, message, dismissLabel = '关闭', disabled = false, busy = false, onDismiss }: FeedbackToastProps) {
  const role = tone === 'error' || tone === 'warning' ? 'alert' : 'status'
  const blocked = disabled || busy
  const spoken = message.trim() === '' ? voiceOverStateLabel('empty') : message
  const valuetext = disabled
    ? voiceOverStateLabel('disabled')
    : busy
      ? voiceOverStateLabel('loading')
      : spoken
  return (
    <div className={`mzgui-feedback-toast mzgui-feedback-toast-${tone}`} role={role} aria-roledescription={voiceOverRoleDescription(role)} aria-busy={busy || undefined} aria-disabled={blocked || undefined} aria-valuetext={valuetext} data-tone={tone}>
      <span className="mzgui-feedback-toast-message">{spoken}</span>
      {onDismiss ? (
        <button data-click-name="关闭提示" type="button" className="mzgui-feedback-toast-dismiss" aria-label={voiceOverName(dismissLabel)} aria-disabled={blocked || undefined} disabled={blocked} onClick={() => {
          if (!blocked) {
            onDismiss()
          }
        }}>
          {voiceOverName(dismissLabel)}
        </button>
      ) : null}
    </div>
  )
}
