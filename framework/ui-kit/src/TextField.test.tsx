import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TextField } from './TextField'

describe('TextField', () => {
  it('is controlled and emits changes', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    render(<TextField id="name" label="名称" value="" onChange={onChange} />)
    await user.type(screen.getByRole('textbox', { name: '名称' }), 'MZ')
    expect(onChange).toHaveBeenCalled()
  })

  it('links errors and required semantics', () => {
    render(<TextField id="name" label="名称" value="" required error="名称不能为空" onChange={() => undefined} />)
    const input = screen.getByRole('textbox', { name: '名称' })
    expect(input).toHaveAttribute('aria-required', 'true')
    expect(input).toHaveAttribute('aria-invalid', 'true')
    expect(input).toHaveAttribute('aria-describedby', 'name-error')
    expect(screen.getByRole('alert')).toHaveTextContent('名称不能为空')
  })

  it('blocks changes in disabled and busy states', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    const { rerender } = render(<TextField id="name" label="名称" value="" disabled onChange={onChange} />)
    const input = screen.getByRole('textbox', { name: '名称' })
    expect(input).toHaveAttribute('aria-valuetext', '不可用')
    await user.type(input, 'x')
    expect(onChange).not.toHaveBeenCalled()
    rerender(<TextField id="name" label="名称" value="" busy onChange={onChange} />)
    expect(input).toHaveAttribute('aria-valuetext', '正在加载')
    await user.type(input, 'x')
    expect(onChange).not.toHaveBeenCalled()
  })
})
