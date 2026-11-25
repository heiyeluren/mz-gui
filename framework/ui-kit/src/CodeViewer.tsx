import { voiceOverName, voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'

/** W116-G667：对照旧版 CodeViewer 再对照。旧版无独立 CodeViewer 入口。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export type CodeViewerProps = {
  ariaLabel: string
  code: string
  language?: string
  emptyLabel?: string
  disabled?: boolean
  busy?: boolean
}

export function CodeViewer({ ariaLabel, code, language = 'text', emptyLabel = voiceOverStateLabel('empty'), disabled = false, busy = false }: CodeViewerProps) {
  const spoken = voiceOverName(ariaLabel)
  const blocked = disabled || busy
  const valuetext = disabled
    ? voiceOverStateLabel('disabled')
    : busy
      ? voiceOverStateLabel('loading')
      : undefined
  if (code.trim() === '') {
    return (
      <section className="mzgui-code-viewer" role="status" aria-label={spoken} aria-roledescription={voiceOverRoleDescription('status')} aria-busy={busy || undefined} aria-disabled={blocked || undefined} aria-valuetext={valuetext ?? emptyLabel} data-language={language}>
        {valuetext ?? emptyLabel}
      </section>
    )
  }
  return (
    <section className="mzgui-code-viewer" role="region" aria-label={spoken} aria-roledescription={voiceOverRoleDescription('region')} aria-busy={busy || undefined} aria-disabled={blocked || undefined} aria-valuetext={valuetext ?? `语言 ${voiceOverName(language)}`} data-language={language}>
      <pre>
        <code>{code}</code>
      </pre>
    </section>
  )
}
