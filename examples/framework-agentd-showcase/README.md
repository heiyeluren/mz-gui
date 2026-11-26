# 单进程 / 多进程任务模板

这个示例用同一个 `DesktopService`、同一份前端和同一个 `TaskBackend` 接口运行两种部署模式。示例任务属于通用框架演示，不包含具体产品业务。

## 默认单进程

```bash
MZGUI_TASK_MODE=inprocess go run ./examples/framework-agentd-showcase
```

`localTaskBackend` 在桌面进程内打开任务存储并执行任务。适合短任务和简单应用；桌面退出后任务运行时也随进程结束。

## 可选多进程

先启动 agentd，再启动桌面，两条命令使用相同的绝对运行目录：

```bash
go run ./examples/framework-agentd-showcase/agentd -run-dir /tmp/mzgui-agentd-showcase
```

```bash
MZGUI_AGENTD_RUN_DIR=/tmp/mzgui-agentd-showcase go run ./examples/framework-agentd-showcase
```

macOS 可一次构建 `.app`、启动两个进程并打开窗口：

```bash
bash scripts/run-agentd-showcase.sh
```

脚本退出时会清理它启动的 agentd。需要观察窗口外续跑时，可直接使用上面的两条命令，或运行集成测试。

## 迁移边界

```text
frontend → DesktopService → TaskBackend
                              ├─ localTaskBackend：进程内调用
                              └─ agentClient：Bootstrap + session + 本机 gRPC → agentd
```

迁移只改变 `main.go` 的组合根。`DesktopService`、前端和任务操作不变；`internal/taskexample` 在两种模式中复用同一个任务引擎配置。多进程模式下只有 agentd 打开 SQLite，桌面没有数据库句柄。

agentd 的 Bootstrap 校验进程身份、应用与构建信息，并创建 session。TaskService 拦截器验证 session 凭据，分别检查 `task.read`、`task.write` 和 `task.cancel`。客户端关闭时撤销 session；这不会停止 agentd 或任务。

## 可验证行为

```bash
go test -v ./examples/framework-agentd-showcase/...
```

集成测试会构建并启动真实 agentd 子进程，验证：

- 桌面 session 关闭后，agentd 继续执行并成功完成持久任务。
- 无有效 session 的调用被拒绝。
- 任务可取消，完成与取消结果都会写入 SQLite。
- agentd 使用同一运行目录重启后，可以读回之前的任务结果。
- 同一 `DesktopService` 可以直接使用进程内后端，不需要另一套 UI 用例。

生成应用元数据：

```bash
go run ./tools/mzguiappgen \
  --manifest ./examples/framework-agentd-showcase/mzgui.app.toml \
  --catalog ./framework/capabilities/catalog.json \
  --output ./examples/framework-agentd-showcase/generated/appmeta/manifest_gen.go \
  --package appmeta
```

修改 `proto/task/v1/task.proto` 后，按仓库 `Taskfile.yml` 的 `generate:proto` 任务同时重新生成 Framework 与本示例协议。不要手工编辑 `gen/` 或 `generated/`。
