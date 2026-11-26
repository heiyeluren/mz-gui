# MermaidDiagram

## 用途

按需加载 Mermaid，把图表渲染为 SVG 图片，严格限制 HTML 标签和事件处理器，不注入应用 DOM。

## Props

| 字段 | 说明 |
| --- | --- |
| `source` | Mermaid 源码 |
| `fallback` | 渲染失败时保留的降级内容 |
| `ariaLabel` | SVG 图片 accessible name，默认 `Mermaid 图表` |

组件没有业务事件；source 变化会重新渲染，卸载会取消过期结果并清理 staging 节点。

## 状态与可访问性

- 加载中显示 `role="status"`，失败显示 `role="alert"` 和 fallback。
- 使用 `securityLevel="strict"`、`htmlLabels=false`，只允许 SVG `<img>`，不创建 iframe。
- Mermaid 是 peer dependency，只有遇到图表时动态加载。

## 最小示例

```tsx
<MermaidDiagram source="graph TD; Start-->Done" ariaLabel="流程图" fallback={<pre>graph TD; Start--&gt;Done</pre>} />
```

## 已知限制

不支持 Mermaid 自定义 HTML、脚本、点击行为或产品主题编辑器；复杂图形的布局由 Mermaid 决定。
