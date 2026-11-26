# Tree

## 用途

提供递归树形数据、展开/收起、选择、层级和键盘导航。

## Props

| 字段 | 类型 / 默认值 | 说明 |
| --- | --- | --- |
| `ariaLabel` | `string` | 树 accessible name |
| `nodes` | `TreeNode[]` | 递归节点，含 `id`、`label`、`children` |
| `selectedID` | `string?` | 当前选中节点 |
| `expandedIDs` | `string[] = []` | 展开节点集合 |
| `emptyLabel` | 统一空态文案 | 空树提示 |
| `disabled` / `busy` | `boolean = false` | 禁止交互 / 加载状态 |
| `onSelect` | `(id: string) => void` | 选中回调 |
| `onToggle` | `(id: string) => void` | 展开/收起回调 |

## 状态与键盘

- 使用 `tree`、`treeitem`、`group` 语义。
- 节点通过 `aria-level`、`aria-posinset`、`aria-setsize`、`aria-selected`、`aria-expanded` 暴露结构。
- 支持方向键、Home/End、PageUp/PageDown、Enter、Space、`*` 和 typeahead。
- 空树使用 `role="status"`。

## 最小示例

```tsx
<Tree
  ariaLabel="文件树"
  nodes={[{ id: 'root', label: '根节点', children: [{ id: 'child', label: '子节点' }] }]}
  selectedID={selected}
  expandedIDs={expanded}
  onSelect={setSelected}
  onToggle={(id) => toggle(id)}
/>
```

## 已知限制

`expandedIDs` 由调用方控制；组件不负责大数据加载、懒加载或业务过滤。
