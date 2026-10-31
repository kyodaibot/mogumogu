# AGENTS.md

开始任何工作前，先阅读：
- docs/project-review.md：长期目标与共同约定
- plans/*.md：当前和接下来的计划、任务与验收标准（只实现其中授权的范围）
- plans/archive/：已完成的计划，只在需要查当时的决定和理由时读

文档地图（按主题找文件；在文件内先用 `grep -n "^#" <文件>` 看标题）：
- 产品功能、调用额度、AI 行为约定、数据表 → docs/project-review.md
- 接口、错误码、请求和响应字段 → docs/api.md
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
