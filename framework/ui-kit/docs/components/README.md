# UI Kit 组件文档

每个公开组件都需要用途、props、事件、受控约定、状态矩阵、peer dependencies、最小示例、键盘/ARIA 和已知限制。

## 已覆盖

- [ThemeProvider](ThemeProvider.md)
- [ModuleRail](ModuleRail.md)
- [Dialog](Dialog.md)
- [Popover](Popover.md)
- [CommandPalette](CommandPalette.md)
- [Tabs](Tabs.md)
- [TextField](TextField.md)
- [Select](Select.md)
- [DataTable](DataTable.md)
- [Tree](Tree.md)
- [VirtualList](VirtualList.md)
- [MarkdownViewer](MarkdownViewer.md)
- [TerminalSurface](TerminalSurface.md)
- [MediaSurface](MediaSurface.md)
- [MermaidDiagram](MermaidDiagram.md)

## 状态矩阵

| 组件 | Vitest | 键盘/焦点 | 空态/错误 | 文档 |
| --- | --- | --- | --- | --- |
| ThemeProvider / theme | 通过 | 不适用 | 对比度与品牌 token | 已完成 |
| ModuleRail | 通过 | 选择、禁用 | 徽标、空态 | 已完成 |
| Dialog | 通过 | Escape、焦点恢复 | disabled、busy | 已完成 |
| Popover | 通过 | Escape、首焦点、外部关闭 | 定位与边界 | 已完成 |
| CommandPalette | 通过 | 查询、选择、Escape | 空态、disabled、busy | 已完成 |
| Tabs | 通过 | 方向键、Home/End、typeahead | 空态、disabled、busy | 已完成 |
| TextField / Select | 通过 | 原生输入与选择 | 空态、required、disabled、busy | 已完成 |
| DataTable | 通过 | 方向键、Home/End、typeahead | 排序、空态、disabled、busy | 已完成 |
| Tree | 通过 | 选择、展开、层级、键盘 | 空态、disabled、busy | 已完成 |
| VirtualList | 通过 | 选择、全局位置、大列表窗口 | 空态、disabled、busy | 已完成 |
| MarkdownViewer | 通过 | 链接、代码块、任务列表 | 注入降级、空态、disabled、busy | 已完成 |
| TerminalSurface | 通过 | 输入、resize、IME、恢复 | 截断、错误、disabled、busy、降级 | 已完成 |
| MediaSurface | 通过 | 原生媒体控制 | 非法地址、disabled、busy、取消与释放 | 已完成 |
| MermaidDiagram | 通过 | SVG 图片与错误提示 | 渲染失败、卸载、staging 清理 | 已完成 |
