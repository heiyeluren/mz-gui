import { useEffect, type ReactNode } from 'react'

import { matchShortcut, type ShortcutBinding, type ShortcutPlatform } from './shortcut'
import { voiceOverName, voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'

export type ShortcutListenerProps = {
  bindings: ShortcutBinding[]
  platform: ShortcutPlatform
  scope: string
  ariaLabel?: string
  disabled?: boolean
  busy?: boolean
  onCommand: (commandType: string, binding: ShortcutBinding) => void
  children?: ReactNode
}

export function ShortcutListener({ bindings, platform, scope, ariaLabel = '快捷键', disabled = false, busy = false, onCommand, children }: ShortcutListenerProps) {
  const blocked = disabled || busy
  const spoken = voiceOverName(ariaLabel)
  const valuetext = disabled
    ? voiceOverStateLabel('disabled')
    : busy
      ? voiceOverStateLabel('loading')
      : undefined
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (blocked) {
        return
      }
      const matched = matchShortcut(event, bindings, platform, scope)
      if (!matched) {
        return
      }
      event.preventDefault()
      onCommand(matched.commandType, matched)
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [bindings, platform, scope, blocked, onCommand])

  return (
    <div
      role="group"
      aria-label={spoken}
      aria-roledescription={voiceOverRoleDescription('group')}
      aria-disabled={blocked || undefined}
      aria-busy={busy || undefined}
      aria-valuetext={valuetext}
    >
      {children}
    </div>
  )
}
