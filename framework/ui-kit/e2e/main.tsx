import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { ThemeProvider } from '../src/ThemeProvider'
import { Tabs } from '../src/Tabs'
import { Dialog } from '../src/Dialog'
import { TextField } from '../src/TextField'
import { DataTable } from '../src/DataTable'
import { VirtualList } from '../src/VirtualList'
import { MarkdownViewer } from '../src/MarkdownViewer'
import '../../web/tokens.css'
import '../src/styles.css'

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
    </main>
  </ThemeProvider>
}

createRoot(document.getElementById('root')!).render(<App />)
