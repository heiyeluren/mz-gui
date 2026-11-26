import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { DataTable } from './DataTable'

describe('DataTable', () => {
  it('exposes sort state and emits sort requests', async () => {
    const onSort = vi.fn()
    const user = userEvent.setup()
    const { rerender } = render(<DataTable caption="文件" columns={[{ key: 'name', header: '名称', sortable: true }]} rows={[{ name: 'a.txt' }]} sortKey="name" sortDirection="asc" onSort={onSort} />)
    const header = screen.getByRole('button', { name: '名称，升序' })
    expect(header.closest('th')).toHaveAttribute('aria-sort', 'ascending')
    await user.click(header)
    expect(onSort).toHaveBeenCalledWith('name')
    rerender(<DataTable caption="文件" columns={[{ key: 'name', header: '名称', sortable: true }]} rows={[{ name: 'a.txt' }]} sortKey="name" sortDirection="desc" onSort={onSort} />)
    expect(screen.getByRole('button', { name: '名称，降序' })).toBeInTheDocument()
  })

  it('renders an empty state', () => {
    render(<DataTable caption="文件" columns={[{ key: 'name', header: '名称' }]} rows={[]} />)
    expect(screen.getByRole('table', { name: '文件' })).toHaveAttribute('aria-valuetext', '空')
    expect(screen.getByText('空')).toBeInTheDocument()
  })

  it('supports typeahead and arrow/Home/End navigation', async () => {
    const user = userEvent.setup()
    render(<DataTable caption="文件" columns={[{ key: 'name', header: '名称' }]} rows={[{ name: '说明.txt' }, { name: '源码.go' }, { name: '测试.go' }]} />)
    const rows = screen.getAllByRole('row').slice(1)
    rows[0].focus()
    await user.keyboard('源')
    expect(document.activeElement).toBe(rows[1])
    await user.keyboard('{End}')
    expect(document.activeElement).toBe(rows[2])
    await user.keyboard('{Home}')
    expect(document.activeElement).toBe(rows[0])
    await user.keyboard('{ArrowDown}')
    expect(document.activeElement).toBe(rows[1])
  })

  it('reports row counts and positions', () => {
    render(<DataTable caption="文件" columns={[{ key: 'name', header: '名称' }]} rows={[{ name: 'a' }, { name: 'b' }, { name: 'c' }]} />)
    expect(screen.getByRole('table', { name: '文件' })).toHaveAttribute('aria-valuetext', '共 3 项')
    const rows = screen.getAllByRole('row').slice(1)
    expect(rows[0]).toHaveAttribute('aria-posinset', '1')
    expect(rows[2]).toHaveAttribute('aria-posinset', '3')
    expect(rows[2]).toHaveAttribute('aria-setsize', '3')
  })

  it('blocks sorting and focus movement in disabled and busy states', async () => {
    const onSort = vi.fn()
    const user = userEvent.setup()
    const { rerender } = render(<DataTable caption="文件" disabled columns={[{ key: 'name', header: '名称', sortable: true }]} rows={[{ name: 'a' }, { name: 'b' }]} sortKey="name" sortDirection="asc" onSort={onSort} />)
    const table = screen.getByRole('table', { name: '文件' })
    expect(table).toHaveAttribute('aria-valuetext', '不可用')
    await user.click(screen.getByRole('button', { name: '名称，升序' }))
    expect(onSort).not.toHaveBeenCalled()
    rerender(<DataTable caption="文件" busy columns={[{ key: 'name', header: '名称' }]} rows={[{ name: 'a' }, { name: 'b' }]} />)
    const rows = screen.getAllByRole('row').slice(1)
    rows[0].focus()
    await user.keyboard('{ArrowDown}')
    expect(document.activeElement).toBe(rows[0])
  })
})
