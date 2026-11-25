import { Fragment, type ReactNode } from 'react'

import { voiceOverName, voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'

/** W116-G669：对照旧版 MarkdownViewer ui-kit 再对照。旧版无独立 ui-kit MarkdownViewer 入口。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export type MarkdownCodeBlock = {
  text: string
  language: string
}

export type MarkdownCodeBlockRender = (block: MarkdownCodeBlock, defaultNode: ReactNode) => ReactNode

export type MarkdownInlineCode = {
  text: string
}

export type MarkdownInlineCodeRender = (code: MarkdownInlineCode, defaultNode: ReactNode) => ReactNode

export type MarkdownLink = {
  href: string
  label: string
}

/** HTTPS 默认渲染为链接；本地文件引用默认仅展示地址，调用方可附加复制等操作。 */
export type MarkdownLinkRender = (link: MarkdownLink, defaultNode: ReactNode) => ReactNode

export type MarkdownViewerProps = {
  ariaLabel: string
  source: string
  /** 摘录可能从表体开始；仅在调用方明确允许时，将完整管道行作为无表头表格展示。 */
  allowHeaderlessTables?: boolean
  /** 由调用方限定可打开的 HTTP 链接，图片仍须使用 HTTPS。 */
  allowHttpLink?: (href: string) => boolean
  emptyLabel?: string
  disabled?: boolean
  busy?: boolean
  renderCodeBlock?: MarkdownCodeBlockRender
  renderInlineCode?: MarkdownInlineCodeRender
  renderLink?: MarkdownLinkRender
}

type MarkdownInlineOptions = {
  allowHttpLink?: (href: string) => boolean
  renderInlineCode?: MarkdownInlineCodeRender
  renderLink?: MarkdownLinkRender
}

type NestedList = { ordered: boolean; items: string[] }
type ListItem = { text: string; children?: NestedList[] }

type MarkdownBlock =
  | { type: 'heading'; level: 1 | 2 | 3 | 4; text: string }
  | { type: 'paragraph'; text: string }
  | { type: 'list'; ordered: boolean; items: ListItem[] }
  | { type: 'code'; text: string; language: string }
  | { type: 'quote'; text: string }
  | { type: 'table'; headers: string[]; rows: string[][] }

const INLINE_TARGET = /!?\[[^\]]*\]\((?:<[^<>\r\n]+>|[^()\s]+(?:\([^()]*\))?[^()\s]*)\)/
const INLINE_CODE = /`[^`]+`/
const INLINE_TOKEN = new RegExp(`(\\*\\*[^*]+\\*\\*|\\*[^*]+\\*|${INLINE_CODE.source}|${INLINE_TARGET.source})`, 'g')
const INLINE_OR_HTML_TOKEN = new RegExp(`(${INLINE_CODE.source}|${INLINE_TARGET.source})|<[^>]*>`, 'g')
const BARE_HTTPS_TOKEN = /https:\/\/[^\s<>"'，。；？！、]+/gi
const BARE_WEB_TOKEN = /https?:\/\/[^\s<>"'，。；？！、（）]+/gi

export function parseMarkdown(source: string, allowHeaderlessTables = false): MarkdownBlock[] {
  const lines = source.replace(/\r\n/g, '\n').split('\n')
  const blocks: MarkdownBlock[] = []
  let index = 0
  while (index < lines.length) {
    const line = lines[index]
    if (line.trim() === '') {
      index += 1
      continue
    }
    if (line.startsWith('```')) {
      const info = line.slice(3).trim()
      const languageToken = info.split(/\s+/)[0] ?? ''
      const language = /^[\w-]+$/.test(languageToken) ? languageToken : ''
      const body: string[] = []
      index += 1
      while (index < lines.length && !lines[index].startsWith('```')) {
        body.push(lines[index])
        index += 1
      }
      if (index < lines.length) {
        index += 1
      }
      blocks.push({ type: 'code', text: body.join('\n'), language })
      continue
    }
    const heading = /^(#{1,4})\s+(.+)$/.exec(line)
    if (heading) {
      blocks.push({
        type: 'heading',
        level: heading[1].length as 1 | 2 | 3 | 4,
        text: stripTags(heading[2]),
      })
      index += 1
      continue
    }
    if (line.startsWith('>')) {
      const quoteLines: string[] = []
      while (index < lines.length && lines[index].startsWith('>')) {
        quoteLines.push(stripTags(lines[index].replace(/^>\s?/, '')))
        index += 1
      }
      blocks.push({ type: 'quote', text: quoteLines.join('\n') })
      continue
    }
    if (index + 1 < lines.length && isTableRow(line) && isTableSeparator(lines[index + 1])) {
      const headers = splitTableCells(line).map(stripTags)
      index += 2
      const rows: string[][] = []
      while (index < lines.length && isTableRow(lines[index])) {
        const cells = splitTableCells(lines[index]).map(stripTags)
        rows.push(headers.map((_, cellIndex) => cells[cellIndex] ?? ''))
        index += 1
      }
      blocks.push({ type: 'table', headers, rows })
      continue
    }
    if (allowHeaderlessTables && isTableFragment(line)) {
      const rows = [splitTableCells(line).map(stripTags)]
      index += 1
      while (index < lines.length && isTableFragment(lines[index]) && !isTableSeparator(lines[index + 1] ?? '')) {
        rows.push(splitTableCells(lines[index]).map(stripTags))
        index += 1
      }
      blocks.push({ type: 'table', headers: [], rows })
      continue
    }
    if (isListItemLine(line)) {
      const ordered = isOrderedListLine(line)
      const items: ListItem[] = []
      while (index < lines.length && isListItemLine(lines[index])) {
        const current = lines[index]
        const itemOrdered = isOrderedListLine(current)
        const text = listItemText(current)
        if (!isNestedListItem(current) || items.length === 0) {
          if (itemOrdered !== ordered) {
            break
          }
          items.push({ text })
          index += 1
          continue
        }
        appendNestedItem(items[items.length - 1], itemOrdered, text)
        index += 1
      }
      blocks.push({ type: 'list', ordered, items })
      continue
    }
    blocks.push({ type: 'paragraph', text: stripTags(line) })
    index += 1
  }
  return blocks
}

