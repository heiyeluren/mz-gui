# CommandPalette

## 用途

提供命令查询、选择、空态和关闭交互。命令定义和执行逻辑由调用方注入。

## Props

| 字段 | 类型 / 默认值 | 说明 |
| --- | --- | --- |
| `open` | `boolean` | 是否显示 |
| `query` | `string` | 受控查询值 |
| `items` | `CommandPaletteItem[]` | 命令列表，含 `id`、`label`、`disabled` |
| `label` | `string = '命令'` | 弹层 accessible name |
| `queryLabel` | `string = '筛选'` | 查询框 accessible name |
| `emptyLabel` | 统一空态文案 | 空列表提示 |
| `disabled` / `busy` | `boolean = false` | 禁止交互 / 加载状态 |
| `onQueryChange` | `(query: string) => void` | 查询变更回调 |
| `onSelect` | `(id: string) => void` | 命令选择回调 |
| `onDismiss` | `() => void` | Escape 关闭回调 |

## 状态与键盘

- 打开时聚焦查询框，Escape 关闭。
- 查询框使用 `combobox`，命令使用 `listbox` / `option`。
- disabled/busy 时禁止查询、选择和关闭。
- 空列表使用 `role="status"` 与统一空态语义。

## 最小示例

```tsx
<CommandPalette
  open={open}
  query={query}
  items={[{ id: 'open', label: '打开' }]}
  onQueryChange={setQuery}
  onSelect={(id) => execute(id)}
  onDismiss={() => setOpen(false)}
/>
```

## 已知限制

不内置模糊搜索、命令执行或业务权限；调用方负责过滤与执行。
