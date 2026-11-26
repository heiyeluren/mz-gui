# TerminalSurface

## 用途

提供基于 xterm.js 的终端显示、输入、输出游标、尺寸同步、IME、截断提示和会话恢复。业务会话协议由调用方注入。

## Props

| 字段 | 说明 |
| --- | --- |
| `ready` | 是否创建终端 |
| `sessionID` | 当前终端会话 ID，切换时隔离输入和输出 |
| `className` / `ariaLabel` | 样式与 accessible name |
| `showOutputTruncatedNotice` | 是否显示保留范围截断提示 |
| `disabled` / `busy` | 禁止输入 / 加载状态 |
| `focusRequest` | 显式聚焦序号，更新后聚焦且不滚动祖先容器 |
| `theme` / `fontSize` | 可选外观，变化时不重建会话 |
| `getSnapshot` | 按游标读取输出快照 |
| `writeInput` | 写入当前会话输入 |
| `resizeSession` | 同步 cols/rows 到后端 |
| `onSession` | 回传最新会话 |
| `onError` | 回传读取、输入和尺寸错误 |
| `onDimensionsChange` | 回传实际尺寸变化 |

## 状态与键盘

- 合并短时输入，快速切换会话时待发内容仍归原会话。
- 大输出页解析完成后才读取下一页，避免渲染器内存膨胀和空转。
- 支持 IME 组合提交、尺寸变化、退出终端本地重排和截断提示独立展示。
- `role="application"`，disabled/busy 不发送输入，空输出使用统一空态。
- WebGL2 可用时动态加载 GPU 渲染；失败时回退 DOM 渲染，不阻塞终端。

## 最小示例

```tsx
<TerminalSurface
  ready
  sessionID={sessionID}
  getSnapshot={(id, cursor) => api.snapshot(id, cursor)}
  writeInput={(id, data) => api.write(id, data)}
  resizeSession={(id, cols, rows) => api.resize(id, cols, rows)}
  onSession={setSession}
  onError={setError}
/>
```

## 已知限制

依赖 `@xterm/xterm`、`@xterm/addon-fit`、可选 `@xterm/addon-webgl`。业务重连、权限和持久化由调用方实现；测试环境需要 mock xterm 和 ResizeObserver。