export function MarkdownViewer({
  ariaLabel,
  source,
  allowHeaderlessTables = false,
  allowHttpLink,
  emptyLabel = voiceOverStateLabel('empty'),
  disabled = false,
  busy = false,
  renderCodeBlock,
  renderInlineCode,
  renderLink,
}: MarkdownViewerProps) {
  const blocks = parseMarkdown(source, allowHeaderlessTables)
  const spoken = voiceOverName(ariaLabel)
  const blocked = disabled || busy
  const valuetext = disabled
    ? voiceOverStateLabel('disabled')
    : busy
      ? voiceOverStateLabel('loading')
      : undefined
  const inline: MarkdownInlineOptions = { renderInlineCode, renderLink, allowHttpLink }
  if (blocks.length === 0) {
    return (
      <div className="mzgui-markdown" role="status" aria-label={spoken} aria-roledescription={voiceOverRoleDescription('status')} aria-busy={busy || undefined} aria-disabled={blocked || undefined} aria-valuetext={valuetext ?? emptyLabel}>
        {valuetext ?? emptyLabel}
      </div>
    )
  }
  return (
    <article className="mzgui-markdown" role="article" aria-label={spoken} aria-roledescription={voiceOverRoleDescription('article')} aria-busy={busy || undefined} aria-disabled={blocked || undefined} aria-valuetext={valuetext}>
      {blocks.map((block, index) => renderBlock(block, index, renderCodeBlock, inline))}
    </article>
  )
}

