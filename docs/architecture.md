# 架构与依赖边界

## 单向依赖

```text
Application UI → Framework UI Kit
Application Composition Root → Framework Go Packages
Framework → 不包含任何产品应用知识
```

Framework 静态链接进具体应用，不引入额外 Framework 进程。产品页面、DTO、数据库 schema、账号和业务协议属于应用层。

## 公共模块

| 模块 | 职责 |
| --- | --- |
| `core` | 类型、时间、版本、错误、健康、Context、Manifest 与公共目录 |
| `lifecycle` | 组件 DAG、应用宿主、sidecar、单实例、Wails 窗口生命周期 |
| `interaction` | Command / Query / Event、Undo/Redo、输入、快捷键和窗口状态 |
| `runtime` | 任务、调度、重试、资源池、终端、进程与更新 |
| `transport` | Bootstrap、gRPC、流恢复、本机与远程通信 |
| `platform` | 窗口、凭据、权限、启动、文件、WebView 和系统适配 |
| `storage` | SQLite、配置、迁移、Blob、媒体与事务 |
| `security` | Capability、Session、FileGrant、Allowlist、审计与 Web 安全 |
| `observability` | 日志、指标、Trace、Telemetry 和诊断 |
| `adapters` | 插件、浏览器、媒体、消息、对象存储、VCS 与驱动扩展 |
| `testing` | Fake、Fixture、Native E2E 和故障注入 |
| `ui-kit` | 无产品语义的 React 组件和 Design Tokens |

## Showcase 数据流

```text
用户点击
  → frontend/main.js
  → Wails Binding: main.ShowcaseService.EmitSample
  → Showcase Go Service
  → 有界 EventBus
  → Go consumer 更新事件计数
  → 前端重新读取 Snapshot
```

健康状态、生命周期和凭据状态也由真实 Go Snapshot 提供，不由前端伪造。凭据 Backend 是内存实现，避免读取用户真实凭据。

## 资源边界

- 前端静态资源通过 `embed.FS` 打包；Wails 将 `index.html` 所在目录作为资源根。
- Framework Design Tokens 通过 `/framework/tokens.css` 提供。
- APP 自有资源使用根相对路径，例如 `/styles.css`、`/main.js`。
- macOS 使用标准 `.app` 包验证窗口、应用标题和系统行为。

## API 成熟度

`framework/api/public-v1.json` 记录 API 级别，`framework/capabilities/catalog.json` 记录能力成熟度。`candidate` 和 `experimental` 不等同于 `stable` 或 `production-ready`。平台验证状态必须来自对应平台真实证据。

## 测试边界

公共仓只保留自包含单元测试、架构测试和本地示例契约测试。具体产品的调用证明和验收证据由产品仓维护，不让公共 Framework 的测试反向依赖产品源码。
