# Tabs

## 用途

提供受控标签页，支持单选、单点 Tab 停靠、方向键、Home/End、PageUp/PageDown 和 typeahead。

## Props

| 字段 | 类型 / 默认值 | 说明 |
| --- | --- | --- |
| `ariaLabel` | `string` | 标签组 accessible name |
| `value` | `T extends string` | 当前选中值 |
| `options` | `TabOption<T>[]` | 含 `value`、`label` 的选项 |
| `emptyLabel` | 统一空态文案 | 无选项提示 |
| `disabled` / `busy` | `boolean = false` | 禁止切换 / 加载状态 |
| `onChange` | `(value: T) => void` | 选中值变化回调 |

## 状态与键盘

- `role="tablist"` 与 `role="tab"`，当前项 `aria-selected="true"`。
- 只有当前项 `tabIndex=0`，其余为 `-1`。
- 支持方向键、Home/End、PageUp/PageDown 和首字符匹配。
- 空选项使用 `role="status"`，disabled/busy 时不切换。

## 最小示例

```tsx
<Tabs
  ariaLabel="视图"
  value={view}
  options={[{ value: 'list', label: '列表' }, { value: 'grid', label: '网格' }]}
  onChange={setView}
/>
```

## 已知限制

组件只负责标签选择与焦点，不负责标签页内容渲染和路由。
