# MarkdownViewer

## 用途

解析受控 Markdown，支持标题、段落、引用、列表、任务、表格、行内/围栏代码、HTTPS 链接和图片。默认剥离 HTML，不把输入当成可执行页面。

## Props

| 字段 | 类型 / 默认值 | 说明 |
| --- | --- | --- |
| `ariaLabel` | `string` | 文章 accessible name |
| `source` | `string` | Markdown 原文 |
| `allowHeaderlessTables` | `boolean = false` | 明确允许无表头管道表格 |
| `allowHttpLink` | `(href: string) => boolean?` | 额外允许指定 HTTP 链接，图片仍只允许 HTTPS |
| `emptyLabel` | 统一空态文案 | 空文档提示 |
| `disabled` / `busy` | `boolean = false` | 禁止交互 / 加载状态 |
| `renderCodeBlock` | 可选渲染端口 | 替换围栏代码展示，保留默认节点 |
| `renderInlineCode` | 可选渲染端口 | 替换行内代码展示 |
| `renderLink` | 可选渲染端口 | 处理本地文件引用，不默认打开 |

## 安全与可访问性

- HTTPS 链接正常渲染；`javascript:`、data URL 和未授权导航降级为文本。
- 本地文件引用只展示完整路径，不自动打开；由调用方决定复制或其他动作。
- 图片只允许 HTTPS，data/javascript 来源只显示 alt。
- HTML 标签、脚本和表格单元格 HTML 被剥离，代码块保持字面文本。
- 空文档使用 `role="status"`，正常文档使用 `article`。

## 最小示例

```tsx
<MarkdownViewer
  ariaLabel="说明"
  source={'# 标题\n[文档](https://example.com/doc)\n```js\nconst x = 1\n```'}
/>
```

## 已知限制

解析器不是完整 CommonMark 实现；无表头表格、嵌套列表和渲染端口必须显式启用。复杂 HTML 和自定义协议由调用方另行净化。
