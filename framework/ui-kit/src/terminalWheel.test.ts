import type { Terminal } from '@xterm/xterm'
import { bindTerminalWheel } from './terminalWheel'

describe('terminalWheel', () => {
  it('accumulates pixel deltas into line events without recursive replay', () => {
    const element = document.createElement('div')
    const screen = document.createElement('div')
    screen.className = 'xterm-screen'
    screen.getBoundingClientRect = () => ({ height: 100 }) as DOMRect
    element.appendChild(screen)
    let custom: ((event: WheelEvent) => boolean) | undefined
    const terminal = {
      element,
      rows: 10,
      options: { scrollSensitivity: 1 },
      modes: { mouseTrackingMode: 'vt200' },
      attachCustomWheelEventHandler(handler: (event: WheelEvent) => boolean) { custom = handler },
    } as unknown as Terminal
    bindTerminalWheel(terminal)
    expect(custom).toBeTypeOf('function')
    const dispatched: WheelEvent[] = []
    element.addEventListener('wheel', (event) => {
      if (event.deltaMode === WheelEvent.DOM_DELTA_LINE) {
        dispatched.push(event)
        custom?.(event)
      }
    })
    const first = new WheelEvent('wheel', { deltaMode: WheelEvent.DOM_DELTA_PIXEL, deltaY: 4, cancelable: true })
    expect(custom!(first)).toBe(false)
    expect(first.defaultPrevented).toBe(true)
    expect(dispatched).toHaveLength(0)
    const second = new WheelEvent('wheel', { deltaMode: WheelEvent.DOM_DELTA_PIXEL, deltaY: 6, cancelable: true })
    expect(custom!(second)).toBe(false)
    expect(dispatched).toHaveLength(1)
    expect(dispatched[0].deltaY).toBe(1)
  })

  it('preserves native behavior for unsupported modes and modifier keys', () => {
    const element = document.createElement('div')
    const screen = document.createElement('div')
    screen.className = 'xterm-screen'
    element.appendChild(screen)
    let custom: ((event: WheelEvent) => boolean) | undefined
    const terminal = {
      element,
      rows: 10,
      options: {},
      modes: { mouseTrackingMode: 'none' },
      attachCustomWheelEventHandler(handler: (event: WheelEvent) => boolean) { custom = handler },
    } as unknown as Terminal
    bindTerminalWheel(terminal)
    const event = new WheelEvent('wheel', { deltaMode: WheelEvent.DOM_DELTA_PIXEL, deltaY: 20, ctrlKey: true, cancelable: true })
    expect(custom!(event)).toBe(true)
    expect(event.defaultPrevented).toBe(false)
  })

  it('does nothing before the terminal element exists', () => {
    expect(() => bindTerminalWheel({ attachCustomWheelEventHandler: vi.fn() } as unknown as Terminal)).not.toThrow()
  })
})
