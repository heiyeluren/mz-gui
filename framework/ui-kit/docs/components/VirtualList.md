# VirtualList

## 用途

按视口和 overscan 渲染大列表窗口，支持选择、全局位置和键盘导航，避免一次渲染全部条目。

## Props

| 字段 | 类型 / 默认值 | 说明 |
| --- | --- | --- |
| `ariaLabel` | `string` | 列表 accessible name |
| `items` | `VirtualListItem[]` | 含 `id`、`label` 的完整数据 |
| `selectedID` | `string?` | 当前选中项 |
| `height` | `number` | 视口高度，非正值时回退到行高 |
| `itemHeight` | `number` | 行高，非正值时回退到 `32` |
| `overscan` | `number = 2` | 视口上下额外渲染行数 |
| `emptyLabel` | 统一空态文案 | 空列表提示 |
| `disabled` / `busy` | `boolean = false` | 禁止选择 / 加载状态 |
| `onSelect` | `(id: string) => void` | 选中回调 |

## 状态与键盘

- 使用 `listbox` 和 `option`，通过 `aria-activedescendant` 关联当前项。
- 支持方向键、Home/End、PageUp/PageDown 和 typeahead。
- 通过 `aria-posinset`、`aria-setsize` 暴露全局位置。
- 空列表使用 `role="status"`。

## 最小示例

```tsx
<VirtualList
  ariaLabel="文件"
  items={files}
  selectedID={selected}
  height={320}
  itemHeight={32}
  onSelect={setSelected}
/>
```

## 已知限制

调用方需提供完整 items 和稳定 ID；组件不负责数据加载、动态行高或滚动位置持久化。
