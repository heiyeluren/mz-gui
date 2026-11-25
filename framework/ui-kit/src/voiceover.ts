import { screenReaderMatrix } from './a11y'

export const voiceOverRoleLabels: Record<string, string> = {
  dialog: '对话框',
  button: '按钮',
  navigation: '导航',
  status: '状态',
  alert: '警告',
  switch: '开关',
  checkbox: '复选框',
  progressbar: '进度条',
  tree: '树',
  menu: '菜单',
  tablist: '标签页',
  listbox: '列表',
  article: '文章',
  img: '图像',
  region: '区域',
  application: '应用程序',
  textbox: '编辑文本',
  group: '组',
  'native-select': '弹出按钮',
  'native-table': '表格',
  link: '链接',
  main: '主要',
}

const voiceOverPhraseNames: Record<string, string> = {
  ModuleRail: '系统模块',
  Select: '选项',
  Checkbox: '选项',
  Switch: '开机启动',
  LiveRegion: '状态更新',
  Dialog: '确认操作',
  DataTable: '数据表',
  Tree: '目录',
  Breadcrumb: '路径',
  ContextMenu: '操作',
  ProgressBar: '进度',
  VirtualList: '列表',
  MarkdownViewer: '文档',
  ImageViewer: '图片',
  CodeViewer: '代码',
  MediaSurface: '媒体',
  TerminalSurface: '终端输出',
  Tabs: '分区',
  IconButton: '操作',
  TextField: '输入',
  FeedbackToast: '已保存',
  SegmentedControl: '分段',
  SkipToContent: '跳到主要内容',
  MainLandmark: '主要内容',
}

export const voiceOverStateLabels: Record<string, string> = {
  on: '打开',
  off: '关闭',
  checked: '已选中',
  unchecked: '未选中',
  mixed: '部分选中',
  expanded: '已展开',
  collapsed: '已折叠',
  selected: '已选中',
  asc: '升序',
  desc: '降序',
  none: '未排序',
  disabled: '不可用',
  empty: '空',
  required: '必填',
  loading: '正在加载',
  idle: '空闲',
  requesting: '正在请求',
  recording: '正在录音',
  paused: '已暂停',
  stopping: '正在停止',
}

export function voiceOverRoleDescription(role: string): string {
  return voiceOverRoleLabels[role] ?? role
}

export function voiceOverStateLabel(state: string): string {
  return voiceOverStateLabels[state] ?? state
}

export function voiceOverName(name: string): string {
  const spoken = name.trim()
  return spoken === '' ? voiceOverStateLabel('empty') : spoken
}

export function speakVoiceOver(role: string, name: string, value?: string): string {
  const spokenName = voiceOverName(name)
  const spokenRole = voiceOverRoleDescription(role)
  const parts = [spokenName, spokenRole]
  if (value && value.trim() !== '') {
    parts.push(value.trim())
  }
  return parts.filter((part) => part !== '').join('，')
}

export function speakVoiceOverState(role: string, name: string, state: string): string {
  return speakVoiceOver(role, name, voiceOverStateLabel(state))
}

export function voiceOverPercentText(value: number, max = 100): string {
  if (!Number.isFinite(value) || !Number.isFinite(max)) {
    return voiceOverStateLabel('empty')
  }
  const ceiling = max > 0 ? max : 100
  const current = Math.min(Math.max(value, 0), ceiling)
  return `百分之 ${Math.round((current / ceiling) * 100)}`
}

export function speakVoiceOverProgress(name: string, value: number, max = 100): string {
  return speakVoiceOver('progressbar', name, voiceOverPercentText(value, max))
}

export function speakVoiceOverSort(name: string, direction?: 'asc' | 'desc'): string {
  const spoken = voiceOverStateLabel(direction === 'asc' || direction === 'desc' ? direction : 'none')
  return [voiceOverName(name), spoken].join('，')
}

export function voiceOverCountText(count: number): string {
  const total = Number.isFinite(count) ? Math.max(0, Math.floor(count)) : 0
  return `共 ${total} 项`
}

export function speakVoiceOverCount(role: string, name: string, count: number): string {
  return speakVoiceOver(role, name, voiceOverCountText(count))
}

export function announceRestore(phrases: string[]): string {
  const items = phrases.map((phrase) => phrase.trim()).filter((phrase) => phrase !== '')
  if (items.length === 0) {
    return ''
  }
  return `已恢复，${items.join('，')}`
}

export function voiceOverPhrases() {
  return screenReaderMatrix.map((entry) => {
    const name = voiceOverPhraseNames[entry.component] ?? entry.component
    return {
      component: entry.component,
      role: entry.role,
      name,
      spoken: speakVoiceOver(entry.role, name),
    }
  })
}
