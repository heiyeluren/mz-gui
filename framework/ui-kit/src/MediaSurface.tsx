import { voiceOverName, voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'

/** W116-G679：对照旧版 MediaSurface 再对照。旧版无独立 MediaSurface 入口。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export type MediaKind = 'audio' | 'video'

export type MediaSurfaceProps = {
  ariaLabel: string
  src: string
  kind?: MediaKind
  emptyLabel?: string
  disabled?: boolean
  busy?: boolean
}

export function allowMediaSrc(src: string): boolean {
  const value = src.trim()
  if (value === '' || value.toLowerCase().includes('javascript:') || value.startsWith('data:')) {
    return false
  }
  return value.startsWith('https://')
}

export function cancelMedia(element: HTMLMediaElement | null): void {
  if (!element) {
    return
  }
  element.pause()
  element.removeAttribute('src')
  element.load()
}

export function MediaSurface({ ariaLabel, src, kind = 'video', emptyLabel = voiceOverStateLabel('empty'), disabled = false, busy = false }: MediaSurfaceProps) {
  const spoken = voiceOverName(ariaLabel)
  const blocked = disabled || busy
  const valuetext = disabled
    ? voiceOverStateLabel('disabled')
    : busy
      ? voiceOverStateLabel('loading')
      : undefined
  if (!allowMediaSrc(src)) {
    return (
      <div className="mzgui-media-surface" role="status" aria-label={spoken} aria-roledescription={voiceOverRoleDescription('status')} aria-busy={busy || undefined} aria-disabled={blocked || undefined} aria-valuetext={valuetext ?? emptyLabel}>
        {valuetext ?? emptyLabel}
      </div>
    )
  }
  const surfaceProps = {
    className: 'mzgui-media-surface',
    src,
    'aria-label': spoken,
    'aria-roledescription': voiceOverRoleDescription('status'),
    'aria-busy': busy || undefined,
    'aria-disabled': blocked || undefined,
    'aria-valuetext': valuetext,
    controls: blocked ? undefined : true,
    preload: 'none' as const,
  }
  if (kind === 'audio') {
    return <audio {...surfaceProps} />
  }
  return <video {...surfaceProps} />
}
