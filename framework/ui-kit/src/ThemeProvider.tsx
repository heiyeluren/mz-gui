import { useEffect, type ReactNode } from 'react'

import { speakTextDirection, speakTheme, textDirection } from './dir'
import { applyThemeSnapshot, createThemeSnapshot, type ThemeMode } from './theme'
import { voiceOverStateLabel } from './voiceover'

export type ThemeProviderProps = {
  mode: ThemeMode
  revision?: number
  prefersDark?: boolean
  locale?: string
  disabled?: boolean
  busy?: boolean
  children: ReactNode
}

function applyThemeSpeech(target: HTMLElement, resolved: string, direction: string, disabled: boolean, busy: boolean) {
  const valuetext = disabled
    ? voiceOverStateLabel('disabled')
    : busy
      ? voiceOverStateLabel('loading')
      : `${speakTheme(resolved)}，${speakTextDirection(direction)}`
  target.setAttribute('aria-valuetext', valuetext)
  if (disabled || busy) {
    target.setAttribute('aria-disabled', 'true')
  } else {
    target.removeAttribute('aria-disabled')
  }
  if (busy) {
    target.setAttribute('aria-busy', 'true')
  } else {
    target.removeAttribute('aria-busy')
  }
}

export function ThemeProvider({ mode, revision = 1, prefersDark = false, locale = 'zh-CN', disabled = false, busy = false, children }: ThemeProviderProps) {
  const snapshot = createThemeSnapshot(mode, revision, prefersDark)
  const direction = textDirection(locale)
  applyThemeSnapshot(document.documentElement, snapshot)
  document.documentElement.dir = direction
  applyThemeSpeech(document.documentElement, snapshot.resolved, direction, disabled, busy)

  useEffect(() => {
    applyThemeSnapshot(document.documentElement, snapshot)
    document.documentElement.dir = direction
    applyThemeSpeech(document.documentElement, snapshot.resolved, direction, disabled, busy)
  }, [snapshot.mode, snapshot.resolved, snapshot.revision, locale, direction, disabled, busy])

  return <>{children}</>
}
