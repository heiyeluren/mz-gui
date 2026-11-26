# APP 界面参考

这些图片取自 MZ-GUI 自带 APP 的真实 macOS 窗口，不是设计稿，也不包含具体产品业务。

## 默认单进程

![黑夜路人的 APP 单进程框架概览](framework-showcase.jpg)

文件：`framework-showcase.jpg`。展示应用标题、导航、生命周期、健康状态、凭据适配和真实事件入口。

复现：

```bash
bash scripts/package-showcase-darwin.sh
open build/MZ-GUI-Showcase.app
```

## 可选多进程

![黑夜路人的 APP 多进程后台任务模板](framework-agentd-showcase.jpg)

文件：`framework-agentd-showcase.jpg`。展示同一任务接口下的后台任务、状态、取消和恢复入口。

复现：

```bash
bash scripts/run-agentd-showcase.sh
```

## Windows 单进程

![Windows 黑夜路人的 APP 单进程](windows-framework-showcase.png)

文件：`windows-framework-showcase.png`。来自 Windows 真机，显示框架概览、导航与 Ready/Healthy 状态；随后通过 UI Automation 调用真实 EventBus。

## Windows 多进程

![Windows 黑夜路人的 APP 多进程](windows-agentd-showcase.png)

文件：`windows-agentd-showcase.png`。来自 Windows 真机，显示后台任务与进程架构；随后启动真实 agentd 并确认任务 `Succeeded`。

截图用于帮助新用户理解默认可用界面。具体平台、窗口尺寸和系统主题可能影响实际显示；不应把截图当作其他平台的真机验收证据。