function renderBlock(block: MarkdownBlock, index: number, renderCodeBlock: MarkdownCodeBlockRender | undefined, inline: MarkdownInlineOptions): ReactNode {
  switch (block.type) {
    case 'heading': {
      const children = renderInline(block.text, inline, `h${index}`)
      if (block.level === 1) {
        return <h1 key={index}>{children}</h1>
      }
      if (block.level === 2) {
        return <h2 key={index}>{children}</h2>
      }
      if (block.level === 3) {
        return <h3 key={index}>{children}</h3>
      }
      return <h4 key={index}>{children}</h4>
    }
    case 'quote':
      return (
        <blockquote key={index}>
          {block.text.split('\n').map((line, lineIndex) => (
            <p key={lineIndex}>{renderInline(line, inline, `q${index}-${lineIndex}`)}</p>
          ))}
        </blockquote>
      )
    case 'table':
      return (
        <table key={index}>
          {block.headers.length > 0 && <thead>
            <tr>
              {block.headers.map((header, headerIndex) => (
                <th key={headerIndex}>{renderInline(header, inline, `th${index}-${headerIndex}`)}</th>
              ))}
            </tr>
          </thead>}
          <tbody>
            {block.rows.map((row, rowIndex) => (
              <tr key={rowIndex}>
                {row.map((cell, cellIndex) => (
                  <td key={cellIndex}>{renderInline(cell, inline, `td${index}-${rowIndex}-${cellIndex}`)}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      )
    case 'list':
      return renderList(block, index, inline)
    case 'code': {
      const defaultNode = (
        <pre>
          <code>{block.text}</code>
        </pre>
      )
      const node = renderCodeBlock ? renderCodeBlock({ text: block.text, language: block.language }, defaultNode) : defaultNode
      return <Fragment key={index}>{node}</Fragment>
    }
    default:
      return <p key={index}>{renderInline(block.text, inline, `p${index}`)}</p>
  }
}

function renderList(list: { ordered: boolean; items: ListItem[] }, key: number, inline: MarkdownInlineOptions): ReactNode {
  const ListTag = list.ordered ? 'ol' : 'ul'
  return (
    <ListTag key={key}>
      {list.items.map((item, itemIndex) => (
        <li key={itemIndex}>
          {renderListItem(item.text, inline, `l${key}-${itemIndex}`)}
          {item.children?.map((child, childIndex) => renderNestedList(child, childIndex, inline, `l${key}-${itemIndex}-n${childIndex}`))}
        </li>
      ))}
    </ListTag>
  )
}

function renderNestedList(list: NestedList, key: number, inline: MarkdownInlineOptions, keyPrefix: string): ReactNode {
  const ListTag = list.ordered ? 'ol' : 'ul'
  return (
    <ListTag key={key}>
      {list.items.map((item, itemIndex) => (
        <li key={itemIndex}>{renderListItem(item, inline, `${keyPrefix}-${itemIndex}`)}</li>
      ))}
    </ListTag>
  )
}

function renderListItem(item: string, inline: MarkdownInlineOptions, keyPrefix: string): ReactNode {
  const task = /^\[([ xX])\]\s+(.*)$/.exec(item)
  if (!task) {
    return renderInline(item, inline, keyPrefix)
  }
  const checked = task[1] !== ' '
  return (
    <label>
      <input type="checkbox" disabled checked={checked} onChange={() => undefined} />
      {renderInline(task[2], inline, `${keyPrefix}-task`)}
    </label>
  )
}

function renderInline(text: string, options: MarkdownInlineOptions = {}, keyPrefix = 'i'): ReactNode[] {
  const nodes: ReactNode[] = []
  const pattern = new RegExp(INLINE_TOKEN.source, 'g')
  let last = 0
  let match: RegExpExecArray | null
  let key = 0
  while ((match = pattern.exec(text)) !== null) {
    if (match.index > last) {
      nodes.push(...renderAutoInlineLinks(text.slice(last, match.index), options, `${keyPrefix}-${key}-pre`))
    }
    const token = match[0]
    if (token.startsWith('**')) {
      nodes.push(<strong key={`${keyPrefix}-${key}`}>{renderAutoInlineLinks(token.slice(2, -2), options, `${keyPrefix}-${key}-b`)}</strong>)
    } else if (token.startsWith('*')) {
      nodes.push(<em key={`${keyPrefix}-${key}`}>{renderAutoInlineLinks(token.slice(1, -1), options, `${keyPrefix}-${key}-e`)}</em>)
    } else if (token.startsWith('`')) {
      const code = token.slice(1, -1)
      const defaultNode = <code>{renderAutoInlineLinks(code, {}, `${keyPrefix}-${key}-c`)}</code>
      const node = options.renderInlineCode ? options.renderInlineCode({ text: code }, defaultNode) : defaultNode
      nodes.push(<Fragment key={`${keyPrefix}-${key}`}>{node}</Fragment>)
    } else {
      const media = parseInlineTarget(token)
      if (media?.image) {
        if (isSafeHttpsUrl(media.href)) {
          nodes.push(<img key={`${keyPrefix}-${key}`} src={media.href} alt={media.label} />)
        } else {
          nodes.push(media.label || token)
        }
      } else if (media && canOpenLink(media.href, options)) {
        nodes.push(<Fragment key={`${keyPrefix}-${key}`}>{renderWebLink(media.href, media.label, options)}</Fragment>)
      } else if (media && isLocalFileReference(media.href)) {
        const defaultNode = (
          <span>
            {media.label && media.label !== media.href ? <>{media.label}（<code>{media.href}</code>）</> : <code>{media.href}</code>}
          </span>
        )
        const node = options.renderLink ? options.renderLink({ href: media.href, label: media.label }, defaultNode) : defaultNode
        nodes.push(<Fragment key={`${keyPrefix}-${key}`}>{node}</Fragment>)
      } else {
        nodes.push(media?.label ?? token)
      }
    }
    key += 1
    last = match.index + token.length
  }
  if (last < text.length) {
    nodes.push(...renderAutoInlineLinks(text.slice(last), options, `${keyPrefix}-tail`))
  }
  return nodes
}

function renderAutoInlineLinks(text: string, options: MarkdownInlineOptions, keyPrefix: string): ReactNode[] {
  if (!text) {
    return []
  }
  const nodes: ReactNode[] = []
  const pattern = new RegExp((options.allowHttpLink ? BARE_WEB_TOKEN : BARE_HTTPS_TOKEN).source, 'gi')
  let last = 0
  let match: RegExpExecArray | null
  let key = 0
  while ((match = pattern.exec(text)) !== null) {
    if (match.index > last) {
      nodes.push(text.slice(last, match.index))
    }
    const token = match[0]
    const { body, suffix } = splitTrailingInlinePunctuation(token)
    if (canOpenLink(body, options)) {
      nodes.push(<Fragment key={`${keyPrefix}-${key}`}>{renderWebLink(body, body, options)}</Fragment>)
    } else {
      nodes.push(body)
    }
    if (suffix) {
      nodes.push(suffix)
    }
    key += 1
    last = match.index + token.length
  }
  if (last < text.length) {
    nodes.push(text.slice(last))
  }
  return nodes
}

function canOpenLink(href: string, options: MarkdownInlineOptions): boolean {
  return isSafeHttpsUrl(href) || (href.startsWith('http://') && options.allowHttpLink?.(href) === true)
}

function renderWebLink(href: string, label: string, options: MarkdownInlineOptions): ReactNode {
  const defaultNode = (
    <a href={href} rel="noreferrer noopener">
      {label}
    </a>
  )
  return options.renderLink ? options.renderLink({ href, label }, defaultNode) : defaultNode
}

function splitTrailingInlinePunctuation(value: string): { body: string; suffix: string } {
  const text = String(value || '')
  const match = text.match(/^(.+?)([),.;!?，。；！？、]+)?$/)
  return {
    body: match?.[1] || text,
    suffix: match?.[2] || '',
  }
}

function parseInlineTarget(token: string): { image: boolean; label: string; href: string } | null {
  const image = token.startsWith('![')
  const body = image ? token.slice(2) : token.startsWith('[') ? token.slice(1) : ''
  const split = body.indexOf('](')
  if (split < 0 || !body.endsWith(')')) {
    return null
  }
  const target = body.slice(split + 2, -1)
  return {
    image,
    label: body.slice(0, split).replace(/<[^>]*>/g, ''),
    href: target.startsWith('<') && target.endsWith('>') ? target.slice(1, -1) : target,
  }
}

function isLocalFileReference(href: string): boolean {
  const value = href.trim()
  if (!value || value.startsWith('#')) return false
  if (/^file:\/\//i.test(value) || /^[a-z]:[\\/]/i.test(value)) return true
  // 无协议的相对/绝对路径只显示为文本，不授予文件访问或系统打开权限。
  return !/^[a-z][a-z\d+.-]*:/i.test(value)
}

function isSafeHttpsUrl(href: string): boolean {
  const value = href.trim()
  const lower = value.toLowerCase()
  if (lower.startsWith('javascript:') || lower.includes('javascript:')) {
    return false
  }
  return lower.startsWith('https://')
}

function splitTableCells(line: string): string[] {
  let value = line.trim()
  if (value.startsWith('|')) {
    value = value.slice(1)
  }
  if (value.endsWith('|')) {
    value = value.slice(0, -1)
  }
  const cells = ['']
  // 代码片段和转义竖线属于单元格正文。
  for (const token of value.split(/(`[^`]*`|\\\||\|)/)) {
    if (token === '|') cells.push('')
    else cells[cells.length - 1] += token === '\\|' ? '|' : token
  }
  return cells.map(cell => cell.trim())
}

function isTableFragment(line: string): boolean {
  const value = line.trim()
  return value.startsWith('| ') && value.endsWith(' |') && splitTableCells(value).length >= 2 && !isTableSeparator(value)
}

function isTableSeparator(line: string): boolean {
  const cells = splitTableCells(line)
  return cells.length > 0 && cells.every((cell) => /^:?-+:?$/.test(cell.replace(/\s+/g, '')))
}

function isTableRow(line: string): boolean {
  return line.includes('|') && line.trim() !== '' && !isTableSeparator(line)
}

function stripTags(text: string): string {
  // 链接目标和代码里的尖括号属于正文，先保护这些 token，再清理真正的 HTML。
  // 后续全部经 React 文本节点输出；保留地址不等于允许 HTML 执行或文件导航。
  return text.replace(INLINE_OR_HTML_TOKEN, (_match, inline: string | undefined) => inline ?? '')
}

function isListItemLine(line: string): boolean {
  return /^\s*[-*]\s+/.test(line) || /^\s*\d+\.\s+/.test(line)
}

function isOrderedListLine(line: string): boolean {
  return /^\s*\d+\.\s+/.test(line)
}

function listItemText(line: string): string {
  return stripTags(line.replace(/^\s*(?:[-*]|\d+\.)\s+/, ''))
}

function listMarkerIndent(line: string): number {
  let width = 0
  for (const ch of line) {
    if (ch === ' ') {
      width += 1
      continue
    }
    if (ch === '\t') {
      width += 2
      continue
    }
    break
  }
  return width
}

function isNestedListItem(line: string): boolean {
  return listMarkerIndent(line) >= 2
}

function appendNestedItem(parent: ListItem, ordered: boolean, text: string): void {
  const children = parent.children ?? []
  const last = children[children.length - 1]
  if (last && last.ordered === ordered) {
    last.items.push(text)
    return
  }
  children.push({ ordered, items: [text] })
  parent.children = children
}
