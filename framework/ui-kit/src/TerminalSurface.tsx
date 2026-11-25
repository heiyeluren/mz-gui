import { useEffect, useRef, useState } from 'react'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal, type ITheme } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'

import { voiceOverName, voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'
import { bindTerminalIme } from './terminalIme'
import { bindTerminalWheel } from './terminalWheel'
import { terminalGeometry } from './terminalGeometry'
import { terminalState } from '../../go/runtime/terminal/xterm/assets/state.js'

/** W116-G673：对照旧版 TerminalSurface 再对照。旧版无独立 ui-kit TerminalSurface 入口。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export interface TerminalSurfaceSession {
  session_id: string
  state: string
  cols?: number
  rows?: number
}

export interface TerminalSurfaceSnapshot<Session extends TerminalSurfaceSession> {
  session: Session
  screen_state?: string
  screen_managed?: boolean
  screen_cursor?: number
  frames?: Array<{ data: string }> | null
  next_cursor: number
  truncated: boolean
  has_more: boolean
}

export interface TerminalSurfaceMutation<Session extends TerminalSurfaceSession> {
  session: Session
}

export interface TerminalSurfaceProps<Session extends TerminalSurfaceSession> {
  ready: boolean
  sessionID: string
  className?: string
  ariaLabel?: string
  showOutputTruncatedNotice?: boolean
  disabled?: boolean
  busy?: boolean
  /** 显式聚焦请求序号；更新后聚焦输入区，保持祖先容器的滚动位置。 */
  focusRequest?: number
  /** 调用方可配置外观；省略时保留原终端默认样式。 */
  theme?: ITheme
  fontSize?: number
  getSnapshot: (sessionID: string, cursor: number) => Promise<TerminalSurfaceSnapshot<Session>>
  writeInput: (sessionID: string, data: string) => Promise<TerminalSurfaceMutation<Session>>
  resizeSession: (sessionID: string, cols: number, rows: number) => Promise<TerminalSurfaceMutation<Session>>
  onSession: (session: Session) => void
  onError?: (message: string) => void
  onDimensionsChange?: (cols: number, rows: number) => void
}

