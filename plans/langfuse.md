# 计划：Langfuse 可观测性（feat/langfuse）

状态：已确认（2026-11-03），进行中

## 背景
每条消息现在只有一行 JSON 日志（结果、调用次数、token、耗时），看不到消息内部的过程：模型每一步看到了什么、调用了什么工具、参数和结果、校验为什么驳回。调试 agent 时只能在本地加打印。引入 Langfuse 记录每条消息的完整执行过程（trace）。

## 已定决定（2026-11-03 讨论）
- 用 **Langfuse Cloud 日本区域**（`https://jp.cloud.langfuse.com`，东京），Hobby 免费计划（每月 5 万 unit，数据保留 30 天）。不用 LangSmith：数据只能放美国或欧盟，免费额度（5,000 trace）不够。不自托管：官方最低配置远超 t4g.micro。
- 目标：(a) 查单条消息的完整执行过程；(b) 比较评估运行。成本和延迟的统计继续用 JSON 日志和 Logs Insights。
- **trace 放完整内容**：上下文（Profile、过敏原、记忆、饮食记录）、每一步模型的输入和输出、工具参数和结果、搜索关键词。修改 project-review §4 的约定，写 ADR 0002。数据留在日本，「请求只在日本处理」不受影响。
- 本地、评估和线上同一套代码：设了 `LANGFUSE_PUBLIC_KEY` 和 `LANGFUSE_SECRET_KEY` 才开启，否则关闭（CI 和 pytest 不设）。环境用 `LANGFUSE_TRACING_ENVIRONMENT` 标为 `local` / `eval` / `prod`，版本用 `LANGFUSE_RELEASE`（git SHA）。
- 用户标识用数据库里的用户 ID（数字），不用邮箱。标签：等级（`normal` / `pro`）和语言（`ja` / `en`）。用户 ID 和等级放在请求的顶层，不进 `Context`，模型看不到。
- 全部记录，不采样。额度用完时 Langfuse 不再接收，回复不受影响。
- 只记录 AI 服务；Go 不接。
- CloudWatch 的 JSON 日志规则不变：不放内容和搜索关键词。
- 这次不做隐私页面。
- 只用 trace 和打分；提示词留在代码里，用 git 管版本。

## 里程碑 1：AI 服务和 Go
- 依赖 `langfuse`。
- `tracing.py`（新增）：
  - `make_langfuse()`：没有 Key 返回 None；
  - `trace_config(req)`：每条消息一个 `CallbackHandler`，trace 名 `reply`，带 user_id 和标签。
- `run_agent` 把它放进 `graph.ainvoke` 的 config；`lifespan` 关闭时 `shutdown()`。
- Go：`assistant.Request` 新增 `user_id` 和 `tier`；集成测试检查传递。
- pytest：没有 Key 时关闭；开启时回调、user_id 和标签传给 graph；Langfuse 连不上时回复照常返回。

## 里程碑 2：评估
- `make ai-eval` 环境为 `eval`，每次运行有一个名字（作为标签），每个用例的 trace 名就是用例名。
- 用例是否通过作为打分写回 trace。

## 里程碑 3：部署和文档
- `deploy.sh` 读 `langfuse_public_key`、`langfuse_secret_key`；`deploy/compose.yaml` 加 `LANGFUSE_*` 和版本号；本地 `compose.yaml`、`.env.example`（默认空，即关闭）。
- **合并到 master 之前，由用户在 SSM 创建这两个参数。**
- 文档：ADR 0002；project-review §4；operations（注册、二步验证、SSM、按用户 ID 查 trace、额度用完）；README 环境变量。

## 验证
- `make test`、`make test-integration`、`make test-e2e` 通过。
- 本地用真实模型问一次，Langfuse 里能看到完整过程；跑一次 `ai-eval`，能看到打分。
- 部署后对比 AI 容器的内存（EC2 只有 1 GB）。

## 不在范围内
- 提示词管理、数据集和实验；
- Go 后端的追踪（OpenTelemetry）；
- 隐私页面、采样、脱敏、自托管。

## 进展
- 里程碑 1–3 的代码和文档完成（未提交）：pytest 153（新增 `test_tracing.py` 5 个）、Vitest 57、Go 单元和集成测试、E2E 58/58、gitleaks 通过；本地 compose 和线上 compose 配置校验通过，`deploy.sh` 语法检查通过。
- 追加：`cmd/admin users` 输出用户 ID，用来在 Langfuse 里按用户查 trace。
- AI 镜像 469 MB → 494 MB；空闲内存开关 Langfuse 都约 80 MiB（本机 Docker，未在 EC2 上测）。
- 真实验证（2026-11-03，日本区域，真实 Bedrock 和 Tavily）：
  - `ai-eval` 3 个用例（有过敏原、记录饮食 + 记住偏好、需要外部事实时搜索）3/3 通过，3 条 trace 名为用例名、环境 `eval`，`passed` 打分（BOOLEAN）都写入；
  - 每条 trace 6–10 个 observation（1 次模型调用 6 个，2 次 10 个）；generation 带模型 ID 和 token（含缓存命中和写入）；
  - 带 `user_id` 的请求：trace 名 `reply`，User ID 正确；环境未设时原来是 `default`，已改为默认 `local`（新增测试）。
- 未验证：标签和版本（API 查不到 trace 级属性，需在网页上看）；线上部署后的内存。
