# 独立抽取范围与兼容策略

## 抽取结论

原项目已经在目录和 Go import 层面形成 Framework / APP 分离，因此本仓采用轻量抽取，而不是重新实现框架。保留 `github.com/heiyeluren/mz-gui` module 和 `framework/...` 包路径，减少调用方迁移成本。

## 纳入公共仓

- `framework/` 全部公共能力、UI Kit、公共 API、能力目录和需求矩阵。
- `gen/go/mzgui/` 中 Framework 协议的生成代码。
- `examples/framework-showcase` 与 `examples/framework-plugin-echo`。
- 公共 `mzgui*` 生成、检查、能力、签名、SBOM 和平台诊断工具。
- 独立构建、测试、打包、文档和 CI。

## 不纳入公共仓

- `apps/`、产品 `internal/`、产品 manifest 与产品 Proto。
- 具体 AI Runtime、知识库、Skill、飞书、孔明等业务实现。
- 产品构建脚本、浏览器扩展、发布凭据、内部服务地址和内部验收材料。
- 反向读取产品源码的 candidate / callsite 测试。此类测试属于产品消费方，不属于公共 Framework 自测。

## 独立化修正

- 公共 API 的本地调用方改为 `framework-showcase` 和 `framework-plugin-echo`。
- 需求矩阵只保留本仓存在的实现与测试引用，不引用产品验收文档。
- 录音存储通过 `VoicePrefix`、`LivePrefix` 注入文件名前缀，默认分别为 `recording`、`recording-live`；产品兼容前缀由产品调用方传入。
- UI Kit 使用自己的 TypeScript 构建和 npm 出口，不再通过相对 alias 引用产品前端。
- Showcase 修正静态资源根路径，并提供标准 macOS `.app` 打包脚本。

## 后续同步原则

通用缺陷修复先进入 Framework 并附公共测试；产品行为留在产品仓。不要通过复制源码维持两个 Framework 版本。后续若让原产品直接消费本仓，应在产品侧单独调整其 module 与依赖关系，避免主 module 和外部依赖共用同一路径。
