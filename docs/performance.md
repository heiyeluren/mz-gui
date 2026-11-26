# 性能与长期稳定性验证

## 验证入口

```bash
# 全量数据竞争检查
go test -race ./...

# 默认 30 分钟 Go + UI 并行 soak
bash scripts/soak.sh

# 可调整时长
MZGUI_SOAK_SECONDS=60 bash scripts/soak.sh
```

Go 报告位于 `build/soak/go-report.json`，UI 报告位于 `build/soak/ui.log`。这些文件是验证产物，不进入 Git。

## 验证边界

- Go 任务引擎每秒最多创建 10 个任务，避免无界写入和全表扫描压垮验证环境。
- SQLite 所有连接使用一致的 `BusyTimeout`，默认仍为 250ms；soak 使用 5 秒，调用 deadline 必须优先于锁等待。
- UI 使用真实 Chromium 和 DOM renderer，滚动 5000 项列表并持续写入终端输出。
- 关注 goroutine、打开连接、HeapAlloc、渲染 option 数量和后台进程退出，不用平均帧率或假快照代替。

## 当前基线

- `go test -race ./...` 全量通过。
- 2 秒快速 Go soak 通过：`tasks_created=20`、`open_connections=0`、`goroutine_delta=0`、`completed_batches=2`。
- 2 秒快速 UI soak 通过：16 批输出与 16 批滚动，最终渲染 17 个 option。
- 30 分钟正式 Go/UI soak 已启动，最终报告尚未完成；在完成前不声明长期稳定性通过。

## 禁止的通过方式

- 不把未运行的 soak、性能预算或其他平台结果写成通过。
- 不用删除数据库、强制解锁或降低断言来掩盖 SQLITE_BUSY。
- 不用缩短到毫秒级的演示代替声明的 30 分钟验证。
