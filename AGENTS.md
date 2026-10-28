# AGENTS.md

开始任何工作前，先阅读：
- docs/project-review.md：长期目标与共同约定
- plans/phase-*.md：当前阶段的任务与验收标准（只实现其中授权的范围）

硬性规则（与上述文档冲突时以此为准）：
- 不使用 --no-verify 跳过 pre-commit；检查失败时修复或报告
- 不修改 gitleaks 配置、不添加忽略规则，除非用户确认
- 不写入真实密钥；不提交 .env 或个人 AI 工具配置
- 不自动提交、推送、合并或部署
