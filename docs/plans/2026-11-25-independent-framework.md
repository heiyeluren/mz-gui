# MZ-GUI 独立框架实现计划

> **致 Claude：** 必须使用子技能 dev-executing-plans 逐任务执行此计划。

**目标：** 交付可独立测试、构建、运行和复用的 MZ-GUI 公共框架及「黑夜路人的 APP」，发布 `main` 与 `v0.0.1`。

**架构：** 保留 `github.com/heiyeluren/mz-gui` module 和现有公共包路径，仅抽取 Framework、公共生成代码、通用工具与示例。Showcase 通过 Wails 调用 Framework 服务，静态前端复用 Design Tokens；Mozi 消费方测试不进入公共仓。

**技术栈：** Go 1.26、Wails v3、React 18、TypeScript 5、原生 HTML/CSS/JavaScript、Taskfile。

---

### 任务 1：建立独立公共源码树

**文件：**
- 创建：`framework/**`
- 创建：`gen/go/mzgui/**`
- 创建：`examples/framework-showcase/**`
- 创建：`examples/framework-plugin-echo/**`
- 创建：`go.mod`、`go.sum`

**步骤：**
1. 从原 Monorepo 只复制公共目录与示例。
2. 排除 `apps/`、Mozi 消费方 candidate 测试、内部证据与产品工具。
3. 运行 `go mod tidy`，预期只保留 Framework 与示例所需依赖。
4. 运行 `go build ./...`，预期所有保留的 Go 包构建成功。

### 任务 2：用独立边界测试驱动清理

**文件：**
- 创建：`tests/architecture/framework_boundary_test.go`
- 修改：`framework/go/storage/media/store.go`
- 修改：`framework/go/storage/media/stream.go`
- 修改：`framework/go/storage/media/store_test.go`
- 修改：`framework/go/storage/media/stream_test.go`

**步骤：**
1. 编写失败测试，要求 Framework 不引用 `apps/mozi`，录音默认文件名不包含 `mozi`。
2. 运行 `go test ./tests/architecture ./framework/go/storage/media`，确认品牌中立断言失败。
3. 为 `media.Options` 增加命名空间配置，默认使用公共前缀，并允许产品注入兼容前缀。
4. 重跑测试，预期 PASS。
5. 扫描生产源码，预期无 `/Users/black`、内部 GitLab 与内网地址。

### 任务 3：完成基础 GUI APP

**文件：**
- 修改：`examples/framework-showcase/frontend/index.html`
- 修改：`examples/framework-showcase/frontend/styles.css`
- 修改：`examples/framework-showcase/frontend/main.js`
- 修改：`examples/framework-showcase/main.go`
- 修改：`examples/framework-showcase/mzgui.app.toml`
- 修改：`examples/framework-showcase/generated/appmeta/manifest_gen.go`
- 测试：`examples/framework-showcase/app_test.go`

**步骤：**
1. 增加标题、导航和真实 Wails 事件交互测试，先确认「黑夜路人的 APP」断言失败。
2. 实现公共能力侧栏、框架状态卡片、事件按钮、快速开始说明和错误反馈。
3. 将窗口标题、应用 manifest 与生成元数据统一为「黑夜路人的 APP」。
4. 运行 `go test ./examples/framework-showcase` 与 `go build ./examples/framework-showcase`，预期 PASS。
5. 实际启动 APP，核对标题、控件和一次事件调用。

### 任务 4：补齐 UI Kit 与公共工具发布配置

**文件：**
- 修改：`framework/ui-kit/package.json`
- 创建：`framework/ui-kit/tsconfig.json`
- 创建：`framework/ui-kit/tsconfig.build.json`
- 创建：`framework/ui-kit/src/index.test.ts`（仅在现有测试体系迁移后采用）
- 创建：`Taskfile.yml`
- 创建：`Makefile`

**步骤：**
1. 增加 UI Kit 独立 TypeScript 配置与声明输出，不引用 Mozi `node_modules`。
2. 配置 `dist` ESM、类型声明、CSS 和子路径导出。
3. 运行 `npm install`、`npm run build` 和 `npm pack --dry-run`，预期生成 JS、声明与 CSS。
4. 仅保留通用 `tools/mzgui*`；提供 `test`、`build`、`run`、`check` 统一入口。

### 任务 5：独立文档与全量验证

**文件：**
- 修改：`README.md`
- 创建：`docs/getting-started.md`
- 创建：`docs/architecture.md`
- 创建：`docs/framework-extraction.md`
- 创建：`.gitignore`
- 创建：`.github/workflows/ci.yml`

**步骤：**
1. 文档写明克隆、测试、构建、运行、Go import、UI Kit 和新项目定制方法。
2. 运行 `gofmt`、`go test ./...`、`go build ./...`，预期全部通过。
3. 运行 UI Kit 构建、仓外消费者测试与架构扫描，预期全部通过。
4. 检查 `git status`，预期无构建产物和未跟踪缓存。

### 任务 6：发布 v0.0.1

**步骤：**
1. 按任务边界提交代码，提交说明使用中文 Conventional Commits。
2. 运行 `git diff --check` 与最终全量验证。
3. 推送 `origin main` 并回读远端 SHA。
4. 创建附注 tag `v0.0.1`，推送后回读远端 tag 与指向 commit。
5. 确认本地 `HEAD`、`origin/main` 和 `v0.0.1` 一致，工作区干净。
