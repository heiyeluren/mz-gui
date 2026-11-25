# MZ-GUI UI Kit

MZ-GUI 的无产品语义 React 组件包，提供 Design Token、主题、导航、表单、反馈、表格、树、虚拟列表、Markdown、代码、图片、媒体、终端和无障碍能力。调用方注入业务文案、图标、路由、状态和品牌资产；本包不依赖任何产品应用、DTO 或协议。

## 使用

```tsx
import { ModuleRail, ThemeProvider } from '@mz-gui/ui-kit'
import { TerminalSurface } from '@mz-gui/ui-kit/terminal-surface'
import '@mz-gui/ui-kit/styles.css'
```

React、React DOM、Mermaid 和 xterm 由宿主应用作为 peer dependencies 提供，避免重复 Runtime。

## 开发

```bash
npm install
npm run typecheck
npm test
npm run build
npm pack --dry-run
```

`npm run build` 输出 ESM JavaScript、类型声明、source map 和 CSS 到 `dist/`。发布内容仅包含 `dist/` 与本文档。
