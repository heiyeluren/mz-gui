type PendingCommit = { preview: string; value: string; committed: string }

export function isMacWebKit(userAgent = globalThis.navigator?.userAgent ?? '') {
  return /Macintosh|Mac OS X/.test(userAgent) && /AppleWebKit/.test(userAgent) && !/Chrome|Chromium|Edg\//.test(userAgent)
}

/** 修正 WebKit 的拼音最终提交与首个标点事件顺序，不改写普通终端文本。 */
export function bindTerminalIme(textarea: HTMLTextAreaElement | undefined, enabled = isMacWebKit(), keyboardState?: { _keyDownSeen?: boolean }) {
  let composing = false
  let shiftOnlyKeyDown = false
  let preview = ''
  let raw: string | null = null
  let initialKey = ''
  let shiftCommit = false
  let pending: PendingCommit | null = null
  let timer: ReturnType<typeof setTimeout> | undefined
  const parent = textarea?.parentElement
  const clearPending = () => {
    if (timer !== undefined) clearTimeout(timer)
    timer = undefined
    pending = null
  }
  const letter = (event: KeyboardEvent) => {
    if (event.ctrlKey || event.altKey || event.metaKey) return ''
    if (/^[a-zA-Z]$/.test(event.key)) return event.key
    if (/^Key[A-Z]$/.test(event.code)) return event.shiftKey ? event.code.slice(3) : event.code.slice(3).toLowerCase()
    return ''
  }
  const onKeyDown = (event: KeyboardEvent) => {
    if (event.target !== textarea) return
    shiftOnlyKeyDown = event.key === 'Shift' && !event.ctrlKey && !event.altKey && !event.metaKey
    if (!composing) {
      initialKey = letter(event)
      return
    }
    if (event.key === 'Shift' && !event.ctrlKey && !event.altKey && !event.metaKey) {
      shiftCommit = true
    } else if (raw !== null) {
      const character = letter(event)
      if (character) raw += character
      else if (event.key === 'Backspace') raw = raw.slice(0, -1)
      else if (!['Control', 'Alt', 'Meta'].includes(event.key)) raw = null
    }
    // xterm 6 仅按 keyCode 判断组合，WebKit 的方向键可能仍带真实 keyCode。
    // 保留浏览器默认动作，让输入法编辑组合；不让 xterm 提前把预览发送给 PTY。
    if (event.isComposing && event.keyCode !== 229) event.stopImmediatePropagation()
  }
  const clearShiftKeyDown = () => { shiftOnlyKeyDown = false }
  const onBeforeInput = (event: InputEvent) => {
    if (event.target !== textarea || composing || pending || event.isComposing || !shiftOnlyKeyDown) return
    if (event.inputType !== 'insertText' || !event.data) return
    // xterm 6.0.0：WebKit 中文标点先 input、后 keydown，残留的 Shift 标记会吞首字。
    // 仅放行尚未出现字符 keydown 的最终文本，保留正常按键的防重复与组合提交逻辑。
    // 私有字段需形状检查；真实 xterm 事件回归约束此适配（上游 issue #6144）。
    if (typeof keyboardState?._keyDownSeen === 'boolean') keyboardState._keyDownSeen = false
  }
  const onStart = () => {
    clearPending()
    clearShiftKeyDown()
    composing = true
    preview = ''
    raw = initialKey || null
    initialKey = ''
    shiftCommit = false
  }
  const onUpdate = (event: CompositionEvent) => { preview = event.data }
  const onEnd = (event: CompositionEvent) => {
    composing = false
    clearShiftKeyDown()
    let committed = event.data
    // 部分拼音输入法在 Shift 原文上屏时仍提交自动分节符。仅在有完整物理字母
    // 按键证据、没有手输引号/光标调整且字符完全吻合时恢复原文，不做全局去标点。
    if (shiftCommit && raw && /^[a-zA-Z]+(?:['’][a-zA-Z]+)+$/.test(committed) && committed.replace(/['’]/g, '') === raw) {
      committed = raw
    }
    pending = { preview, value: event.data, committed }
    // 本监听器在 terminal.open 后注册，排在 xterm 的延迟组合提交之后。
    timer = setTimeout(clearPending, 0)
  }
  if (enabled && textarea) {
    parent?.addEventListener('keydown', onKeyDown, true)
    parent?.addEventListener('beforeinput', onBeforeInput, true)
    textarea.addEventListener('keyup', clearShiftKeyDown)
    textarea.addEventListener('blur', clearShiftKeyDown)
    textarea.addEventListener('compositionstart', onStart)
    textarea.addEventListener('compositionupdate', onUpdate)
    textarea.addEventListener('compositionend', onEnd)
  }
  return {
    filter(data: string) {
      if (!enabled || !pending) return data
      if (data !== pending.preview && data !== pending.value) return data
      const result = pending.committed
      clearPending()
      return result
    },
    dispose() {
      clearPending()
      parent?.removeEventListener('keydown', onKeyDown, true)
      parent?.removeEventListener('beforeinput', onBeforeInput, true)
      textarea?.removeEventListener('keyup', clearShiftKeyDown)
      textarea?.removeEventListener('blur', clearShiftKeyDown)
      textarea?.removeEventListener('compositionstart', onStart)
      textarea?.removeEventListener('compositionupdate', onUpdate)
      textarea?.removeEventListener('compositionend', onEnd)
    },
  }
}
