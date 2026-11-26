# ThemeProvider

## 用途

为应用注入 light、dark、system、high-contrast 主题和文本方向，统一 Design Tokens，不包含产品品牌默认值。

## Props

| 字段 | 类型 / 默认值 | 说明 |
| --- | --- | --- |
| `mode` | `ThemeMode` | 主题模式，必填 |
| `revision` | `number = 1` | 主题快照 revision，切换时原子更新 |
| `prefersDark` | `boolean = false` | system 模式下是否偏向 dark |
| `locale` | `string = 'zh-CN'` | 决定 LTR/RTL |
| `disabled` | `boolean = false` | 标记不可用 |
| `busy` | `boolean = false` | 标记加载中 |
| `children` | `ReactNode` | 应用内容 |

## 状态与可访问性

- 支持 light、dark、high-contrast、reduced-motion、键盘焦点和中英文长文本。
- 更新 `documentElement` 的主题变量、`dir`、`aria-valuetext`、`aria-disabled`、`aria-busy`。
- `createThemeSnapshot` 返回独立 token 副本，品牌 token 必须通过 `validateBrandTokens` 检查对比度。

## 最小示例

```tsx
import { ThemeProvider } from '@mz-gui/ui-kit'
import '@mz-gui/ui-kit/styles.css'

export function App() {
  return <ThemeProvider mode="system" revision={1} locale="zh-CN">
    <main>黑夜路人的 APP</main>
  </ThemeProvider>
}
```

## 已知限制

不提供产品主题编辑器；品牌覆盖由宿主应用注入。SSR 场景需要确保 `document` 可用。
