import { useEffect, useRef, type ReactNode } from 'react'
import { createPortal } from 'react-dom'

import { rememberFocus, trapFocus } from './focus'
import { voiceOverName, voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'

/** W116-G665：对照旧版通用 Dialog 再对照。旧版无通用 Dialog 入口。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export type DialogProps = {
  open: boolean
  title: string
  dismissLabel?: string
  disabled?: boolean
  busy?: boolean
  onDismiss: () => void
  children: ReactNode
}

export function Dialog({ open, title, dismissLabel = '关闭', disabled = false, busy = false, onDismiss, children }: DialogProps) {
  const dialogRef = useRef<HTMLDivElement>(null)
  const blocked = disabled || busy

  useEffect(() => {
    if (!open) {
      return
    }
    const previouslyFocused = rememberFocus()
    dialogRef.current?.focus()
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        if (!blocked) {
          onDismiss()
        }
        return
      }
      trapFocus(event, dialogRef.current)
    }
    window.addEventListener('keydown', onKeyDown)
    return () => {
      window.removeEventListener('keydown', onKeyDown)
      previouslyFocused?.focus()
    }
  }, [open, blocked, onDismiss])

  if (!open) {
    return null
  }

  const titleID = 'mzgui-dialog-title'
  const bodyID = 'mzgui-dialog-body'
  const spokenTitle = title.trim() === '' ? voiceOverStateLabel('empty') : title
  const spokenBody = typeof children === 'string' && children.trim() === '' ? voiceOverStateLabel('empty') : children
  const empty = spokenTitle === voiceOverStateLabel('empty') || spokenBody === voiceOverStateLabel('empty')
  const valuetext = disabled
    ? voiceOverStateLabel('disabled')
    : busy
      ? voiceOverStateLabel('loading')
      : empty ? voiceOverStateLabel('empty') : undefined
  return createPortal(
    <div className="mzgui-dialog-backdrop">
      <div ref={dialogRef} className="mzgui-dialog" role="dialog" aria-modal="true" aria-labelledby={titleID} aria-describedby={bodyID} aria-roledescription={voiceOverRoleDescription('dialog')} aria-busy={busy || undefined} aria-disabled={blocked || undefined} aria-valuetext={valuetext} tabIndex={-1}>
        <div className="mzgui-dialog-header">
          <h2 id={titleID} className="mzgui-dialog-title">{spokenTitle}</h2>
          <button data-click-name="关闭对话框" type="button" className="mzgui-dialog-dismiss" aria-label={voiceOverName(dismissLabel)} aria-disabled={blocked || undefined} disabled={blocked} onClick={() => {
            if (!blocked) {
              onDismiss()
            }
          }}>
            {voiceOverName(dismissLabel)}
          </button>
        </div>
        <div id={bodyID} className="mzgui-dialog-body">{spokenBody}</div>
      </div>
    </div>,
    document.body,
  )
}
