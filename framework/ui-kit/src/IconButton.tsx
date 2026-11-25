import type { ReactNode } from 'react'

import { voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'

/** W116-G680：对照旧版 IconButton 再对照。旧版无独立 ui-kit IconButton 入口。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export type IconButtonProps = {
  ariaLabel: string
  disabled?: boolean
  busy?: boolean
  children: ReactNode
  onPress: () => void
}

export function IconButton({ ariaLabel, disabled = false, busy = false, children, onPress }: IconButtonProps) {
  const spoken = ariaLabel.trim() === '' ? voiceOverStateLabel('empty') : ariaLabel
  const blocked = disabled || busy
  const valuetext = disabled
    ? voiceOverStateLabel('disabled')
    : busy
      ? voiceOverStateLabel('loading')
      : spoken === voiceOverStateLabel('empty') ? spoken : undefined
  return <button data-click-name="图标按钮"
    type="button"
    className="mzgui-icon-button"
    aria-label={spoken}
    aria-roledescription={voiceOverRoleDescription('button')}
    aria-busy={busy || undefined}
    aria-disabled={blocked || undefined}
    aria-valuetext={valuetext}
    disabled={blocked}
    onClick={() => {
      if (!blocked) {
        onPress()
      }
    }}
  >{children}</button>
}
