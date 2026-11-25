type Dimensions = { cols: number; rows: number }

interface GeometryPorts {
  dimensions: () => Dimensions | undefined
  resize: (size: Dimensions) => Promise<void>
  apply: (size: Dimensions) => void
  parsing: () => boolean
  closed: () => boolean
  paused: () => boolean
  reconciled: () => void
  failed: () => void
}

const same = (left: Dimensions | null, right: Dimensions | null) =>
  left?.cols === right?.cols && left?.rows === right?.rows

/** 尺寸变更串行确认；同一尺寸合并，输出解析完成后再调整前后台网格。 */
export function terminalGeometry(ports: GeometryPorts) {
  let desired: Dimensions | null = null, confirmed: Dimensions | null = null
  let inFlight = false, disposed = false, failures = 0
  let timer: ReturnType<typeof setTimeout> | undefined
  const state = {
    pending: false,
    revision: 0,
    acceptSnapshot(size: Dimensions) { confirmed = size },
    request() {
      if (disposed || ports.closed() || ports.paused()) return
      const next = ports.dimensions()
      if (!next || next.cols < 20 || next.rows < 5) return
      if (same(next, desired) && (state.pending || failures >= 3)) return
      if (!same(next, desired)) failures = 0
      desired = next
      if (!state.pending && same(next, confirmed)) return
      state.pending = true
      state.revision++
      if (!inFlight) schedule(80)
    },
    dispose() {
      disposed = true
      if (timer !== undefined) clearTimeout(timer)
    },
  }
  function schedule(delay: number) {
    if (timer !== undefined) clearTimeout(timer)
    timer = setTimeout(() => { timer = undefined; void flush() }, delay)
  }
  async function flush() {
    if (disposed || inFlight || !desired) return
    if (ports.paused()) { state.pending = false; state.revision++; return }
    if (ports.parsing()) { schedule(16); return }
    const target = desired
    inFlight = true
    try {
      if (!ports.closed()) {
        await ports.resize(target)
        if (disposed) return
        ports.apply(target)
        confirmed = target
        failures = 0
      }
    } catch {
      if (disposed) return
      failures = same(target, desired) ? failures + 1 : 0
      if (failures >= 3 && !ports.paused()) ports.failed()
    } finally {
      inFlight = false
      if (!disposed) {
        if (ports.paused()) {
          state.pending = false
          state.revision++
        } else if (!ports.closed() && !same(desired, confirmed) && failures < 3) {
          schedule(failures ? 150 * failures : 0)
        } else {
          state.pending = false
          state.revision++
          // 包括失败路径：服务端可能已经应用尺寸但回执丢失，仍需回读其真实画面。
          ports.reconciled()
        }
      }
    }
  }
  return state
}
