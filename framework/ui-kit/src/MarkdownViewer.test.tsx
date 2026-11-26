import { render, screen } from '@testing-library/react'
import { MarkdownViewer, parseMarkdown } from './MarkdownViewer'

describe('MarkdownViewer', () => {
  it('renders an empty state', () => {
    render(<MarkdownViewer ariaLabel="说明" source={'   \n'} />)
    expect(screen.getByRole('status', { name: '说明' })).toHaveTextContent('空')
  })

  it('renders headings and a quote', () => {
    render(<MarkdownViewer ariaLabel="说明" source={'# 一\n## 二\n> 结论：**通过**'} />)
    expect(screen.getByRole('heading', { level: 1, name: '一' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 2, name: '二' })).toBeInTheDocument()
    expect(document.querySelector('blockquote strong')).toHaveTextContent('通过')
  })

  it('renders GFM tables and strips HTML', () => {
    const source = '| 名称 | 值 |\n| --- | --- |\n| <b>a</b> | **1** |'
    render(<MarkdownViewer ariaLabel="说明" source={source} />)
    expect(screen.getByRole('columnheader', { name: '名称' })).toBeInTheDocument()
    expect(screen.getByRole('cell', { name: 'a' })).toBeInTheDocument()
    expect(document.querySelector('td b')).toBeNull()
    expect(screen.getByRole('cell', { name: '1' }).querySelector('strong')).toHaveTextContent('1')
  })

  it('renders task lists without enabling checkboxes', () => {
    render(<MarkdownViewer ariaLabel="说明" source={'- [ ] 待办\n- [x] 完成\n- 普通'} />)
    const pending = screen.getByRole('checkbox', { name: '待办' })
    const done = screen.getByRole('checkbox', { name: '完成' })
    expect(pending).not.toBeChecked()
    expect(done).toBeChecked()
    expect(pending).toBeDisabled()
    expect(done).toBeDisabled()
    expect(screen.getByText('普通')).toBeInTheDocument()
  })

  it('allows HTTPS links and downgrades javascript links to text', () => {
    render(<MarkdownViewer ariaLabel="说明" source={'[安全](https://example.com/a) [危险](javascript:alert(1))'} />)
    expect(screen.getByRole('link', { name: '安全' })).toHaveAttribute('href', 'https://example.com/a')
    expect(screen.queryByRole('link', { name: '危险' })).toBeNull()
    expect(screen.getByText('危险')).toBeInTheDocument()
  })

  it('keeps local file references visible without navigation', () => {
    render(<MarkdownViewer ariaLabel="说明" source={'[报告](/tmp/report.md) [相对](./report.md)'} />)
    expect(screen.getByRole('article', { name: '说明' })).toHaveTextContent('/tmp/report.md')
    expect(screen.getByRole('article', { name: '说明' })).toHaveTextContent('./report.md')
    expect(screen.queryByRole('link')).toBeNull()
  })

  it('loads only HTTPS images', () => {
    render(<MarkdownViewer ariaLabel="说明" source={'![ok](https://example.com/a.png) ![data](data:image/png;base64,aaaa) ![js](javascript:alert(1))'} />)
    const article = screen.getByRole('article', { name: '说明' })
    expect(screen.getByRole('img', { name: 'ok' })).toHaveAttribute('src', 'https://example.com/a.png')
    expect(screen.queryByRole('img', { name: 'data' })).toBeNull()
    expect(screen.queryByRole('img', { name: 'js' })).toBeNull()
    expect(article).toHaveTextContent('data')
    expect(article).toHaveTextContent('js')
  })

  it('keeps code fences as text instead of executing HTML', () => {
    render(<MarkdownViewer ariaLabel="说明" source={'```html\n<script>alert(1)</script>\n```'} />)
    expect(document.querySelector('pre code')).toHaveTextContent('<script>alert(1)</script>')
    expect(document.querySelector('script')).toBeNull()
  })

  it('supports code block render ports', () => {
    render(<MarkdownViewer ariaLabel="说明" source={'```js\nconst x = 1\n```'} renderCodeBlock={(block, defaultNode) => <div data-testid="slot" data-language={block.language}>{defaultNode}</div>} />)
    expect(screen.getByTestId('slot')).toHaveAttribute('data-language', 'js')
    expect(screen.getByTestId('slot').querySelector('pre code')).toHaveTextContent('const x = 1')
  })

  it('parses nested lists and fence languages', () => {
    expect(parseMarkdown('- a\n  - b')).toEqual([{ type: 'list', ordered: false, items: [{ text: 'a', children: [{ ordered: false, items: ['b'] }] }] }])
    expect(parseMarkdown('```js\nconst x = 1\n```')[0]).toEqual({ type: 'code', text: 'const x = 1', language: 'js' })
  })
})
