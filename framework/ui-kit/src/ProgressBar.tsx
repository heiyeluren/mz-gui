import { voiceOverPercentText, voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'

/** W116-G670：对照旧版 ProgressBar 再对照。旧版无独立 ui-kit ProgressBar 入口。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export type ProgressBarProps = {
  ariaLabel: string
  value: number
  max?: number
  busy?: boolean
}

export function ProgressBar({ ariaLabel, value, max = 100, busy = false }: ProgressBarProps) {
  const finite = Number.isFinite(value) && Number.isFinite(max)
  const ceiling = finite && max > 0 ? max : 100
  const current = finite ? Math.min(Math.max(value, 0), ceiling) : 0
  const spoken = ariaLabel.trim() === '' ? voiceOverStateLabel('empty') : ariaLabel
  return (
    <div
      className="mzgui-progress"
      role="progressbar"
      aria-label={spoken}
      aria-valuemin={0}
      aria-valuemax={ceiling}
      aria-valuenow={busy || !finite ? undefined : current}
      aria-valuetext={busy ? voiceOverStateLabel('loading') : finite ? voiceOverPercentText(current, ceiling) : voiceOverStateLabel('empty')}
      aria-busy={busy || undefined}
      aria-roledescription={voiceOverRoleDescription('progressbar')}
    >
      <div className="mzgui-progress-value" style={{ width: finite ? `${(current / ceiling) * 100}%` : '0%' }} />
    </div>
  )
}
