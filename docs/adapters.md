# Adapter 与插件升级指南

## MessageBus 两种实现

`framework/go/adapters/messagebus` 同时提供：

- `New`：进程内 MessageBus，适合默认单进程和短生命周期事件。
- `NewDurable`：SQLite durable MessageBus，适合需要进程重启后恢复消息的应用。

二者实现同一 `contract.MessageBus`，调用方的 Publish、Subscribe、Ack、Nack、Info、Health 和 Close 不需要改写。只有组合根选择实现不同。

```go
bus, err := messagebus.NewDurable(messagebus.DurableOptions{
    DatabasePath: "/absolute/path/to/messages.db",
})
```

DurableBus 在发布时保存消息和幂等 ack，启动时按 sequence 恢复。重复 `IdempotencyKey` 不重复投递，SQLite 由 adapter 独占打开，调用方关闭 bus 后不得继续复用连接。

## 生命周期与取消

- `Close` 释放订阅、数据库和后台等待；关闭后 Publish/Next 返回明确错误。
- Context 必须设置 deadline，长等待 `stream.Next` 可被取消。
- SQLite migration bundle owner 为 `framework.messagebus`，变更必须保留向前 migration 和 schema golden 审阅。

## 插件故障隔离与恢复

- 插件进程由 managedprocess 管理，启动时验证 manifest、签名、隔离环境、工作目录和可执行文件。
- 异常退出按 RestartConfig 有界退避，禁止无限重启和不受管后代。
- Windows 使用 Job Object，POSIX 使用进程组，Stop 必须确认整棵进程树退出。
- 升级插件时先更新签名 artifact 与 manifest，保留旧版本回滚路径；不要复用旧 BuildID 或绕过签名校验。

## 验证

```bash
go test ./framework/go/adapters/messagebus ./framework/go/adapters/plugin ./framework/go/runtime/managedprocess
```

DurableBus 测试覆盖重启恢复、幂等顺序和同一契约调用；插件与进程测试覆盖异常退出恢复和停止确认。
