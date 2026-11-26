# Select

## 用途

提供原生 select 的受控封装，支持选中值、选项文案、空态、disabled 和 busy。

## Props

| 字段 | 类型 / 默认值 | 说明 |
| --- | --- | --- |
| `id` | `string?` | 可选 DOM ID，默认 `mzgui-select` |
| `label` | `string` | 字段 accessible name |
| `value` | `T extends string` | 当前选中值 |
| `options` | `SelectOption<T>[]` | 含 `value`、`label` 的选项 |
| `disabled` / `busy` | `boolean = false` | 禁止切换 / 加载状态 |
| `onChange` | `(value: T) => void` | 新选中值回调 |

## 状态与可访问性

- 使用原生 `combobox` 语义，保留平台原生键盘行为。
- 选中项通过 `aria-valuetext` 暴露；值不在选项中时使用空态。
- disabled/busy 时不触发 `onChange`。
- 选项提供 `aria-posinset` 和 `aria-setsize`。

## 最小示例

```tsx
<Select
  id="view"
  label="视图"
  value={view}
  options={[{ value: 'list', label: '列表' }, { value: 'grid', label: '网格' }]}
  onChange={setView}
/>
```

## 已知限制

这是原生 select，不是自定义弹层；复杂搜索和多选使用其他组件实现。
