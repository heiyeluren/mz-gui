export type ShortcutPlatform = 'macos' | 'windows'

export type ShortcutModifier = 'ctrl' | 'alt' | 'shift' | 'meta' | 'cmd-or-ctrl'

export type ShortcutAccelerator = {
  key: string
  modifiers: ShortcutModifier[]
}

export type ShortcutBinding = {
  id: string
  commandType: string
  scope: string
  accelerator: ShortcutAccelerator
}

export type ShortcutKeyEvent = {
  key: string
  ctrlKey: boolean
  metaKey: boolean
  altKey: boolean
  shiftKey: boolean
}

const modifierSpoken: Record<ShortcutModifier, { macos: string; windows: string }> = {
  ctrl: { macos: 'Control', windows: 'Control' },
  alt: { macos: 'Option', windows: 'Alt' },
  shift: { macos: 'Shift', windows: 'Shift' },
  meta: { macos: '命令', windows: 'Win' },
  'cmd-or-ctrl': { macos: '命令', windows: 'Control' },
}

const keySpoken: Record<string, string> = {
  arrowdown: '下箭头',
  arrowup: '上箭头',
  arrowleft: '左箭头',
  arrowright: '右箭头',
  home: '行首',
  end: '行尾',
  escape: '退出',
  tab: '制表符',
  enter: '回车',
  return: '回车',
  ' ': '空格',
  space: '空格',
  spacebar: '空格',
  delete: '删除',
  backspace: '删除',
  pageup: '上一页',
  pagedown: '下一页',
  insert: '插入',
  '+': '加号',
  add: '加号',
  '-': '减号',
  subtract: '减号',
  '*': '星号',
  '=': '等号',
  equal: '等号',
  '/': '斜杠',
  slash: '斜杠',
  ',': '逗号',
  comma: '逗号',
  '.': '句号',
  period: '句号',
  '[': '左方括号',
  bracketleft: '左方括号',
  ']': '右方括号',
  bracketright: '右方括号',
  ';': '分号',
  semicolon: '分号',
  "'": '引号',
  quote: '引号',
  '\\': '反斜杠',
  backslash: '反斜杠',
  '`': '重音符',
  backquote: '重音符',
  '{': '左花括号',
  braceleft: '左花括号',
  '}': '右花括号',
  braceright: '右花括号',
  '(': '左圆括号',
  ')': '右圆括号',
  '|': '竖线',
  bar: '竖线',
  '?': '问号',
  '!': '感叹号',
  '@': '商业at',
  '#': '井号',
  $: '美元符号',
  '%': '百分号',
  '^': '插入符号',
  '&': '和号',
  '<': '小于号',
  '>': '大于号',
  capslock: '大写锁定',
  contextmenu: '菜单',
  printscreen: '打印屏幕',
  scrolllock: '滚动锁定',
  pause: '暂停',
  numlock: '数字锁定',
}

export function speakShortcut(accelerator: ShortcutAccelerator, platform: ShortcutPlatform): string {
  const parts = accelerator.modifiers.map((modifier) => modifierSpoken[modifier][platform])
  const raw = accelerator.key
  if (raw === '') {
    return parts.filter((part) => part && part.trim() !== '').join('+') || '空'
  }
  const spokenKey = raw.trim() === '' ? keySpoken[' '] : (keySpoken[raw.toLowerCase()] ?? raw.toUpperCase())
  parts.push(spokenKey)
  return parts.filter((part) => part && part.trim() !== '').join('+')
}

export function matchShortcut(
  event: ShortcutKeyEvent,
  bindings: ShortcutBinding[],
  platform: ShortcutPlatform,
  scope: string,
): ShortcutBinding | null {
  const wantedScope = scope.trim().toLowerCase()
  const eventKey = event.key.trim().toLowerCase()
  if (eventKey === '' || wantedScope === '') {
    return null
  }
  for (const binding of bindings) {
    if (binding.scope.trim().toLowerCase() !== wantedScope) {
      continue
    }
    if (acceleratorMatches(event, binding.accelerator, platform)) {
      return binding
    }
  }
  return null
}

function acceleratorMatches(event: ShortcutKeyEvent, accelerator: ShortcutAccelerator, platform: ShortcutPlatform): boolean {
  if (event.key.trim().toLowerCase() !== accelerator.key.trim().toLowerCase()) {
    return false
  }
  const wanted = new Set(accelerator.modifiers)
  const ctrl = wanted.has('ctrl') || (wanted.has('cmd-or-ctrl') && platform === 'windows')
  const meta = wanted.has('meta') || (wanted.has('cmd-or-ctrl') && platform === 'macos')
  return event.ctrlKey === ctrl && event.metaKey === meta && event.altKey === wanted.has('alt') && event.shiftKey === wanted.has('shift')
}
