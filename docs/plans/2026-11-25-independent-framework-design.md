# MZ-GUI 独立框架设计

日期：2026-11-25
状态：已按用户目标确认实施

## 目标

将 `mozi-gui-app` 中通用的 MZ-GUI-Framework 抽取为独立开源仓库，提供完整的公共 Go 包、React UI Kit、协议与生成代码、通用工具、文档和示例。仓库克隆后可独立测试、构建并运行一个标题为「黑夜路人的 APP」的基础桌面程序，最终发布 `main` 与 `v0.0.1`。

## 方案取舍

1. 采用保留 Go module 与包路径的轻量拆仓。现有 `github.com/heiyeluren/mz-gui/framework/...` import 不变，避免无关重写。
2. 不采用全仓复制。Mozi 业务、内部服务、产品证据、发布凭据与产品构建链不进入公共仓。
3. 不采用首轮目录扁平化。`framework/`、`gen/`、`examples/`、`tools/` 保持现状，降低抽取引入的兼容风险。

## 仓库结构

- `framework/go/`：生命周期、交互、运行时、通信、平台、存储、安全、可观测性、适配器与 Testing SDK。
- `framework/ui-kit/`：无产品语义的 React 组件、主题和样式。
- `framework/proto/` 与 `gen/go/mzgui/`：公共协议源及生成代码。
- `framework/api/`、`framework/capabilities/`、`framework/appmanifest/`：公共契约与能力目录。
- `examples/framework-showcase/`：可独立运行的桌面 APP 与第二调用方。
- `examples/framework-plugin-echo/`：公共 Plugin SDK 示例。
- `tools/`：仅保留公共生成、检查、签名与诊断工具，不保留 Mozi 专属工具。
- `tests/architecture/`：独立边界与无产品耦合门禁。
- `docs/`：使用、架构、设计和实施记录。

## 独立 APP

Showcase 使用 Wails v3、Framework 生命周期、健康检查、EventBus、凭据抽象和 Design Tokens。界面包含产品标题「黑夜路人的 APP」、公共能力导航、框架状态卡片、事件交互和开发入口说明，不依赖 Mozi 页面、数据或后台进程。前端使用静态 HTML/CSS/JavaScript 与 Wails Binding，保证克隆后无需先安装 npm 依赖即可构建运行。

## 清理边界

- 不复制 `apps/`、根 `internal/`、Mozi manifest、浏览器扩展、产品脚本与内部文档。
- Mozi 消费方证明留在原产品仓，公共仓只保留自包含单元测试和本地示例契约测试。
- 录音文件前缀通过 Framework 配置注入，默认品牌中立；Mozi 兼容值由产品调用方负责。
- UI Kit 增加独立 TypeScript 构建配置、声明文件与 npm 包出口，不使用对 Mozi 前端目录的相对 alias。

## 验证

- Go：格式化、独立 `go test ./...`、showcase 测试、桌面 APP 构建和仓外消费者构建。
- Web：UI Kit 独立 TypeScript 检查与 `npm pack` 内容检查。
- 架构：禁止 Framework 导入 Mozi 产品包，禁止产品内部路径和内部服务地址。
- 运行：实际启动 Showcase，核对标题、主要控件与一次 Wails 事件调用。
- 发布：检查 Git 工作区干净，推送 `main`，创建并推送 `v0.0.1`，回读远端分支与 tag SHA。

## 非目标

- 不修改 GitLab 仓库或其分支。
- 不在本版本迁移 Mozi APP 到外部依赖。
- 不宣称 macOS Intel、Windows 或 Linux 真机验证已完成。
- 不将 candidate/experimental API 描述为稳定生产承诺。
