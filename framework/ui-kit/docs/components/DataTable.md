# DataTable

## 用途

提供数据表格、可排序表头、行数量、位置、键盘导航和空态。

## Props

| 字段 | 类型 / 默认值 | 说明 |
| --- | --- | --- |
| `caption` | `string` | 表格 accessible name |
| `columns` | `DataTableColumn<K>[]` | 列定义，含 `key`、`header`、`sortable` |
| `rows` | `Array<Record<K, string>>` | 行数据 |
| `sortKey` | `K?` | 当前排序列 |
| `sortDirection` | `'asc' = 'asc'` / `'desc'` | 排序方向 |
| `disabled` / `busy` | `boolean = false` | 禁止交互 / 加载状态 |
| `onSort` | `(key: K) => void` | 排序请求回调 |

## 状态与键盘

- `caption` 作为表格 accessible name。
- 当前排序列使用 `aria-sort="ascending/descending"`。
- 行支持方向键、Home/End、PageUp/PageDown 和 typeahead。
- 行通过 `aria-posinset`、`aria-setsize` 暴露全局位置。
- 空表格和空单元格使用统一空态语义。

## 最小示例

```tsx
<DataTable
  caption="文件"
  columns={[{ key: 'name', header: '名称', sortable: true }]}
  rows={[{ name: 'a.txt' }]}
  sortKey="name"
  sortDirection="asc"
  onSort={(key) => setSort(key)}
/>
```

## 已知限制

组件不负责实际排序、分页和业务权限；调用方必须处理 `onSort` 并提供排序后的 rows。
