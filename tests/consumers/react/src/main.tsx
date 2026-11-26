import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { DataTable, Dialog, ModuleRail, ThemeProvider, VirtualList } from '@mz-gui/ui-kit'
import '@mz-gui/ui-kit/styles.css'

export function ConsumerApp() {
  const [open, setOpen] = useState(false)
  const [selected, setSelected] = useState('one')
  return <ThemeProvider mode="light">
    <ModuleRail ariaLabel="消费者导航" activeID="home" groups={[{ items: [{ id: 'home', label: '首页', icon: <span>H</span> }] }]} onSelect={() => undefined} />
    <button type="button" onClick={() => setOpen(true)}>打开消费者对话框</button>
    <Dialog open={open} title="独立消费者" onDismiss={() => setOpen(false)}>npm tarball 渲染成功</Dialog>
    <DataTable caption="消费者文件" columns={[{ key: 'name', header: '名称' }]} rows={[{ name: 'consumer.go' }]} />
    <VirtualList ariaLabel="消费者列表" items={[{ id: 'one', label: '第一项' }, { id: 'two', label: '第二项' }]} selectedID={selected} height={64} itemHeight={24} onSelect={setSelected} />
  </ThemeProvider>
}

if (document.getElementById('root')) {
  createRoot(document.getElementById('root')!).render(<ConsumerApp />)
}
