import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { VirtualList } from './VirtualList'

describe('VirtualList', () => {
  it('renders an empty state', () => {
    render(<VirtualList ariaLabel="文件" items={[]} height={80} itemHeight={20} />)
    expect(screen.getByRole('status', { name: '文件' })).toHaveTextContent('空')
  })

  it('changes selection through arrows and typeahead', async () => {
    const user = userEvent.setup()
    function Host() {
      const [selectedID, setSelectedID] = useState('a')
      return <VirtualList ariaLabel="文件" items={[{ id: 'a', label: '说明' }, { id: 'b', label: '源码' }, { id: 'c', label: '测试' }]} selectedID={selectedID} height={80} itemHeight={20} onSelect={setSelectedID} />
    }
    render(<Host />)
    const list = screen.getByRole('listbox', { name: '文件' })
    list.focus()
    await user.keyboard('{ArrowDown}')
    expect(list).toHaveAttribute('aria-valuetext', '源码')
    await user.keyboard('测')
    expect(list).toHaveAttribute('aria-valuetext', '测试')
    await user.keyboard('{Home}')
    expect(list).toHaveAttribute('aria-valuetext', '说明')
    await user.keyboard('{End}')
    expect(list).toHaveAttribute('aria-valuetext', '测试')
  })

  it('renders only the visible window for large lists', () => {
    const items = Array.from({ length: 100 }, (_, index) => ({ id: `i${index}`, label: `项${index}` }))
    render(<VirtualList ariaLabel="文件" items={items} height={40} itemHeight={10} overscan={1} />)
    expect(screen.getAllByRole('option')).toHaveLength(6)
  })

  it('reports global positions and selected labels', () => {
    render(<VirtualList ariaLabel="文件" items={[{ id: 'a', label: '说明' }, { id: 'b', label: '源码' }]} selectedID="b" height={80} itemHeight={20} />)
    expect(screen.getByRole('option', { name: '源码' })).toHaveAttribute('aria-posinset', '2')
    expect(screen.getByRole('option', { name: '源码' })).toHaveAttribute('aria-setsize', '2')
    expect(screen.getByRole('listbox', { name: '文件' })).toHaveAttribute('aria-valuetext', '源码')
  })

  it('blocks selection in disabled and busy states', async () => {
    const onSelect = vi.fn()
    const user = userEvent.setup()
    const { rerender } = render(<VirtualList ariaLabel="文件" items={[{ id: 'a', label: '说明' }, { id: 'b', label: '源码' }]} selectedID="a" height={80} itemHeight={20} disabled onSelect={onSelect} />)
    const list = screen.getByRole('listbox', { name: '文件' })
    expect(list).toHaveAttribute('aria-valuetext', '不可用')
    list.focus()
    await user.keyboard('{ArrowDown}')
    await user.click(screen.getByRole('option', { name: '源码' }))
    expect(onSelect).not.toHaveBeenCalled()
    rerender(<VirtualList ariaLabel="文件" items={[{ id: 'a', label: '说明' }, { id: 'b', label: '源码' }]} selectedID="a" height={80} itemHeight={20} busy onSelect={onSelect} />)
    expect(list).toHaveAttribute('aria-valuetext', '正在加载')
    await user.keyboard('{End}')
    await user.click(screen.getByRole('option', { name: '源码' }))
    expect(onSelect).not.toHaveBeenCalled()
  })
})
