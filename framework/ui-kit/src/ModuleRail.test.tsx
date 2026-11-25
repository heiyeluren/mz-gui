import { fireEvent, render, screen } from '@testing-library/react'
import { ModuleRail } from './ModuleRail'

describe('ModuleRail', () => {
  it('emits the selected public module id', () => {
    const onSelect = vi.fn()
    render(<ModuleRail
      ariaLabel="公共模块"
      activeID="overview"
      groups={[{ items: [
        { id: 'overview', label: '概览', icon: <span>O</span> },
        { id: 'runtime', label: '运行时', icon: <span>R</span> },
      ] }]}
      onSelect={onSelect}
    />)

    fireEvent.click(screen.getByRole('button', { name: '运行时' }))
    expect(onSelect).toHaveBeenCalledWith('runtime')
    expect(screen.getByRole('button', { name: '概览' })).toHaveAttribute('aria-current', 'page')
  })
})
