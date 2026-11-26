import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { Dialog } from './Dialog'

describe('Dialog', () => {
  it('does not render while closed', () => {
    render(<Dialog open={false} title="确认" onDismiss={() => undefined}>内容</Dialog>)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('exposes dialog semantics and content', () => {
    render(<Dialog open title="确认操作" onDismiss={() => undefined}>请确认这次更改</Dialog>)
    const dialog = screen.getByRole('dialog', { name: '确认操作' })
    expect(dialog.closest('.mzgui-dialog-backdrop')?.parentElement).toBe(document.body)
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    expect(dialog).toHaveAttribute('aria-describedby', 'mzgui-dialog-body')
    expect(dialog).toHaveTextContent('请确认这次更改')
  })

  it('dismisses through Escape and the close button', async () => {
    const onDismiss = vi.fn()
    const user = userEvent.setup()
    render(<Dialog open title="关闭测试" onDismiss={onDismiss}>面板</Dialog>)
    await user.keyboard('{Escape}')
    await user.click(screen.getByRole('button', { name: '关闭' }))
    expect(onDismiss).toHaveBeenCalledTimes(2)
  })

  it('moves focus into the dialog and restores it on close', async () => {
    const user = userEvent.setup()
    function Host() {
      const [open, setOpen] = useState(false)
      return <div>
        <button type="button" onClick={() => setOpen(true)}>打开</button>
        <Dialog open={open} title="焦点测试" onDismiss={() => setOpen(false)}><button type="button">继续</button></Dialog>
      </div>
    }
    render(<Host />)
    const opener = screen.getByRole('button', { name: '打开' })
    opener.focus()
    await user.click(opener)
    const dialog = screen.getByRole('dialog', { name: '焦点测试' })
    expect(document.activeElement).toBe(dialog)
    await user.keyboard('{Escape}')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(document.activeElement).toBe(opener)
  })

  it.each([
    { disabled: true, busy: false, label: '不可用', attribute: 'aria-disabled' },
    { disabled: false, busy: true, label: '正在加载', attribute: 'aria-busy' },
  ])('blocks dismissal in blocked states', async ({ disabled, busy, label, attribute }) => {
    const onDismiss = vi.fn()
    const user = userEvent.setup()
    render(<Dialog open title="确认操作" disabled={disabled} busy={busy} onDismiss={onDismiss}>内容</Dialog>)
    const dialog = screen.getByRole('dialog', { name: '确认操作' })
    expect(dialog).toHaveAttribute(attribute, 'true')
    expect(dialog).toHaveAttribute('aria-valuetext', label)
    await user.keyboard('{Escape}')
    await user.click(screen.getByRole('button', { name: '关闭' }))
    expect(onDismiss).not.toHaveBeenCalled()
  })
})
