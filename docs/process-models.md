# 单进程与多进程模式

## 默认选择

**单进程是默认模式。** 基本窗口、主题、表单、本地设置、短任务、少量文件操作和简单 CRUD 不应承担 agentd、RPC 与多进程发布成本。

只有出现以下实际需求时，才选择多进程：

- 窗口关闭后仍需继续任务，或进程重启后需要恢复任务。
- 需要 agentd 作为 SQLite 单 owner 或长期任务 owner。
- PTY、浏览器自动化、系统自动化和高风险操作需要进程隔离。
- 插件需要独立 capability、握手和生命周期。

`browser-host` 和 `helper` 始终可选，不与多进程默认包绑定。

## 单进程 Manifest

`agentd` 为空或省略即表示不启用后台进程：

```toml
[processes]
desktop = "example-desktop"
```

`desktop` 必填，`agentd`、`browser_host`、`helper` 均可选。`framework-showcase` 的生成元数据保持 `AgentdProcess = ""`，防止示例看似单进程、实际隐式声明后台进程。

## 多进程 Manifest

只有确定需要后台任务或隔离时才声明：

```toml
[processes]
desktop = "example-desktop"
agentd = "example-agentd"
# browser_host = "example-browser-host"  # 可选
# helper = "example-helper"              # 可选，仅高风险操作需要
```

多进程模板必须证明窗口关闭后任务继续、SQLite 单 owner、握手/session、取消和恢复，不能只启动两个空进程。

## 迁移原则

两种模式复用相同的业务接口：

```text
业务用例 → 同一组端口
单进程   → 进程内调用
多进程   → desktop facade → gRPC/本机传输 → agentd
```

迁移只修改组合根和通信适配，不重写 UI 和业务用例。不得为了统一架构而提前引入多进程。

## 当前实现范围

- 单进程 Showcase：已具备，包含窗口、状态、EventBus、Command/Query、FileGrant、Bootstrap 和真实事件交互。
- 多进程模板与迁移：后续阶段交付，必须增加真实后台任务与恢复证据。

验证：

```bash
go test ./framework/go/core/appmanifest ./tests/architecture ./examples/framework-showcase
```
