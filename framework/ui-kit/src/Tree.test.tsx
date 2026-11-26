import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Tree } from './Tree'

const nodes = [{ id: 'root', label: '根节点', children: [{ id: 'child', label: '子节点' }] }, { id: 'other', label: '其他' }]

describe('Tree', () => {
  it('renders an empty state', () => {
    render(<Tree ariaLabel="文件树" nodes={[]} />)
    expect(screen.getByRole('status', { name: '文件树' })).toHaveTextContent('空')
  })

  it('selects and toggles nodes', async () => {
    const onSelect = vi.fn()
    const onToggle = vi.fn()
    const user = userEvent.setup()
    render(<Tree ariaLabel="文件树" nodes={nodes} expandedIDs={['root']} onSelect={onSelect} onToggle={onToggle} />)
    await user.click(screen.getByRole('button', { name: '根节点' }))
    expect(onSelect).toHaveBeenCalledWith('root')
    expect(onToggle).toHaveBeenCalledWith('root')
    expect(screen.getByRole('button', { name: '子节点' })).toBeInTheDocument()
  })

  it('supports keyboard selection and expansion', async () => {
    const onSelect = vi.fn()
    const onToggle = vi.fn()
    const user = userEvent.setup()
    render(<Tree ariaLabel="文件树" nodes={nodes} selectedID="root" expandedIDs={['root']} onSelect={onSelect} onToggle={onToggle} />)
    const tree = screen.getByRole('tree', { name: '文件树' })
    tree.focus()
    await user.keyboard('{ArrowDown}')
    expect(onSelect).toHaveBeenCalledWith('child')
    await user.keyboard('{ArrowLeft}')
    expect(onToggle).toHaveBeenCalledWith('root')
    await user.keyboard('{Home}')
    expect(onSelect).toHaveBeenCalledWith('root')
  })

  it('exposes tree levels and positions', () => {
    render(<Tree ariaLabel="文件树" nodes={nodes} expandedIDs={['root']} />)
    expect(screen.getByRole('treeitem', { name: '根节点' })).toHaveAttribute('aria-level', '1')
    expect(screen.getByRole('treeitem', { name: '子节点' })).toHaveAttribute('aria-level', '2')
    expect(screen.getByRole('treeitem', { name: '其他' })).toHaveAttribute('aria-posinset', '2')
  })

  it('blocks selection in disabled and busy states', async () => {
    const onSelect = vi.fn()
    const onToggle = vi.fn()
    const user = userEvent.setup()
    const { rerender } = render(<Tree ariaLabel="文件树" nodes={nodes} disabled onSelect={onSelect} onToggle={onToggle} />)
    const tree = screen.getByRole('tree', { name: '文件树' })
    expect(tree).toHaveAttribute('aria-valuetext', '不可用')
    await user.click(screen.getByRole('button', { name: '根节点' }))
    expect(onSelect).not.toHaveBeenCalled()
    rerender(<Tree ariaLabel="文件树" nodes={nodes} busy onSelect={onSelect} onToggle={onToggle} />)
    expect(tree).toHaveAttribute('aria-valuetext', '正在加载')
    tree.focus()
    await user.keyboard('{ArrowDown}')
    expect(onSelect).not.toHaveBeenCalled()
  })
})
