import type { Terminal } from '@xterm/xterm'

/** 按 CSS 行高保留应用鼠标模式的像素滚动量，协议编码仍交给 xterm。 */
export function bindTerminalWheel(terminal: Terminal) {
  const element = terminal.element
  const screen = element?.querySelector<HTMLElement>('.xterm-screen')
  if (!element || !screen) return
  let remainder = 0
  let previousMode = ''
  let previousHeight = 0
  let replaying = false
  terminal.attachCustomWheelEventHandler(event => {
    if (replaying) return true
    const mode = terminal.modes.mouseTrackingMode
    // 普通回滚、X10、离散行/页滚轮及修饰键保留原行为。
    if (!['vt200', 'drag', 'any'].includes(mode) || event.deltaMode !== WheelEvent.DOM_DELTA_PIXEL
      || event.ctrlKey || event.altKey || event.metaKey || event.shiftKey || !Number.isFinite(event.deltaY)) {
      remainder = 0
      previousMode = mode
      return true
    }
    const height = screen.getBoundingClientRect().height / terminal.rows
    if (!Number.isFinite(height) || height <= 0) return true
    if (mode !== previousMode || height !== previousHeight || remainder * event.deltaY < 0) remainder = 0
    previousMode = mode
    previousHeight = height
    remainder += event.deltaY * (terminal.options.scrollSensitivity ?? 1) / height
    const lines = Math.trunc(remainder)
    remainder -= lines
    // xterm 6 将小像素缩到 0.3，且每个事件最多编码一次报告。
    // 拆成单行事件以保留幅度，避免自行编码 SGR/DEFAULT 等协议和鼠标坐标。
    replaying = true
    try {
      for (let line = 0; line < Math.abs(lines); line++) {
        element.dispatchEvent(new WheelEvent('wheel', {
          bubbles: true, cancelable: true, deltaMode: WheelEvent.DOM_DELTA_LINE,
          deltaY: Math.sign(lines), clientX: event.clientX, clientY: event.clientY,
        }))
      }
    } finally {
      replaying = false
    }
    event.preventDefault()
    return false
  })
}
