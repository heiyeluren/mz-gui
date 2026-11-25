import { pageIndex, typeaheadIndex } from './focus'
import { voiceOverCountText, voiceOverName, voiceOverRoleDescription, voiceOverStateLabel } from './voiceover'

/** W116-G681：对照旧版 Tree 再对照。旧版无独立 ui-kit Tree 入口。诚实不画。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export type TreeNode = {
  id: string
  label: string
  children?: TreeNode[]
}

export type TreeProps = {
  ariaLabel: string
  nodes: TreeNode[]
  selectedID?: string
  expandedIDs?: string[]
  emptyLabel?: string
  disabled?: boolean
  busy?: boolean
  onSelect?: (id: string) => void
  onToggle?: (id: string) => void
}

type FlatTreeNode = {
  id: string
  label: string
  parentID?: string
  hasChildren: boolean
}

function flattenVisible(nodes: TreeNode[], expandedIDs: string[], parentID?: string): FlatTreeNode[] {
  const rows: FlatTreeNode[] = []
  for (const node of nodes) {
    const hasChildren = Boolean(node.children?.length)
    rows.push({ id: node.id, label: node.label, parentID, hasChildren })
    if (hasChildren && expandedIDs.includes(node.id) && node.children) {
      rows.push(...flattenVisible(node.children, expandedIDs, node.id))
    }
  }
  return rows
}

export function Tree({ ariaLabel, nodes, selectedID, expandedIDs = [], emptyLabel = voiceOverStateLabel('empty'), disabled = false, busy = false, onSelect, onToggle }: TreeProps) {
  const visible = flattenVisible(nodes, expandedIDs)
  const spoken = voiceOverName(ariaLabel)
  const blocked = disabled || busy
  const valuetext = disabled
    ? voiceOverStateLabel('disabled')
    : busy
      ? voiceOverStateLabel('loading')
      : voiceOverCountText(visible.length)
  if (nodes.length === 0) {
    return (
      <div className="mzgui-tree" role="status" aria-label={spoken} aria-roledescription={voiceOverRoleDescription('status')} aria-busy={busy || undefined} aria-disabled={blocked || undefined} aria-valuetext={disabled ? voiceOverStateLabel('disabled') : busy ? voiceOverStateLabel('loading') : emptyLabel}>
        {disabled ? voiceOverStateLabel('disabled') : busy ? voiceOverStateLabel('loading') : emptyLabel}
      </div>
    )
  }
  return (
    <ul
      className="mzgui-tree"
      role="tree"
      tabIndex={0}
      aria-label={spoken}
      aria-roledescription={voiceOverRoleDescription('tree')}
      aria-busy={busy || undefined}
      aria-disabled={blocked || undefined}
      aria-valuetext={valuetext}
      onKeyDown={(event) => {
        if (blocked || visible.length === 0) {
          return
        }
        const current = visible.findIndex((row) => row.id === selectedID)
        const row = current >= 0 ? visible[current] : visible[0]
        if (event.key === 'ArrowDown') {
          event.preventDefault()
          onSelect?.(visible[Math.min(visible.length - 1, Math.max(0, current) + 1)].id)
        } else if (event.key === 'ArrowUp') {
          event.preventDefault()
          onSelect?.(visible[Math.max(0, current < 0 ? 0 : current - 1)].id)
        } else if (event.key === 'Home') {
          event.preventDefault()
          onSelect?.(visible[0].id)
        } else if (event.key === 'End') {
          event.preventDefault()
          onSelect?.(visible[visible.length - 1].id)
        } else if (event.key === 'PageDown' || event.key === 'PageUp') {
          event.preventDefault()
          const next = pageIndex(current, event.key === 'PageDown' ? 1 : -1, visible.length)
          if (next >= 0) {
            onSelect?.(visible[next].id)
          }
        } else if (event.key === 'ArrowRight') {
          event.preventDefault()
          if (row.hasChildren && !expandedIDs.includes(row.id)) {
            onToggle?.(row.id)
            return
          }
          const next = visible[current + 1]
          if (next?.parentID === row.id) {
            onSelect?.(next.id)
          }
        } else if (event.key === 'ArrowLeft') {
          event.preventDefault()
          if (row.hasChildren && expandedIDs.includes(row.id)) {
            onToggle?.(row.id)
            return
          }
          if (row.parentID) {
            onSelect?.(row.parentID)
          }
        } else if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault()
          onSelect?.(row.id)
          if (row.hasChildren) {
            onToggle?.(row.id)
          }
        } else if (event.key === '*') {
          event.preventDefault()
          for (const sibling of visible) {
            if (sibling.parentID === row.parentID && sibling.hasChildren && !expandedIDs.includes(sibling.id)) {
              onToggle?.(sibling.id)
            }
          }
        } else {
          const next = typeaheadIndex(visible.map((item) => item.label), current, event.key)
          if (next >= 0) {
            event.preventDefault()
            onSelect?.(visible[next].id)
          }
        }
      }}
    >
      {nodes.map((node, index) => (
        <TreeItem
          key={node.id}
          node={node}
          level={1}
          posinset={index + 1}
          setsize={nodes.length}
          selectedID={selectedID}
          expandedIDs={expandedIDs}
          blocked={blocked}
          onSelect={onSelect}
          onToggle={onToggle}
        />
      ))}
    </ul>
  )
}

function TreeItem({
  node,
  level,
  posinset,
  setsize,
  selectedID,
  expandedIDs,
  blocked,
  onSelect,
  onToggle,
}: {
  node: TreeNode
  level: number
  posinset: number
  setsize: number
  selectedID?: string
  expandedIDs: string[]
  blocked: boolean
  onSelect?: (id: string) => void
  onToggle?: (id: string) => void
}) {
  const hasChildren = Boolean(node.children?.length)
  const expanded = expandedIDs.includes(node.id)
  const spoken = voiceOverName(node.label)
  return (
    <li
      role="treeitem"
      aria-label={spoken}
      aria-level={level}
      aria-posinset={posinset}
      aria-setsize={setsize}
      aria-selected={node.id === selectedID}
      aria-expanded={hasChildren ? expanded : undefined}
    >
      <button data-click-name="选择树节点"
        type="button"
        className="mzgui-tree-label"
        tabIndex={-1}
        disabled={blocked}
        aria-disabled={blocked || undefined}
        onClick={() => {
          if (blocked) {
            return
          }
          onSelect?.(node.id)
          if (hasChildren) {
            onToggle?.(node.id)
          }
        }}
      >
        {spoken}
      </button>
      {hasChildren && expanded ? (
        <ul role="group">
          {node.children?.map((child, index) => (
            <TreeItem
              key={child.id}
              node={child}
              level={level + 1}
              posinset={index + 1}
              setsize={node.children?.length ?? 0}
              selectedID={selectedID}
              expandedIDs={expandedIDs}
              blocked={blocked}
              onSelect={onSelect}
              onToggle={onToggle}
            />
          ))}
        </ul>
      ) : null}
    </li>
  )
}
