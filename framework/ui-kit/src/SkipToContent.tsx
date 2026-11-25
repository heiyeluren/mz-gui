import type { ReactNode } from 'react'

import { voiceOverStateLabel } from './voiceover'

/** W116-G662：对照旧版 SkipToContent 再对照。旧版无入口。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export type SkipToContentProps = {
  href?: string
  label?: string
  disabled?: boolean
  busy?: boolean
}

export function SkipToContent({ href = '#mzgui-main', label = '跳到主要内容', disabled = false, busy = false }: SkipToContentProps) {
  const spoken = label.trim() === '' ? voiceOverStateLabel('empty') : label
  const target = href.trim() === '' ? '#mzgui-main' : href
  const blocked = disabled || busy
  const valuetext = disabled
    ? voiceOverStateLabel('disabled')
    : busy
      ? voiceOverStateLabel('loading')
      : spoken === voiceOverStateLabel('empty') ? spoken : undefined
  return (
    <a
      className="mzgui-skip-to-content"
      href={target}
      aria-disabled={blocked || undefined}
      aria-busy={busy || undefined}
      aria-valuetext={valuetext}
      onClick={(event) => {
        if (blocked) {
          event.preventDefault()
        }
      }}
    >
      {spoken}
    </a>
  )
}

export type MainLandmarkProps = {
  id?: string
  label?: string
  children: ReactNode
  disabled?: boolean
  busy?: boolean
}

export function MainLandmark({ id = 'mzgui-main', label = '主要内容', disabled = false, busy = false, children }: MainLandmarkProps) {
  const spoken = label.trim() === '' ? voiceOverStateLabel('empty') : label
  const blocked = disabled || busy
  const body = typeof children === 'string' && children.trim() === '' ? voiceOverStateLabel('empty') : children
  const valuetext = disabled
    ? voiceOverStateLabel('disabled')
    : busy
      ? voiceOverStateLabel('loading')
      : body === voiceOverStateLabel('empty') ? spoken : undefined
  return (
    <main id={id} aria-label={spoken} aria-disabled={blocked || undefined} aria-busy={busy || undefined} aria-valuetext={valuetext}>
      {body}
    </main>
  )
}
