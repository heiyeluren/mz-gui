import { useEffect, useState, type ReactNode } from 'react'

let diagramSequence = 0
let rendererPromise: Promise<typeof import('mermaid')['default']> | undefined

/** 仅遇到图表时加载；安全配置不可被消息中的 Mermaid 指令覆盖。 */
function loadRenderer() {
  if (!rendererPromise) {
    rendererPromise = import('mermaid').then(({ default: mermaid }) => {
      mermaid.initialize({
        startOnLoad: false,
        securityLevel: 'strict',
        suppressErrorRendering: true,
        theme: 'default',
        htmlLabels: false,
        flowchart: { htmlLabels: false, useMaxWidth: false },
        secure: ['secure', 'securityLevel', 'startOnLoad', 'maxTextSize', 'maxEdges', 'suppressErrorRendering', 'htmlLabels', 'flowchart'],
      })
      return mermaid
    }).catch((error: unknown) => {
      rendererPromise = undefined
      throw error
    })
  }
  return rendererPromise
}

export type MermaidDiagramProps = {
  source: string
  fallback: ReactNode
  ariaLabel?: string
}

/** SVG 作为图片展示，不将生成的标记或事件处理器注入应用 DOM。 */
export function MermaidDiagram({ source, fallback, ariaLabel = 'Mermaid 图表' }: MermaidDiagramProps) {
  const [result, setResult] = useState<{ source: string; url?: string; failed?: boolean }>()
  useEffect(() => {
    let cancelled = false
    const render = async () => {
      const mermaid = await loadRenderer()
      if (cancelled) return
      const staging = document.createElement('div')
      staging.dataset.mermaidStaging = ''
      staging.setAttribute('aria-hidden', 'true')
      staging.style.cssText = 'position:absolute;left:-100000px;top:0;width:1200px;pointer-events:none;'
      document.body.append(staging)
      try {
        const { svg } = await mermaid.render(`mzgui-mermaid-${++diagramSequence}`, source, staging)
        if (!cancelled) setResult({ source, url: `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}` })
      } finally {
        staging.remove()
      }
    }
    void render().catch(() => {
      if (!cancelled) setResult({ source, failed: true })
    })
    return () => { cancelled = true }
  }, [source])

  const current = result?.source === source ? result : undefined
  if (current?.failed) return <><div role="alert" style={{ padding: 16, background: '#fff', color: '#9f1239' }}>图表渲染失败，请检查 Mermaid 语法；源码已保留。</div>{fallback}</>
  if (!current?.url) return <div role="status" style={{ padding: 16, background: '#fff', color: '#334155' }}>正在渲染图表…</div>
  return (
    <div style={{ padding: 16, background: '#fff', overflowX: 'auto', textAlign: 'center' }}>
      <img
        src={current.url}
        alt={ariaLabel}
        style={{ display: 'block', maxWidth: '100%', height: 'auto', margin: '0 auto' }}
        onError={() => setResult({ source, failed: true })}
      />
    </div>
  )
}
