# 独立消费者升级验证

`basic` 覆盖版本、EventBus 与本机 Endpoint；`storage` 覆盖 SQLite migration bundle 和 streamresume 的 open/append/replay。

两个目录都是独立 Go module，从 `v0.0.1` 开始，不使用 `replace`、工作仓源码路径或内部包。验证脚本先安装旧版本并执行测试，再升级到 `MZGUI_CONSUMER_REF`（默认 `main`）重跑测试与构建：

```bash
bash scripts/verify-consumer-upgrade.sh
MZGUI_CONSUMER_REF=v0.0.4 bash scripts/verify-consumer-upgrade.sh
```

`make check` 和 GitHub Actions 执行同一脚本。临时 module 在系统临时目录创建并在测试后清理，不会把 `go.sum` 或构建产物写回 fixture。
