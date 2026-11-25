import { voiceOverName, voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'

/** W116-G668：对照旧版 ImageViewer 再对照。旧版无独立 ImageViewer 入口。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export type ImageViewerProps = {
  ariaLabel: string
  src: string
  emptyLabel?: string
  disabled?: boolean
  busy?: boolean
}

export function allowImageSrc(src: string): boolean {
  const value = src.trim()
  if (value === '' || value.toLowerCase().includes('javascript:')) {
    return false
  }
  if (value.startsWith('https://')) {
    return true
  }
  return /^data:image\/(?:png|jpeg|jpg|gif|webp);base64,/i.test(value)
}

export function ImageViewer({ ariaLabel, src, emptyLabel = voiceOverStateLabel('empty'), disabled = false, busy = false }: ImageViewerProps) {
  const spoken = voiceOverName(ariaLabel)
  const blocked = disabled || busy
  const valuetext = disabled
    ? voiceOverStateLabel('disabled')
    : busy
      ? voiceOverStateLabel('loading')
      : undefined
  if (!allowImageSrc(src)) {
    return (
      <div className="mzgui-image-viewer" role="status" aria-label={spoken} aria-roledescription={voiceOverRoleDescription('status')} aria-busy={busy || undefined} aria-disabled={blocked || undefined} aria-valuetext={valuetext ?? emptyLabel}>
        {valuetext ?? emptyLabel}
      </div>
    )
  }
  return (
    <figure className="mzgui-image-viewer">
      <img src={src} alt={spoken} role="img" aria-roledescription={voiceOverRoleDescription('img')} aria-busy={busy || undefined} aria-disabled={blocked || undefined} aria-valuetext={valuetext} loading="lazy" />
    </figure>
  )
}
