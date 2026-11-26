# Popover

## 用途

在锚点附近创建 portal 弹层，支持视口边界、上下方向选择、焦点进入与恢复，避免被滚动容器裁切。

## Props

| 字段 | 说明 |
| --- | --- |
| `open` | 是否显示 |
| `anchor` | `RefObject<HTMLElement>`，定位与焦点恢复锚点 |
| `placementAnchor` | 可选的独立布局锚点 |
| `id` | 弹层 DOM ID |
| `title` | 弹层 accessible name |
| `className` | 附加 class，默认空字符串 |
| `width` | 期望宽度，默认 `420`，视口内自动收敛 |
| `onDismiss` | Escape、外部点击或外部聚焦时触发 |
| `children` | 弹层内容 |

## 状态与键盘

- 首次可见后聚焦 `[data-autofocus]`、输入框、选择框或首个可用按钮。
- Escape 关闭；Tab 焦点限制在弹层内。
- 外部 pointerdown/focusin 关闭，卸载时恢复锚点焦点。
- 使用 `role="dialog"` 与 `aria-modal="true"`。

## 最小示例

```tsx
const anchor = useRef<HTMLButtonElement>(null)

<button ref={anchor}>打开</button>
<Popover open={open} anchor={anchor} id="settings" title="设置" onDismiss={() => setOpen(false)}>
  <button>操作</button>
</Popover>
```

## 已知限制

依赖 `ResizeObserver`；测试环境需要提供 mock。复杂嵌套弹层的业务层级由调用方协调。
