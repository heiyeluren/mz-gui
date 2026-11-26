import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { Tabs } from './Tabs'

const options = [{ value: 'list', label: '列表' }, { value: 'grid', label: '网格' }, { value: 'map', label: '地图' }]

describe('Tabs', () => {
  it('changes the selected tab', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    render(<Tabs ariaLabel="视图" value="list" options={options} onChange={onChange} />)
    expect(screen.getByRole('tablist', { name: '视图' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '列表' })).toHaveAttribute('aria-selected', 'true')
    await user.click(screen.getByRole('tab', { name: '网格' }))
    expect(onChange).toHaveBeenCalledWith('grid')
  })

  it('supports arrow, Home and End keys with one tab stop', async () => {
    const user = userEvent.setup()
    function Host() {
      const [value, setValue] = useState('list')
      return <Tabs ariaLabel="视图" value={value} options={options} onChange={setValue} />
    }
    render(<Host />)
    const list = screen.getByRole('tab', { name: '列表' })
    expect(list).toHaveAttribute('tabIndex', '0')
    expect(screen.getByRole('tab', { name: '网格' })).toHaveAttribute('tabIndex', '-1')
    list.focus()
    await user.keyboard('{ArrowRight}')
    expect(screen.getByRole('tab', { name: '网格' })).toHaveAttribute('aria-selected', 'true')
    expect(document.activeElement).toBe(screen.getByRole('tab', { name: '网格' }))
    await user.keyboard('{End}')
    expect(screen.getByRole('tab', { name: '地图' })).toHaveAttribute('aria-selected', 'true')
    await user.keyboard('{Home}')
    expect(screen.getByRole('tab', { name: '列表' })).toHaveAttribute('aria-selected', 'true')
  })

  it('supports typeahead selection', async () => {
    const user = userEvent.setup()
    function Host() {
      const [value, setValue] = useState('list')
      return <Tabs ariaLabel="视图" value={value} options={options} onChange={setValue} />
    }
    render(<Host />)
    screen.getByRole('tab', { name: '列表' }).focus()
    await user.keyboard('网')
    expect(screen.getByRole('tab', { name: '网格' })).toHaveAttribute('aria-selected', 'true')
  })

  it('renders an empty state', () => {
    render(<Tabs ariaLabel="视图" value="" options={[]} onChange={() => undefined} />)
    const empty = screen.getByRole('status', { name: '视图' })
    expect(empty).toHaveTextContent('空')
    expect(empty).toHaveAttribute('aria-valuetext', '空')
  })

  it.each([
    { disabled: true, busy: false, label: '不可用', attribute: 'aria-disabled' },
    { disabled: false, busy: true, label: '正在加载', attribute: 'aria-busy' },
  ])('blocks changes in blocked states', async ({ disabled, busy, label, attribute }) => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    render(<Tabs ariaLabel="视图" value="list" disabled={disabled} busy={busy} options={options.slice(0, 2)} onChange={onChange} />)
    const list = screen.getByRole('tablist', { name: '视图' })
    expect(list).toHaveAttribute(attribute, 'true')
    expect(list).toHaveAttribute('aria-valuetext', label)
    await user.click(screen.getByRole('tab', { name: '网格' }))
    expect(onChange).not.toHaveBeenCalled()
  })
})
