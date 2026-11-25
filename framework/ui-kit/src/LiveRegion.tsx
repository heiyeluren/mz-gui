import { voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'

/** W116-G678：对照旧版 LiveRegion 不含 VoiceOver 诚实。旧版无独立 ui-kit LiveRegion 入口。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export type LiveRegionProps = {
  message: string
  politeness?: 'polite' | 'assertive'
  disabled?: boolean
  busy?: boolean
}

export function LiveRegion({ message, politeness = 'polite', disabled = false, busy = false }: LiveRegionProps) {
  const spoken = message.trim() === '' ? voiceOverStateLabel('empty') : message
  const blocked = disabled || busy
  const valuetext = disabled
    ? voiceOverStateLabel('disabled')
    : busy
      ? voiceOverStateLabel('loading')
      : spoken
  return (
    <div className="mzgui-live-region" role="status" aria-live={politeness} aria-atomic="true" aria-roledescription={voiceOverRoleDescription('status')} aria-busy={busy || undefined} aria-disabled={blocked || undefined} aria-valuetext={valuetext}>
      {spoken}
    </div>
  )
}
