# MediaSurface

## 用途

展示产品无关的音频或视频，只允许 HTTPS 媒体地址，默认不预加载，不包含业务凭据或自动播放逻辑。

## Props

| 字段 | 说明 |
| --- | --- |
| `ariaLabel` | 媒体 accessible name |
| `src` | HTTPS 媒体 URL |
| `kind` | `video`（默认）或 `audio` |
| `emptyLabel` | 非法地址或空态文案 |
| `disabled` / `busy` | 禁止交互 / 加载状态 |

无受控播放事件；播放控制由浏览器原生媒体元素负责。`cancelMedia` 可暂停并释放 `src`。

## 状态与可访问性

- `preload="none"`，只有用户主动操作才加载媒体。
- disabled/busy 时移除 controls，并暴露 `aria-disabled`、`aria-busy`。
- 非法 HTTP、JavaScript、data URI 返回 status 空态，不发起请求。

## 最小示例

```tsx
<MediaSurface ariaLabel="演示视频" src="https://example.com/demo.mp4" kind="video" />
```

## 已知限制

不提供解码、转码、流媒体鉴权、字幕或自定义控制条；这些由宿主应用负责。
