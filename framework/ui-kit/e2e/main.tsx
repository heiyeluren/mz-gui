import { useRef, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { ThemeProvider } from '../src/ThemeProvider'
import { Tabs } from '../src/Tabs'
import { Dialog } from '../src/Dialog'
import { TextField } from '../src/TextField'
import { DataTable } from '../src/DataTable'
import { VirtualList } from '../src/VirtualList'
import { MarkdownViewer } from '../src/MarkdownViewer'
import { TerminalSurface } from '../src/TerminalSurface'
import { Tree } from '../src/Tree'
import { MediaSurface } from '../src/MediaSurface'
import { MermaidDiagram } from '../src/MermaidDiagram'
import '../../web/tokens.css'
import '../src/styles.css'

function TerminalFixture() {
  const model = useRef({
    frames: ['ready\r\n'],
    batch: 0,
    failNext: false,
    lastInput: '',
  })
  const cursorHost = useRef<HTMLOutputElement>(null)
  const bufferedHost = useRef<HTMLOutputElement>(null)
  const [lastInput, setLastInput] = useState('')
  const [size, setSize] = useState('0x0')
  const session = { session_id: 'e2e-terminal', state: 'running' }
  return <section aria-label="终端测试" style={{ height: 300, display: 'flex', flexDirection: 'column' }}>
    <div>
      <button type="button" onClick={() => {
        model.current.batch += 1
        if (bufferedHost.current) bufferedHost.current.textContent = String(model.current.frames.length + 500)
        for (let index = 0; index < 500; index++) model.current.frames.push(`batch-${model.current.batch}-line-${String(index).padStart(4, '0')}\r\n`)
      }}>模拟高频输出</button>
      <button type="button" onClick={() => {
        model.current.failNext = true
        model.current.frames.push('recovered\r\n')
      }}>模拟断线</button>
      <output data-testid="terminal-last-input">{lastInput}</output>
      <output data-testid="terminal-size">{size}</output>
      <output ref={cursorHost} data-testid="terminal-cursor">0</output>
      <output ref={bufferedHost} data-testid="terminal-buffered">1</output>
    </div>
    <TerminalSurface
      ready
      ariaLabel="交互终端"
      sessionID="e2e-terminal"
      getSnapshot={async (_sessionID, cursor) => {
        if (model.current.failNext) {
          model.current.failNext = false
          throw new Error('simulated disconnect')
        }
        const next = Math.min(cursor + 40, model.current.frames.length)
        if (cursorHost.current) cursorHost.current.textContent = String(next)
        if (bufferedHost.current) bufferedHost.current.textContent = String(model.current.frames.length - next)
        return {
          session,
          frames: model.current.frames.slice(cursor, next).map((data) => ({ data })),
          next_cursor: next,
          truncated: false,
          has_more: next < model.current.frames.length,
        }
      }}
      writeInput={async (_sessionID, data) => {
        model.current.lastInput = data
        setLastInput(data)
        if (data === 'pwd\r') model.current.frames.push('ready\r\n')
        return { session }
      }}
      resizeSession={async (_sessionID, cols, rows) => {
        setSize(`${cols}x${rows}`)
        return { session: { ...session, cols, rows } }
      }}
      onSession={() => undefined}
    />
  </section>
}

function AdvancedDataFixture() {
  const [direction, setDirection] = useState<'asc' | 'desc'>('asc')
  const [selectedTree, setSelectedTree] = useState('root')
  const [expandedTree, setExpandedTree] = useState<string[]>([])
  const [selectedItem, setSelectedItem] = useState('item-0')
  const savedScroll = useRef(0)
  const listHost = useRef<HTMLDivElement>(null)
  const items = useRef(Array.from({ length: 5000 }, (_, index) => ({ id: `item-${index}`, label: `项目 ${index}` })))
  const rows = [{ name: 'alpha' }, { name: 'zeta' }, { name: 'middle' }]
  const sortedRows = [...rows].sort((left, right) => direction === 'asc' ? left.name.localeCompare(right.name) : right.name.localeCompare(left.name))
  const setListScroll = (top: number) => {
    const list = listHost.current?.querySelector<HTMLElement>('[role="listbox"]')
    if (list) {
      list.scrollTop = top
      list.dispatchEvent(new Event('scroll', { bubbles: true }))
    }
  }
  return <section aria-label="高级数据组件">
    <DataTable caption="可排序文件" columns={[{ key: 'name', header: '名称', sortable: true }]} rows={sortedRows} sortKey="name" sortDirection={direction} onSort={() => setDirection((current) => current === 'asc' ? 'desc' : 'asc')} />
    <Tree ariaLabel="模块树" nodes={[{ id: 'root', label: '根模块', children: [{ id: 'child', label: '子模块' }] }]} selectedID={selectedTree} expandedIDs={expandedTree} onSelect={setSelectedTree} onToggle={(id) => setExpandedTree((current) => current.includes(id) ? current.filter((item) => item !== id) : [...current, id])} />
    <div ref={listHost}>
      <VirtualList ariaLabel="五千项列表" items={items.current} selectedID={selectedItem} height={120} itemHeight={24} onSelect={setSelectedItem} />
    </div>
    <button type="button" onClick={() => {
      savedScroll.current = listHost.current?.querySelector('[role="listbox"]')?.scrollTop ?? 0
    }}>保存滚动位置</button>
    <button type="button" onClick={() => setListScroll(savedScroll.current)}>恢复滚动位置</button>
  </section>
}

function App() {
  const [view, setView] = useState('list')
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  return <ThemeProvider mode="light" locale="zh-CN">
    <main>
      <h1>UI Kit E2E</h1>
      <Tabs ariaLabel="视图" value={view} options={[{ value: 'list', label: '列表' }, { value: 'grid', label: '网格' }]} onChange={setView} />
      <button type="button" onClick={() => setOpen(true)}>打开对话框</button>
      <Dialog open={open} title="编辑名称" onDismiss={() => setOpen(false)}>
        <TextField id="name" label="名称" value={name} required onChange={setName} />
      </Dialog>
      <DataTable caption="文件" columns={[{ key: 'name', header: '名称', sortable: true }]} rows={[{ name: 'a.txt' }, { name: 'b.txt' }]} sortKey="name" sortDirection="asc" onSort={() => undefined} />
      <VirtualList ariaLabel="大列表" items={Array.from({ length: 100 }, (_, index) => ({ id: `i${index}`, label: `项${index}` }))} selectedID="i0" height={80} itemHeight={20} onSelect={() => undefined} />
      <MarkdownViewer ariaLabel="说明" source={'[安全](https://example.com/a) [危险](javascript:alert(1))'} />
      <TerminalFixture />
      <AdvancedDataFixture />
      <section aria-label="媒体与图表">
        <MediaSurface ariaLabel="演示视频" src="https://example.com/demo.mp4" />
        <MediaSurface ariaLabel="非法媒体" src="javascript:alert(1)" />
        <MermaidDiagram source="graph TD; A-->B" ariaLabel="简单流程图" fallback={<span>图表渲染失败</span>} />
      </section>
    </main>
  </ThemeProvider>
}

createRoot(document.getElementById('root')!).render(<App />)
