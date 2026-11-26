import { createRef } from 'react'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Popover } from './Popover'

describe('Popover', () => {
  it('does not render while closed', () => {
    const anchor = createRef<HTMLButtonElement>()
    render(<Popover open={false} anchor={anchor} id="test" title="测试" onDismiss={() => undefined}>内容</Popover>)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('focuses the first action and dismisses through Escape and outside pointer down', async () => {
    const onDismiss = vi.fn()
    const user = userEvent.setup()
    const anchor = createRef<HTMLButtonElement>()
    render(<div>
      <button ref={anchor} type="button">锚点</button>
      <Popover open anchor={anchor} id="test" title="测试弹层" onDismiss={onDismiss}>
        <button type="button">操作</button>
      </Popover>
    </div>)
    expect(screen.getByRole('dialog', { name: '测试弹层' })).toBeInTheDocument()
    expect(document.activeElement).toBe(screen.getByRole('button', { name: '操作' }))
    await user.keyboard('{Escape}')
    expect(onDismiss).toHaveBeenCalledTimes(1)
    await user.click(document.body)
    expect(onDismiss).toHaveBeenCalledTimes(2)
  })
})
