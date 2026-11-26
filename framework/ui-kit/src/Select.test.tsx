import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Select } from './Select'

const options = [{ value: 'list', label: '列表' }, { value: 'grid', label: '网格' }]

describe('Select', () => {
  it('shows the selected label and emits a new value', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    render(<Select id="view" label="视图" value="list" options={options} onChange={onChange} />)
    const select = screen.getByRole('combobox', { name: '视图' })
    expect(select).toHaveAttribute('aria-valuetext', '列表')
    await user.selectOptions(select, 'grid')
    expect(onChange).toHaveBeenCalledWith('grid')
  })

  it('uses an empty label when the selected option is absent', () => {
    render(<Select label="视图" value="missing" options={options} onChange={() => undefined} />)
    expect(screen.getByRole('combobox', { name: '视图' })).toHaveAttribute('aria-valuetext', '空')
  })

  it('blocks changes in disabled and busy states', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    const { rerender } = render(<Select label="视图" value="list" options={options} disabled onChange={onChange} />)
    const select = screen.getByRole('combobox', { name: '视图' })
    expect(select).toHaveAttribute('aria-valuetext', '不可用')
    await user.selectOptions(select, 'grid')
    expect(onChange).not.toHaveBeenCalled()
    rerender(<Select label="视图" value="list" options={options} busy onChange={onChange} />)
    expect(select).toHaveAttribute('aria-valuetext', '正在加载')
    await user.selectOptions(select, 'grid')
    expect(onChange).not.toHaveBeenCalled()
  })
})
