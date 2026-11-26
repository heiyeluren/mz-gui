# TextField

## 用途

提供受控文本输入、label、required、错误关联、disabled 和 busy 状态。

## Props

| 字段 | 类型 / 默认值 | 说明 |
| --- | --- | --- |
| `id` | `string` | input 与错误元素的关联前缀 |
| `label` | `string` | 字段 accessible name |
| `value` | `string` | 受控输入值 |
| `error` | `string?` | 错误文案，存在时渲染 `role="alert"` |
| `required` | `boolean = false` | 是否必填 |
| `disabled` / `busy` | `boolean = false` | 禁止输入 / 加载状态 |
| `onChange` | `(value: string) => void` | 新输入值回调 |

## 状态与可访问性

- label 通过 `htmlFor` 关联 input。
- 错误通过 `aria-invalid` 和 `aria-describedby` 关联。
- required 使用 `aria-required`。
- disabled/busy 使用 `aria-disabled`、`aria-busy` 和对应状态文案。
- 空值使用统一空态语义。

## 最小示例

```tsx
<TextField
  id="name"
  label="名称"
  value={name}
  required
  error={name ? undefined : '名称不能为空'}
  onChange={setName}
/>
```

## 已知限制

只支持单行文本；复杂校验、提交和异步错误由调用方负责。
