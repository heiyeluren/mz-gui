# Dialog

## 用途

通过 portal 在 `document.body` 渲染模态对话框，提供焦点管理、Escape、关闭按钮和焦点恢复。

## Props

| 字段 | 类型 / 默认值 | 说明 |
| --- | --- | --- |
| `open` | `boolean` | 是否显示，关闭时不渲染 |
| `title` | `string` | 标题，作为 accessible name |
| `dismissLabel` | `string = '关闭'` | 关闭按钮文案 |
| `disabled` | `boolean = false` | 禁止关闭和交互 |
| `busy` | `boolean = false` | 加载状态，同样禁止关闭 |
| `onDismiss` | `() => void` | Escape 或关闭按钮触发 |
| `children` | `ReactNode` | 对话框正文 |

## 状态与键盘

- `role="dialog"`、`aria-modal="true"`，标题与正文通过 `aria-labelledby/describedby` 关联。
- 打开时焦点进入对话框，关闭后恢复原焦点。
- Escape 和关闭按钮调用 `onDismiss`；disabled/busy 时不调用。
- 空标题、空正文和空按钮文案使用统一空态语义。

## 最小示例

```tsx
<Dialog open={open} title="确认操作" onDismiss={() => setOpen(false)}>
  请确认这次更改
</Dialog>
```

## 已知限制

不处理业务提交和嵌套业务流程；调用方负责确定关闭后的页面状态。
