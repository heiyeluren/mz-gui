import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { CommandPalette } from './CommandPalette'

describe('CommandPalette', () => {
  it('does not render while closed', () => {
    render(<CommandPalette open={false} query="" items={[]} onQueryChange={() => undefined} onDismiss={() => undefined} />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('exposes the dialog, query and options', () => {
    render(<CommandPalette open query="" items={[{ id: 'open', label: '打开' }]} onQueryChange={() => undefined} onDismiss={() => undefined} />)
    expect(screen.getByRole('dialog', { name: '命令' })).toHaveClass('mzgui-command-palette')
    expect(screen.getByRole('combobox', { name: '筛选' })).toHaveClass('mzgui-command-palette-query')
    expect(screen.getByRole('option', { name: '打开' })).toBeInTheDocument()
  })

  it('renders an empty status', () => {
    render(<CommandPalette open query="" items={[]} onQueryChange={() => undefined} onDismiss={() => undefined} />)
    expect(screen.getByRole('dialog', { name: '命令' })).toHaveAttribute('aria-valuetext', '空')
    expect(screen.getByRole('status')).toHaveTextContent('空')
  })

  it('handles query changes, selection and dismissal', async () => {
    const onQueryChange = vi.fn()
    const onSelect = vi.fn()
    const onDismiss = vi.fn()
    const user = userEvent.setup()
    render(<CommandPalette open query="" items={[{ id: 'open', label: '打开' }]} onQueryChange={onQueryChange} onSelect={onSelect} onDismiss={onDismiss} />)
    await user.type(screen.getByRole('combobox', { name: '筛选' }), 'a')
    expect(onQueryChange).toHaveBeenCalled()
    await user.click(screen.getByRole('option', { name: '打开' }))
    expect(onSelect).toHaveBeenCalledWith('open')
    await user.keyboard('{Escape}')
    expect(onDismiss).toHaveBeenCalledTimes(1)
  })

  it.each([
    { disabled: true, busy: false, label: '不可用', attribute: 'aria-disabled' },
    { disabled: false, busy: true, label: '正在加载', attribute: 'aria-busy' },
  ])('blocks dismissal and query changes in blocked states', async ({ disabled, busy, label, attribute }) => {
    const onDismiss = vi.fn()
    const onQueryChange = vi.fn()
    const user = userEvent.setup()
    render(<CommandPalette open query="" items={[{ id: 'open', label: '打开' }]} disabled={disabled} busy={busy} onQueryChange={onQueryChange} onDismiss={onDismiss} />)
    const dialog = screen.getByRole('dialog', { name: '命令' })
    expect(dialog).toHaveAttribute(attribute, 'true')
    expect(dialog).toHaveAttribute('aria-valuetext', label)
    await user.keyboard('{Escape}')
    expect(onDismiss).not.toHaveBeenCalled()
    expect(onQueryChange).not.toHaveBeenCalled()
  })
})
