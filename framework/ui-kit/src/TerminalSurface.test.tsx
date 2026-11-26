import { act, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { TerminalSurface } from './TerminalSurface'

const terminal = vi.hoisted(() => ({
  write: vi.fn(),
  reset: vi.fn(),
  dispose: vi.fn(),
  fit: vi.fn(),
  focus: vi.fn(),
  loadAddon: vi.fn(),
  options: null as null | Record<string, unknown>,
  input: null as null | ((data: string) => void),
}))

vi.mock('@xterm/xterm', () => ({ Terminal: class {
  cols = 120
  rows = 36
  resize(cols: number, rows: number) { this.cols = cols; this.rows = rows }
  constructor(options: Record<string, unknown>) { terminal.options = options }
  get options() { return terminal.options ?? {} }
  set options(value: Record<string, unknown>) { terminal.options = { ...terminal.options, ...value } }
  loadAddon = terminal.loadAddon
  open() {}
  textarea = Object.assign(document.createElement('textarea'), { focus: terminal.focus })
  write = terminal.write
  reset = terminal.reset
  dispose = terminal.dispose
  onData(callback: (data: string) => void) {
    terminal.input = callback
    return { dispose() {} }
  }
} }))
vi.mock('@xterm/addon-fit', () => ({ FitAddon: class { fit = terminal.fit; proposeDimensions() { return { cols: 120, rows: 36 } } } }))

const session = { session_id: 'terminal-1', state: 'exited' }
const running = { ...session, state: 'running' }
const idleSnapshot = () => vi.fn().mockResolvedValue({ session: running, frames: [], next_cursor: 0, truncated: false, has_more: false })

describe('TerminalSurface', () => {
  let resize: ResizeObserverCallback | undefined

  beforeEach(() => {
    vi.clearAllMocks()
    terminal.write.mockImplementation((_data: string, parsed?: () => void) => parsed?.())
    terminal.input = null
    terminal.options = null
    resize = undefined
    globalThis.ResizeObserver = class {
      constructor(callback: ResizeObserverCallback) { resize = callback }
      observe() {}
      unobserve() {}
      disconnect() {}
    }
  })

  it('focuses only for new explicit requests', () => {
    const ports = { getSnapshot: idleSnapshot(), writeInput: vi.fn(), resizeSession: vi.fn(), onSession: vi.fn() }
    const view = render(<TerminalSurface ready sessionID="terminal-1" {...ports} />)
    expect(terminal.focus).not.toHaveBeenCalled()
    view.rerender(<TerminalSurface ready sessionID="terminal-1" {...ports} focusRequest={1} />)
    expect(terminal.focus).toHaveBeenCalledExactlyOnceWith({ preventScroll: true })
    view.rerender(<TerminalSurface ready sessionID="terminal-1" {...ports} focusRequest={1} fontSize={15} />)
    view.rerender(<TerminalSurface ready sessionID="terminal-1" {...ports} focusRequest={1} busy />)
    view.rerender(<TerminalSurface ready sessionID="terminal-1" {...ports} focusRequest={1} />)
    expect(terminal.focus).toHaveBeenCalledTimes(1)
    view.rerender(<TerminalSurface ready sessionID="terminal-1" {...ports} focusRequest={2} />)
    expect(terminal.focus).toHaveBeenCalledTimes(2)
    expect(terminal.dispose).not.toHaveBeenCalled()
  })

  it('changes appearance without rebuilding the terminal or losing input ownership', async () => {
    const writeInput = vi.fn().mockResolvedValue({ session: running })
    const ports = { getSnapshot: idleSnapshot(), writeInput, resizeSession: vi.fn().mockResolvedValue({ session }), onSession: vi.fn() }
    const theme = { background: '#f7f8fb', foreground: '#252b37', cursor: '#6848c4' }
    const view = render(<TerminalSurface ready sessionID="terminal-1" {...ports} theme={theme} fontSize={14} />)
    expect(terminal.options).toMatchObject({ theme, fontSize: 14, minimumContrastRatio: 4.5 })
    terminal.reset.mockClear()
    view.rerender(<TerminalSurface ready sessionID="terminal-1" {...ports} theme={{ ...theme, background: '#fffdf8' }} fontSize={16} />)
    expect(terminal.options).toMatchObject({ theme: { background: '#fffdf8' }, fontSize: 16 })
    expect(terminal.dispose).not.toHaveBeenCalled()
    expect(terminal.reset).not.toHaveBeenCalled()
    terminal.input?.('hello\r')
    await waitFor(() => expect(writeInput).toHaveBeenCalledWith('terminal-1', 'hello\r'))
    view.unmount()
  })

  it('reads output by cursor and returns the current session', async () => {
    const getSnapshot = vi.fn().mockResolvedValue({ session, frames: [{ data: 'ready\r\n' }], next_cursor: 1, truncated: false, has_more: false })
    const onSession = vi.fn()
    const view = render(<TerminalSurface ready sessionID="terminal-1" getSnapshot={getSnapshot} writeInput={vi.fn()} resizeSession={vi.fn()} onSession={onSession} />)
    await waitFor(() => expect(terminal.write).toHaveBeenCalledWith('ready\r\n', expect.any(Function)))
    expect(getSnapshot).toHaveBeenCalledWith('terminal-1', 0)
    expect(onSession).toHaveBeenCalledWith(session)
    view.unmount()
    expect(terminal.dispose).toHaveBeenCalled()
  })

  it('waits for the current output page before reading the next page', async () => {
    let parsed: (() => void) | undefined
    terminal.write.mockImplementation((_data: string, callback?: () => void) => { parsed = callback })
    const getSnapshot = vi.fn()
      .mockResolvedValueOnce({ session: running, frames: [{ data: 'x'.repeat(256 * 1024) }], next_cursor: 1, truncated: false, has_more: true })
      .mockResolvedValue({ session, frames: [], next_cursor: 1, truncated: false, has_more: false })
    const view = render(<TerminalSurface ready sessionID="terminal-1" getSnapshot={getSnapshot} writeInput={vi.fn()} resizeSession={vi.fn()} onSession={vi.fn()} />)
    await waitFor(() => expect(terminal.write).toHaveBeenCalledOnce())
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 100)) })
    expect(getSnapshot).toHaveBeenCalledTimes(1)
    expect(parsed).toBeTypeOf('function')
    await act(async () => parsed?.())
    await waitFor(() => expect(getSnapshot).toHaveBeenCalledTimes(2))
    expect(getSnapshot).toHaveBeenLastCalledWith('terminal-1', 1)
    view.unmount()
    await act(async () => parsed?.())
    expect(getSnapshot).toHaveBeenCalledTimes(2)
  })

  it('keeps pending input assigned to the original session during a fast switch', async () => {
    const write = vi.fn(async (id: string) => ({ session: { session_id: id, state: 'running' } }))
    const get = vi.fn(async (id: string) => ({ session: { session_id: id, state: 'running' }, frames: [], next_cursor: 0, truncated: false, has_more: false }))
    const view = render(<TerminalSurface ready sessionID="A" getSnapshot={get} writeInput={write} resizeSession={vi.fn()} onSession={vi.fn()} />)
    await waitFor(() => expect(terminal.input).not.toBeNull())
    terminal.input?.('命令A\r')
    view.rerender(<TerminalSurface ready sessionID="B" getSnapshot={get} writeInput={write} resizeSession={vi.fn()} onSession={vi.fn()} />)
    terminal.input?.('命令B\r')
    await waitFor(() => expect(write).toHaveBeenCalledWith('A', '命令A\r'))
    await waitFor(() => expect(write).toHaveBeenCalledWith('B', '命令B\r'))
    expect(write).toHaveBeenCalledTimes(2)
  })

  it('merges short input bursts and synchronizes dimensions', async () => {
    const writeInput = vi.fn().mockResolvedValue({ session: running })
    const resizeSession = vi.fn().mockResolvedValue({ session: running })
    const onDimensionsChange = vi.fn()
    render(<TerminalSurface ready sessionID="terminal-1" getSnapshot={idleSnapshot()} writeInput={writeInput} resizeSession={resizeSession} onSession={vi.fn()} onDimensionsChange={onDimensionsChange} />)
    await waitFor(() => expect(terminal.input).not.toBeNull())
    terminal.input?.('p')
    terminal.input?.('wd\r')
    await waitFor(() => expect(writeInput).toHaveBeenCalledWith('terminal-1', 'pwd\r'))
    await waitFor(() => expect(onDimensionsChange).toHaveBeenCalledWith(120, 36))
    resize?.([], {} as ResizeObserver)
    await waitFor(() => expect(resizeSession).toHaveBeenCalledWith('terminal-1', 120, 36))
  })

  it('keeps local reflow for exited terminals without sending resize requests', async () => {
    const resizeSession = vi.fn(), onSession = vi.fn()
    const getSnapshot = vi.fn().mockResolvedValue({ session, frames: [], next_cursor: 0, truncated: false, has_more: false })
    const view = render(<TerminalSurface ready sessionID="terminal-1" getSnapshot={getSnapshot} writeInput={vi.fn()} resizeSession={resizeSession} onSession={onSession} />)
    await waitFor(() => expect(onSession).toHaveBeenCalledWith(session))
    terminal.fit.mockClear()
    resize?.([], {} as ResizeObserver)
    expect(terminal.fit).toHaveBeenCalledOnce()
    expect(resizeSession).not.toHaveBeenCalled()
    view.unmount()
  })

  it('renders an empty status', async () => {
    render(<TerminalSurface ready sessionID="terminal-1" getSnapshot={idleSnapshot()} writeInput={vi.fn()} resizeSession={vi.fn()} onSession={vi.fn()} />)
    await waitFor(() => expect(screen.getByRole('application', { name: '终端输出' })).toHaveAttribute('aria-valuetext', '空'))
    expect(screen.getByRole('status')).toHaveTextContent('空')
  })

  it('blocks input in disabled and busy states', async () => {
    const writeInput = vi.fn().mockResolvedValue({ session })
    const { rerender } = render(<TerminalSurface ready sessionID="terminal-1" disabled getSnapshot={idleSnapshot()} writeInput={writeInput} resizeSession={vi.fn()} onSession={vi.fn()} />)
    await waitFor(() => expect(screen.getByRole('application', { name: '终端输出' })).toHaveAttribute('aria-valuetext', '不可用'))
    await waitFor(() => expect(terminal.input).not.toBeNull())
    terminal.input?.('pwd\r')
    await new Promise(resolve => setTimeout(resolve, 40))
    expect(writeInput).not.toHaveBeenCalled()
    rerender(<TerminalSurface ready sessionID="terminal-1" busy getSnapshot={idleSnapshot()} writeInput={writeInput} resizeSession={vi.fn()} onSession={vi.fn()} />)
    await waitFor(() => expect(screen.getByRole('application', { name: '终端输出' })).toHaveAttribute('aria-valuetext', '正在加载'))
    terminal.input?.('pwd\r')
    await new Promise(resolve => setTimeout(resolve, 40))
    expect(writeInput).not.toHaveBeenCalled()
  })

  it('shows truncation separately and clears it on session switch', async () => {
    const output = '\x1b[2J\x1b[1;1H最近内容'
    const getSnapshot = vi.fn(async (id: string) => ({ session: { ...session, session_id: id }, frames: [{ data: output }], next_cursor: 5000, truncated: id === 'terminal-1', has_more: false }))
    const view = render(<TerminalSurface ready sessionID="terminal-1" getSnapshot={getSnapshot} writeInput={vi.fn()} resizeSession={vi.fn()} onSession={vi.fn()} />)
    await waitFor(() => expect(terminal.write).toHaveBeenCalledWith(output, expect.any(Function)))
    expect(screen.getByText('较早终端输出已超出保留范围，当前显示最近内容。')).toBeVisible()
    expect(terminal.write.mock.calls.every(([data]) => !data.includes('缓冲区清理'))).toBe(true)
    view.rerender(<TerminalSurface ready sessionID="terminal-2" getSnapshot={getSnapshot} writeInput={vi.fn()} resizeSession={vi.fn()} onSession={vi.fn()} />)
    await waitFor(() => expect(screen.queryByText('较早终端输出已超出保留范围，当前显示最近内容。')).not.toBeInTheDocument())
  })
})
