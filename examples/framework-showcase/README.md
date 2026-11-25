# 黑夜路人的 APP

这是 MZ-GUI 的独立桌面示例，不依赖任何产品业务包。它使用 Framework 的生命周期 DAG、健康 Registry、窗口关闭与托盘绑定、EventBus、系统凭据抽象、错误契约和 Web Design Tokens。

## 运行

```bash
go run ./examples/framework-showcase
```

macOS 也可生成标准 `.app` 并打开：

```bash
bash scripts/package-showcase-darwin.sh
open build/MZ-GUI-Showcase.app
```

## 验证

```bash
go test ./examples/framework-showcase
go build ./examples/framework-showcase
```

页面包含框架概览、公共能力和快速开始三个视图。点击「发送框架事件」会通过 Wails Binding 调用 Go 服务，并由有界 EventBus 更新事件计数。示例使用内存凭据 Backend，不读取当前用户 Keychain 或 Windows Credential Manager。