const terminalStates = new Set(['closed', 'exited', 'interrupted'])
// 交互回显通常晚于输入确认；短时按帧读取，避免首次空结果直接退避到 100ms。
const interactionReadInterval = 16
const interactionReadWindow = 250
// 与后端一致，仅识别完整设备属性、RGB 颜色及焦点报告；混合正文仍按用户输入处理。
const terminalReplyPattern = /^(?:\x1b\[\??[0-9]{1,5};[0-9]{1,5}R|\x1b\[\??[0-9]{1,5}n|\x1bP[01]\$r[^\x1b\x00-\x1f]{0,128}\x1b\\|\x1b\[[IO]|\x1b\[[?>][0-9]{1,5}(?:;[0-9]{1,5}){0,16}c|\x1b\](?:10|11|12);rgb:[0-9a-fA-F]{1,4}\/[0-9a-fA-F]{1,4}\/[0-9a-fA-F]{1,4}(?:\x07|\x1b\\))+$/
const terminalReplyOnly = (data: string) => data.length > 0 && data.length <= 2048 && terminalReplyPattern.exec(data)?.[0] === data

// DOM 渲染器每次滚动都重建整屏 span，大量 ANSI 输出下在 WKWebView 明显掉帧；有 WebGL2 时改用 GPU 渲染。
export const webglRendererSupported = () => typeof WebGL2RenderingContext !== 'undefined'

type WebglRendererAddon = { onContextLoss: (listener: () => void) => { dispose: () => void }; dispose: () => void }
type WebglRendererLoader = () => Promise<{ WebglAddon: new () => WebglRendererAddon }>
let loadWebglRenderer: WebglRendererLoader = () => import('@xterm/addon-webgl')

/** 仅供测试替换 WebGL addon 加载方式；生产始终走动态导入，避免把渲染器塞进首屏包。 */
export function setWebglRendererLoaderForTest(loader: WebglRendererLoader | null) {
  loadWebglRenderer = loader ?? (() => import('@xterm/addon-webgl'))
}

// 上下文丢失或初始化失败时回退 xterm 自带 DOM 渲染器，终端仍可用，只是滚动性能下降。
async function attachWebglRenderer(terminal: Terminal, isDisposed: () => boolean) {
  if (!webglRendererSupported()) return null
  try {
    const { WebglAddon } = await loadWebglRenderer()
    if (isDisposed()) return null
    const addon = new WebglAddon()
    const lost = addon.onContextLoss(() => { lost.dispose(); addon.dispose() })
    terminal.loadAddon(addon as unknown as Parameters<Terminal['loadAddon']>[0])
    return addon
  } catch {
    return null
  }
}

function validSession<Session extends TerminalSurfaceSession>(value: unknown): value is Session {
  if (!value || typeof value !== 'object') return false
  const session = value as Partial<TerminalSurfaceSession>
  return typeof session.session_id === 'string' && session.session_id.length > 0
    && typeof session.state === 'string' && session.state.length > 0
}

// TerminalSurface 只负责跨应用可复用的终端渲染与流式交互，不感知业务会话来源。
export function TerminalSurface<Session extends TerminalSurfaceSession>({
  ready,
  sessionID,
  className = '',
  ariaLabel = '终端输出',
  showOutputTruncatedNotice = true,
  disabled = false,
  busy = false,
  focusRequest,
  theme,
  fontSize = 13,
  getSnapshot,
  writeInput,
  resizeSession,
  onSession,
  onError = () => undefined,
  onDimensionsChange = () => undefined,
}: TerminalSurfaceProps<Session>) {
  const hostRef = useRef<HTMLDivElement>(null)
  const terminalRef = useRef<Terminal | null>(null)
  const focusedRequest = useRef<{ sessionID: string; request: number } | null>(null)
  const appearanceRef = useRef({ theme, fontSize })
  appearanceRef.current = { theme, fontSize }
  const sessionRef = useRef(sessionID)
  const closedSessionRef = useRef('')
  const restoringRef = useRef(false)
  const managedScreenRef = useRef(false)
  const parsingOutputRef = useRef(false)
  const restoredCursorRef = useRef<number | null>(null)
  const geometryRef = useRef<ReturnType<typeof terminalGeometry> | null>(null)
  const syncSizeRef = useRef<(() => void) | null>(null)
  const cursorRef = useRef(0)
  const inputRef = useRef('')
  const inputEpochRef = useRef(0)
  const inputOwnerRef = useRef<{id:string; write:typeof writeInput; error:typeof onError} | null>(null)
  const inputOperationRef = useRef<Promise<boolean> | null>(null)
  const inputTimerRef = useRef<number | null>(null)
  const inputAttemptRef = useRef('')
  const inputInFlightRef = useRef(false)
  const inputRetryCountRef = useRef(0)
  const wakeOutputRef = useRef<((interactive: boolean) => void) | null>(null)
  const callbacksRef = useRef({ getSnapshot, writeInput, resizeSession, onSession, onError, onDimensionsChange })
  const [status, setStatus] = useState('')
  const [outputTruncated, setOutputTruncated] = useState(false)
  const blocked = disabled || busy
  const blockedRef = useRef(blocked)
  blockedRef.current = blocked
  const previousBlocked = useRef(blocked)
  const valuetext = disabled
    ? voiceOverStateLabel('disabled')
    : busy
      ? voiceOverStateLabel('loading')
      : (status || undefined)

  // 同一会话更新端口函数时保留在途解析与游标，避免释放尚未解析完的输出页。
  callbacksRef.current = { getSnapshot, writeInput, resizeSession, onSession, onError, onDimensionsChange }

  useEffect(() => {
    inputEpochRef.current += 1
    inputRef.current = ''; inputAttemptRef.current = ''; inputInFlightRef.current = false; inputRetryCountRef.current = 0
    inputOwnerRef.current = null; inputOperationRef.current = null; inputTimerRef.current = null
    sessionRef.current = sessionID
    closedSessionRef.current = ''
    cursorRef.current = 0
    setOutputTruncated(false)
    restoringRef.current = false
    parsingOutputRef.current = false
    managedScreenRef.current = false
    restoredCursorRef.current = null
    terminalRef.current?.reset()
  }, [sessionID, ready])

  useEffect(() => {
    if (!ready || !hostRef.current) return
    const terminal = new Terminal({
      convertEol: false,
      cursorBlink: true,
      fontFamily: 'SFMono-Regular, Consolas, "Liberation Mono", monospace',
      fontSize: appearanceRef.current.fontSize,
      lineHeight: 1.25,
      // CLI 可能显式输出黑字或真彩色；按实际背景提升对比度，保持浅色背景上的深色字。
      minimumContrastRatio: 4.5,
      scrollback: 10000,
      // CLI 开启鼠标上报后，macOS 仍可用 Option 拖选并复制；其他平台保留 Shift 拖选。
      macOptionClickForcesSelection: true,
      // 鼠标滚轮每格按行跳动，短暂插值让滚动连续；触控板增量小，不受影响。
      smoothScrollDuration: 60,
      theme: { ...terminalState.defaultTheme, selectionBackground: '#315b57', ...appearanceRef.current.theme },
    })
    const fit = new FitAddon()
    terminal.loadAddon(fit)
    terminal.open(hostRef.current)
    bindTerminalWheel(terminal)
    const core = (terminal as Terminal & { _core?: { _keyDownSeen?: boolean; coreService?: { onUserInput?: (listener: () => void) => { dispose(): void } } } })._core
    const ime = bindTerminalIme(terminal.textarea, undefined, core)
    fit.fit()
    terminalRef.current = terminal
    focusedRequest.current = null
    callbacksRef.current.onDimensionsChange(terminal.cols, terminal.rows)

    let disposed = false
    let webglRenderer: WebglRendererAddon | null = null
    void attachWebglRenderer(terminal, () => disposed).then((addon) => {
      if (disposed) { addon?.dispose(); return }
      webglRenderer = addon
    })
    const scheduleInput = (delay: number) => {
      if (disposed || inputTimerRef.current !== null) return
      inputTimerRef.current = window.setTimeout(flushInput, delay)
    }
    const discardClosedReply = (id: string, data: string) => {
      if (sessionRef.current !== id || closedSessionRef.current !== id || !terminalReplyOnly(data)) return false
      inputRef.current = inputRef.current.slice(data.length)
      inputAttemptRef.current = ''
      inputRetryCountRef.current = 0
      return true
    }
    const flushInput = () => {
      inputTimerRef.current = null
      if (disposed || inputInFlightRef.current) return
      if (restoringRef.current) { scheduleInput(10); return }
      const currentSessionID = sessionRef.current
      const data = inputAttemptRef.current || inputRef.current
      if (!currentSessionID || !data) return
      if (discardClosedReply(currentSessionID, data)) {
        if (inputRef.current) scheduleInput(0)
        return
      }
      inputAttemptRef.current = data
      inputInFlightRef.current = true
      const epoch = inputEpochRef.current
      inputOperationRef.current = callbacksRef.current.writeInput(currentSessionID, data)
        .then((result) => {
          const applied = validSession<Session>(result?.session) && result.session.session_id === currentSessionID
          if (disposed || epoch !== inputEpochRef.current) return applied
          if (validSession<Session>(result?.session) && result.session.session_id === currentSessionID) {
            if (terminalStates.has(result.session.state)) closedSessionRef.current = currentSessionID
            if (sessionRef.current === currentSessionID) inputRef.current = inputRef.current.slice(data.length)
            inputAttemptRef.current = ''
            inputRetryCountRef.current = 0
            setStatus('')
            callbacksRef.current.onSession(result.session)
            wakeOutputRef.current?.(!terminalReplyOnly(data))
            if (inputRef.current) scheduleInput(0)
            return true
          }
          setStatus('终端输入响应无效')
          callbacksRef.current.onError('终端输入响应无效')
          if (inputRetryCountRef.current < 3) {
            inputRetryCountRef.current += 1
            scheduleInput(80 * inputRetryCountRef.current)
          }
          return false
        })
        .catch(() => {
          if (disposed || epoch !== inputEpochRef.current) return false
          if (discardClosedReply(currentSessionID, data)) return true
          setStatus('终端输入发送失败')
          callbacksRef.current.onError('终端输入发送失败')
          if (inputRetryCountRef.current < 3) {
            inputRetryCountRef.current += 1
            scheduleInput(80 * inputRetryCountRef.current)
          }
          return false
        })
        .finally(() => {
          if (disposed || epoch !== inputEpochRef.current) return
          inputInFlightRef.current = false
          if (inputRef.current && inputTimerRef.current === null && inputRetryCountRef.current === 0) scheduleInput(0)
        })
    }
    let userInputEvent = false
    // 绑定同版本 xterm 的输入来源事件：真实键入/粘贴与解析器设备应答严格区分。
    const provenance = core?.coreService?.onUserInput?.(() => { userInputEvent = true })
    const inputDisposable = terminal.onData((data) => {
      const fromUser = userInputEvent
      userInputEvent = false
      if (blockedRef.current || closedSessionRef.current === sessionRef.current) {
        return
      }
      const rendering = restoringRef.current || (managedScreenRef.current && parsingOutputRef.current)
      const focusOrMouse = /^(?:\x1b\[[IO]|\x1b\[<\d+;\d+;\d+[Mm])$/.test(data)
      if (rendering && !fromUser && !focusOrMouse && (provenance || terminalReplyOnly(data))) return
      const committed = ime.filter(data)
      if (!committed) return
      if (!inputRef.current) inputOwnerRef.current = {id:sessionRef.current,write:callbacksRef.current.writeInput,error:callbacksRef.current.onError}
      inputRef.current += committed
      if (!inputInFlightRef.current && inputRetryCountRef.current >= 3) inputRetryCountRef.current = 0
      scheduleInput(12)
    })
    const geometry = terminalGeometry({
      dimensions: () => fit.proposeDimensions(),
      parsing: () => parsingOutputRef.current || restoringRef.current,
      closed: () => closedSessionRef.current === sessionID,
      paused: () => blockedRef.current,
      resize: async ({ cols, rows }) => {
        const result = await callbacksRef.current.resizeSession(sessionID, cols, rows)
        if (disposed) return
        if (!validSession<Session>(result?.session) || result.session.session_id !== sessionID) throw new Error('终端尺寸响应无效')
        if (terminalStates.has(result.session.state)) closedSessionRef.current = sessionID
        callbacksRef.current.onSession(result.session)
      },
      apply: ({ cols, rows }) => {
        terminal.resize(cols, rows)
        callbacksRef.current.onDimensionsChange(cols, rows)
      },
      reconciled: () => {
        if (managedScreenRef.current) {
          cursorRef.current = 0
          restoredCursorRef.current = null
        }
        wakeOutputRef.current?.(true)
      },
      failed: () => callbacksRef.current.onError('终端尺寸同步失败，请调整大小后重试'),
    })
    geometryRef.current = geometry
    const syncSize = () => {
      if (closedSessionRef.current === sessionID) {
        // 已结束的画面没有后续控制序列，保留纯本地重排能力。
        fit.fit()
        callbacksRef.current.onDimensionsChange(terminal.cols, terminal.rows)
      } else geometry.request()
    }
    const observer = new ResizeObserver(syncSize)
    syncSizeRef.current = syncSize
    observer.observe(hostRef.current)
    return () => {
      // 页面/会话切换前已键入但尚未合并发送的内容仍归原会话，绝不改投新会话。
      const owner = inputOwnerRef.current
      const pending = inputRef.current
      const attempted = inputAttemptRef.current
      const inFlight = inputInFlightRef.current
      const completion = inputOperationRef.current
      const unsent = inFlight ? pending.slice(attempted.length) : attempted ? '' : pending
      if (owner && unsent && !(closedSessionRef.current === owner.id && terminalReplyOnly(unsent))) {
        const send = async (confirmed: boolean) => {
          if (!confirmed) { owner.error('原终端输入未确认'); return }
          try { await owner.write(owner.id, unsent) } catch { if (!terminalReplyOnly(unsent)) owner.error('原终端输入未确认') }
        }
        if (inFlight && completion) void completion.then(send)
        else void send(true)
      }
      disposed = true
      if (inputTimerRef.current !== null) window.clearTimeout(inputTimerRef.current)
      geometry.dispose()
      if (geometryRef.current === geometry) geometryRef.current = null
      observer.disconnect()
      if (syncSizeRef.current === syncSize) syncSizeRef.current = null
      inputDisposable.dispose()
      provenance?.dispose()
      ime.dispose()
      webglRenderer?.dispose()
      terminal.dispose()
      terminalRef.current = null
    }
  }, [ready, sessionID])

  useEffect(() => {
    if (previousBlocked.current && !blocked) syncSizeRef.current?.()
    previousBlocked.current = blocked
  }, [blocked])

  useEffect(() => {
    if (focusRequest === undefined) {
      focusedRequest.current = null
      return
    }
    if (!ready || !sessionID || blocked || !terminalRef.current?.textarea) return
    if (focusedRequest.current?.sessionID === sessionID && focusedRequest.current.request === focusRequest) return
    terminalRef.current.textarea.focus({ preventScroll: true })
    focusedRequest.current = { sessionID, request: focusRequest }
  }, [ready, sessionID, focusRequest, blocked])

  useEffect(() => {
    const terminal = terminalRef.current
    if (!ready || !terminal) return
    // 外观变化不重建会话、不重放输出，也不改变输入归属。
    const previousFontSize = terminal.options?.fontSize ?? fontSize
    terminal.options = {
      theme: { ...terminalState.defaultTheme, selectionBackground: '#315b57', ...theme },
      fontSize,
    }
    if (previousFontSize !== fontSize) syncSizeRef.current?.()
  }, [theme, fontSize, ready, sessionID])

  useEffect(() => {
    if (!ready || !sessionID || !terminalRef.current) return
    let active = true
    let timer: number | null = null
    let inFlight = false
    let wakeRequested = false
    let idleReads = 0
    let interactiveUntil = 0
    let releaseWrite: (() => void) | null = null
    const schedule = (delay: number) => {
      if (timer !== null) window.clearTimeout(timer)
      timer = window.setTimeout(poll, delay)
    }
    const wake = (interactive: boolean) => {
      if (!active) return
      if (interactive) interactiveUntil = performance.now() + interactionReadWindow
      idleReads = 0
      if (inFlight) wakeRequested = true
      else schedule(0)
    }
    wakeOutputRef.current = wake
    const poll = async () => {
      if (!active || inFlight) return
      timer = null
      inFlight = true
      let nextDelay: number | undefined
      const geometryRevision = geometryRef.current?.revision
      try {
        if (geometryRef.current?.pending) { nextDelay = interactionReadInterval; return }
        const snapshot = await callbacksRef.current.getSnapshot(sessionID, cursorRef.current)
        if (!active || sessionRef.current !== sessionID) return
        // 旧尺寸产生的 ANSI 增量不能进入新网格；确认尺寸后从后台恢复点继续。
        if (geometryRef.current?.revision !== geometryRevision) { nextDelay = 0; return }
        if (!snapshot || !validSession<Session>(snapshot.session) || snapshot.session.session_id !== sessionID) {
          throw new Error('终端快照响应无效')
        }
        const nextCursor = snapshot.screen_state ? (snapshot.screen_cursor ?? snapshot.next_cursor) : snapshot.next_cursor
        const hasMore = !snapshot.screen_state && snapshot.has_more
        managedScreenRef.current = Boolean(snapshot.screen_managed)
        if (!snapshot.screen_state && Number.isInteger(snapshot.session.cols) && Number.isInteger(snapshot.session.rows)) {
          geometryRef.current?.acceptSnapshot({ cols: snapshot.session.cols!, rows: snapshot.session.rows! })
        }
        // 先记录真实终态，再解析历史转义序列，防止回放触发自动应答写入已结束的 PTY。
        if (terminalStates.has(snapshot.session.state) || snapshot.session.state === 'closing') closedSessionRef.current = sessionID
        if (!Number.isSafeInteger(nextCursor) || nextCursor < cursorRef.current) throw new Error('终端输出游标无效')
        if (hasMore && nextCursor <= cursorRef.current) throw new Error('终端输出游标未推进')
        // 提示不能写入 ANSI 数据流，否则会改变交互终端的光标与屏幕布局。
        if (snapshot.truncated && !snapshot.screen_state) setOutputTruncated(true)
        if (snapshot.screen_state && restoredCursorRef.current !== nextCursor) {
          const saved = JSON.parse(snapshot.screen_state)
          terminalState.validate(saved)
          restoringRef.current = true
          try {
            await new Promise<void>((resolve, reject) => {
              let finished = false
              const complete = (error?: unknown) => { if (!finished) { finished = true; releaseWrite = null; error ? reject(error) : resolve() } }
              releaseWrite = () => complete()
              const terminal = terminalRef.current
              if (!terminal) { complete(); return }
              terminalState.restore(terminal, saved, complete)
            })
          } finally { if (active && sessionRef.current === sessionID) restoringRef.current = false }
          if (!active || sessionRef.current !== sessionID) return
          if (geometryRef.current?.revision !== geometryRevision) { nextDelay = 0; return }
          restoredCursorRef.current = nextCursor
          setOutputTruncated(false)
          geometryRef.current?.acceptSnapshot({ cols: saved.cols, rows: saved.rows })
          syncSizeRef.current?.()
        }
        const data = snapshot.screen_state ? '' : (snapshot.frames ?? []).map(frame => frame.data).join('')
        if (data) {
          // 每次只让一个快照页进入解析队列，避免快速输出把渲染器的待处理内存撑大。
          await new Promise<void>(resolve => {
            let finished = false
            const parsed = () => { if (!finished) { if (active && sessionRef.current === sessionID) parsingOutputRef.current = false; finished = true; releaseWrite = null; resolve() } }
            releaseWrite = parsed
            const terminal = terminalRef.current
            if (!terminal) { parsed(); return }
            parsingOutputRef.current = true
            terminal.write(data, parsed)
          })
          if (!active || sessionRef.current !== sessionID) return
        }
        if (geometryRef.current?.revision !== geometryRevision) { nextDelay = 0; return }
        cursorRef.current = nextCursor
        const hasOutput = (snapshot.frames ?? []).length > 0 || nextCursor > 0
        setStatus(hasOutput ? '' : voiceOverStateLabel('empty'))
        callbacksRef.current.onSession(snapshot.session)
        if (!terminalStates.has(snapshot.session.state) || hasMore) {
          const interactive = performance.now() < interactiveUntil
          idleReads = data || interactive ? 0 : Math.min(idleReads + 1, 3)
          nextDelay = hasMore ? 0 : interactive ? interactionReadInterval : data ? 50 : 50 * (2 ** idleReads)
        }
      } catch {
        if (active && geometryRef.current?.revision !== geometryRevision) {
          nextDelay = 0
        } else if (active) {
          setStatus('读取终端输出失败')
          callbacksRef.current.onError('读取终端输出失败')
          nextDelay = 1000
        }
      } finally {
        inFlight = false
        if (active && nextDelay !== undefined) schedule(wakeRequested ? 0 : nextDelay)
        wakeRequested = false
      }
    }
    void poll()
    return () => {
      active = false
      if (timer !== null) window.clearTimeout(timer)
      if (wakeOutputRef.current === wake) wakeOutputRef.current = null
      releaseWrite?.()
    }
  }, [ready, sessionID])

  return (
    <div className={`mzgui-terminal-surface ${outputTruncated && showOutputTruncatedNotice ? 'has-truncated-output' : ''} ${className}`.trim()} role="application" aria-label={voiceOverName(ariaLabel)} aria-roledescription={voiceOverRoleDescription('application')} aria-busy={busy || undefined} aria-disabled={blocked || undefined} aria-valuetext={valuetext}>
      {outputTruncated && showOutputTruncatedNotice && <div className="mzgui-terminal-output-notice" role="status">较早终端输出已超出保留范围，当前显示最近内容。</div>}
      <div ref={hostRef} className="mzgui-terminal-surface-host" title="选中后复制；无法拖选时，macOS 按住 Option 拖选，Windows/Linux 按住 Shift 拖选。" />
      <div className="mzgui-live-region" role="status" aria-live="polite" aria-atomic="true">{status}</div>
    </div>
  )
}
