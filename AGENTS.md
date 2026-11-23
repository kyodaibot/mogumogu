# AGENTS.md

按当前任务选读下列文档。有对应计划时，先读 `plans/` 顶层的相关计划，确认任务范围、进展和验收标准。只实现用户或对应计划明确授权的内容。需要追溯历史决定时再查 `plans/archive/`。

文档地图（按主题找文件；在文件内先用 `grep -n "^#" <文件>` 看标题）：
- 产品功能、AI 行为约定、数据表 → docs/project-review.md
- 接口、错误码、请求和响应字段 → docs/api.md；饮食助手的[调用额度见第 9.2 节](docs/api.md#92-调用额度)
- 部署、日志、服务器操作、账号等级的修改 → docs/operations.md
- 术语 → GLOSSARY.md
- 难以撤销的架构决定 → docs/adr/
- 架构图 → docs/diagram/
- 开发命令、目录结构、测试方式 → README.md

硬性规则（与上述文档冲突时以此为准）：
- 不使用 --no-verify 跳过 pre-commit；检查失败时修复或报告
- 不修改 gitleaks 配置、不添加忽略规则，除非用户确认
- 不写入真实密钥；不提交 .env 或个人 AI 工具配置
- 不自动提交、推送、合并或部署
