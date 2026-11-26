import { act, render, screen, waitFor } from '@testing-library/react'
import { MermaidDiagram } from './MermaidDiagram'

const renderer = vi.hoisted(() => ({
  render: vi.fn(),
  initialize: vi.fn(),
}))

vi.mock('mermaid', () => ({
  default: {
    initialize: renderer.initialize,
    render: renderer.render,
  },
}))

describe('MermaidDiagram', () => {
  beforeEach(() => {
    renderer.render.mockReset()
    renderer.initialize.mockReset()
    renderer.render.mockResolvedValue({ svg: '<svg xmlns="http://www.w3.org/2000/svg"><text>safe</text></svg>' })
  })

  it('renders SVG as an image and cleans staging nodes', async () => {
    render(<MermaidDiagram source="graph TD; A-->B" ariaLabel="流程" fallback={<span>fallback</span>} />)
    const image = await screen.findByRole('img', { name: '流程' }) as HTMLImageElement
    expect(image.src).toContain('data:image/svg+xml')
    expect(document.querySelector('[data-mermaid-staging]')).toBeNull()
    expect(document.querySelector('iframe')).toBeNull()
    expect(renderer.initialize).toHaveBeenCalledWith(expect.objectContaining({ securityLevel: 'strict', htmlLabels: false }))
  })

  it('shows an alert and preserves fallback on render errors', async () => {
    renderer.render.mockRejectedValue(new Error('invalid source'))
    render(<MermaidDiagram source="not mermaid" fallback={<span>源码保留</span>} />)
    expect(await screen.findByRole('alert')).toHaveTextContent('图表渲染失败')
    expect(screen.getByText('源码保留')).toBeInTheDocument()
  })

  it('cancels stale rendering and releases staging after unmount', async () => {
    let resolveRender: ((value: { svg: string }) => void) | undefined
    renderer.render.mockImplementation(() => new Promise((resolve) => { resolveRender = resolve }))
    const view = render(<MermaidDiagram source="graph TD; A-->B" fallback={null} />)
    view.unmount()
    await act(async () => { resolveRender?.({ svg: '<svg />' }) })
    expect(document.querySelector('[data-mermaid-staging]')).toBeNull()
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
  })

  it('falls back when the rendered image cannot load', async () => {
    render(<MermaidDiagram source="graph TD; A-->B" fallback={<span>降级内容</span>} />)
    const image = await screen.findByRole('img')
    await act(async () => { image.dispatchEvent(new Event('error', { bubbles: true })) })
    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument())
    expect(screen.getByText('降级内容')).toBeInTheDocument()
  })
})
