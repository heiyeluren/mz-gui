# API 版本与兼容策略

## 版本模型

MZ-GUI 区分三种版本，不能混用：

- `release_version`：产品发布版本，当前为 `0.0.2`。
- `api_schema_version`：公共 API 与治理文档的 schema 版本，当前为 `1.0.0`。
- `core-v1.json`：最小核心 API 清单及其基线文件，不是第三个版本号。

Go module 发布必须使用 `vX.Y.Z` 形式的 tag，例如 `v0.0.2`。`mz-gui-vX.Y.Z` 可以作为产品发布标签，但不能替代 Go module 版本 tag。UI Kit 的 npm 版本与 `release_version` 对齐。

`framework/api/core-v1.json` 是当前核心清单的机器事实源，`framework/api/baselines/v0.0.1.json` 是兼容比较基线。修改公共 API 时同步更新 `public-v1.json`、调用方清单和基线 diff，不得只修改代码而漏掉契约。

## 稳定性级别

| 级别 | 兼容承诺 | 默认生产使用 |
| --- | --- | --- |
| `stable` | 同主版本只做向后兼容追加；弃用至少提前两个 minor 版本并附迁移指南 | 可以 |
| `candidate` | 允许小幅破坏，但必须有迁移说明、兼容窗口和升级示例 | 评估后 |
| `experimental` | 可以调整或移除，不承诺兼容 | 否 |
| `deprecated` | 保留至声明的移除版本，使用 replacement 和 migration guide 迁移 | 仅限兼容期 |

不得为了完成发布而批量把 API 标为 `stable`。只有具有公共文档、两个真实消费者、契约测试和兼容证据的接口才能升档。

## 当前最小核心面

`core-v1.json` 当前冻结以下已登记接口：

- `core.clock`：系统 UTC 时钟。
- `core.fault`：错误 Mapper、默认 Mapper 和 reason 绑定。
- `core.health`：内建健康 Registry 和 Service panic marker。
- `core.model`：请求上下文、Caller、Request ID、RequestMeta 和语义化版本解析。
- `lifecycle`：依赖 DAG Supervisor、Wails 方法边界和受保护调用。
- `interaction`：CommandBus、QueryBus、EventBus、CommandUndo、Undo/Redo 和窗口状态的 Memory/SQLite Store。
- `platform`：运行目录诊断和可注入 Backend 的凭据 Store。
- `storage.sqlite`：SQLite 打开、migration bundle 与备份。
- `security.filegrant`：按主体、来源和有效期策略校验文件授权。
- `transport`：gRPC stream 边界、本机 Endpoint 校验和 Bootstrap Handshake。

SQLite open/migration 已进入最小核心面，当前为 `candidate`，由默认 Showcase、多进程模板及独立 storage 消费者共同验证。streamresume 的 `New`、`Frame`、`Cursor` 和 `Replay` 已登记为 `candidate` 公共契约，但暂不进入最小核心面；升级更多真实消费者后再评估。

UI Kit 基础组件将在组件测试、文档和两个消费者证据补齐后逐项升档。它们目前存在于代码中，但“存在实现”不等于“已形成 stable API”。

## 变更规则

- `stable` 接口同主版本只允许追加兼容能力，不允许删除、改签名或改语义。
- `candidate` 接口允许小幅破坏，但每次必须提供迁移说明和升级示例。
- `experimental` 接口可以调整或移除，不得进入默认生产路径。
- Go package path、公开字段、事件名和配置 key 不得随意改名。
- Protobuf 删除字段必须 `reserved`，字段号不得复用；Wails DTO 与 gRPC DTO 分开演进。
- 公共 API diff 必须进入 CI，并保留两个真实消费者的兼容验证。

## 发布检查

```bash
go run ./tools/mzguiapidiff \
  --old framework/api/baselines/v0.0.1.json \
  --new framework/api/public-v1.json \
  --caller-usage framework/api/caller-usage/framework-showcase.json \
  --caller-usage framework/api/caller-usage/framework-plugin-echo.json \
  --callsite-caller framework-showcase \
  --callsite-root examples/framework-showcase
```

Protobuf 同时执行字段级兼容检查，覆盖字段删除、字段号复用、类型/label 变化、reserved 退役和 reserved 冲突：

```bash
go run ./tools/mzguiprotodiff \
  --old framework/api/baselines/proto/bootstrap-v0.0.1.proto \
  --new framework/proto/mzgui/framework/bootstrap/v1/bootstrap.proto
```

`tests/consumers` 中的 `basic` 和 `storage` 是两个独立 Go module。`scripts/verify-consumer-upgrade.sh` 先安装 `v0.0.1` 并测试，再升级到 `MZGUI_CONSUMER_REF`（默认 `main`）重跑测试与构建；不使用 `replace` 或工作仓私有路径。正式发布后可设 `MZGUI_CONSUMER_REF=vX.Y.Z` 从 tag 复验。

`framework/api/golden/schema-v1.json` 固定 API schema、catalog、应用 Manifest 与 config schema 的 SHA-256。变更这些文件时必须先评估兼容影响，再显式更新 golden；`tests/architecture` 会阻止未审阅的静默变化。`make check` 和 GitHub Actions 均执行上述检查。发布破坏性变更时，先提供替代接口、迁移文档和兼容窗口，不得直接改旧基线掩盖破坏。
