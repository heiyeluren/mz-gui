# 快速开始

## 环境

- Go 1.26+
- 可选：Node.js 22.12+，仅开发 UI Kit 时需要
- 可选：Task，用于执行 `task test/build/run`
- macOS 打包需要 macOS；Windows 和 Linux 真机状态见版本说明

## 运行示例

```bash
go run ./examples/framework-showcase
```

macOS 标准 APP：

```bash
bash scripts/package-showcase-darwin.sh
open build/MZ-GUI-Showcase.app
```

如需指定输出位置：

```bash
bash scripts/package-showcase-darwin.sh /path/to/MZ-GUI-Showcase.app
```

需要后台任务或窗口外恢复时，运行可选多进程模板：

```bash
go run ./examples/framework-agentd-showcase/agentd -run-dir /tmp/mzgui-agentd-showcase
```

另开终端：

```bash
MZGUI_AGENTD_RUN_DIR=/tmp/mzgui-agentd-showcase go run ./examples/framework-agentd-showcase
```

macOS 也可用 `bash scripts/run-agentd-showcase.sh` 一次构建并启动两个进程。若要在同一示例中切回单进程，设置 `MZGUI_TASK_MODE=inprocess`；前端和 `DesktopService` 不变。详见[示例说明](../examples/framework-agentd-showcase/README.md)。

## 全量验证

```bash
npm --prefix framework/ui-kit ci
make test
make build
make check
```

UI Kit 单独验证：

```bash
cd framework/ui-kit
npm run typecheck
npm test
npm run test:e2e
npm run build
npm pack --dry-run
```

## 新建自己的 APP

默认从单进程开始，只有后台任务、SQLite 单 owner 或高风险隔离需求出现时才启用多进程。选择规则见[单进程与多进程模式](process-models.md)。

1. 复制 `examples/framework-showcase`，保留 Framework 为独立依赖。
2. 修改新 APP 的 `mzgui.app.toml`：`display_name`、应用 ID、协议 scheme、bundle ID、进程名和存储命名空间。
3. 执行 `go run ./tools/mzguiappgen --manifest <你的APP>/mzgui.app.toml --catalog ./framework/capabilities/catalog.json --output <你的APP>/generated/appmeta/manifest_gen.go --package appmeta`，确认生成的 `appmeta` 与能力目录一致。仓库两个示例可统一用 `make generate-app` 重新生成。
4. 在 `main.go` 中替换窗口配置，在 `app.go` 中组合自己的服务，在 `frontend/` 中实现产品页面。
5. 为新 APP 编写生命周期、错误、真实请求和 GUI 交互测试。

Framework 不提供产品数据库、业务 RPC 或账号配置。应用负责注入这些能力。

## 作为 Go 依赖

```bash
go get github.com/heiyeluren/mz-gui@v0.0.4
```

```go
import (
    "github.com/heiyeluren/mz-gui/framework/go/core/clock"
    "github.com/heiyeluren/mz-gui/framework/go/interaction/eventbus"
)
```

公共包不允许反向导入产品应用。产品组合根可以依赖 Framework；Framework 不知道产品存在。

## 作为 React 依赖

将打包后的 `@mz-gui/ui-kit` 安装到宿主前端，并满足 peer dependencies。使用公开出口，不引用 `src` 私有路径：

```tsx
import { Dialog, ModuleRail, ThemeProvider } from '@mz-gui/ui-kit'
import { TerminalSurface } from '@mz-gui/ui-kit/terminal-surface'
import '@mz-gui/ui-kit/styles.css'
```

## 常见命令

| 命令 | 用途 |
| --- | --- |
| `make test` | Go 与 UI Kit 全量测试 |
| `make build` | Go 与 UI Kit 构建 |
| `make check` | 需求矩阵、appmeta 和架构检查 |
| `make run` | 启动 Showcase |
| `make package-darwin` | 生成 macOS `.app` |
| `make generate-app` | 重新生成 Showcase 应用元数据 |
| `task generate:proto` | 重新生成 Framework Protobuf Go 代码 |
