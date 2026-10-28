# 阶段 4a：饮食助手（单轮对话）

> 状态：**已确认**（2026-10-27）。按里程碑逐个实施，每个里程碑开始前由用户安排。

长期目标和共同约定见 docs/project-review.md（第 3.6、4、5、6 节），术语见 GLOSSARY.md，接口约定见 docs/api.md，agent 循环见 docs/diagram/agent-loop.html。

## 这个阶段要学会什么

- 用 **uv** 管理一个 Python 项目，用 **FastAPI** 写一个只供内部调用的服务。
- 用 **LangGraph** 写一个有步数上限、能兜底的工具调用 agent。
- 通过 **Amazon Bedrock** 调用 Claude，不用 API Key，用 AWS 凭证授权。
- 让 Go 和 Python 两个服务分工协作：权限和数据在 Go，推理在 Python。
- 用 mock 模型测试 agent 的各种异常路径。

## 前提

- 阶段 3 已合并到 dev。
- **由你完成的 AWS 准备**（里程碑 3 需要，里程碑 1、2 不需要）：
  - 有一个 AWS 账号，并为自己创建日常使用的身份（IAM Identity Center 用户，或可以用 `aws login` 获取临时凭证的身份），不使用 root，也不创建长期 Access Key。
  - 在东京区域（ap-northeast-1）的 Bedrock 控制台确认 Claude Haiku 4.5 可用（Haiku 5.5 暂时无法使用），记下日本 Geo 跨区域推理配置的 ID。
  - 在 AWS Budgets 设置每月 10 美元的预算告警（原计划在阶段 5，提前到第一次调用真实模型之前）。
- 本机安装 uv 和 AWS CLI v2。

## 不在本阶段范围内

多轮对话和对话历史（阶段 4b）；AWS 上的部署、EC2 的 IAM 角色（阶段 5）；流式输出；写入数据的工具；通用聊天。

## 关键设计（实现前确认）

| 项目 | 做法 |
|---|---|
| Python 项目位置 | 仓库根目录下的 `ai/`，按第 6 节的 uv 约定管理 |
| 对外接口 | `POST /api/assistant/messages`，请求 `{date, message}`，响应 `{reply, recommendations, ask_meal_type}`。`recommendations` 为空或正好 3 个；`ask_meal_type` 为 true 时，前端在回复下方显示三个餐次的预设问题。需要登录和 CSRF，和其他写接口一样 |
| Go → Python | `POST /internal/reply`，传入初始上下文和请求令牌。Python 服务在容器方式下不映射端口；宿主机方式只监听 127.0.0.1 |
| Python → Go（工具） | Go 新开一个**独立的内部端口**（默认 8081），只提供 `GET /internal/agent/meals` 和 `GET /internal/agent/events`。Nginx 不转发，容器方式下不映射到宿主机 |
| 请求令牌 | Go 用 HMAC 签名 `{user_id, exp}`，有效期 60 秒。签名密钥在 Go **启动时随机生成**、只存在内存中，因为签发和校验都在 Go。不需要在 `.env` 里新增密钥 |
| 调用额度 | 新迁移 `000006`：按「日期 + 用户」记录消息条数。在调用 Python 前原子地检查并加一（每用户 30 条、全站 300 条，按 Asia/Tokyo 的自然日计算）；超出时返回 `429 rate_limited`。**Python 调用失败时退还这一次额度** |
| 失败 | Python 超时（30 秒）、模型出错或两次校验都不通过时，返回 `503 ai_unavailable`，前端显示重试按钮。新错误码写入 docs/api.md |
| 本地凭证 | 宿主机方式：Python 直接用 AWS CLI 的默认凭证（`aws login` 或 SSO）。容器方式（`make app-up`）：把 `~/.aws/config` **只读挂载**、`~/.aws/login` 可写挂载进 ai 容器（botocore 刷新令牌时要写回缓存），并传入 `AWS_PROFILE`（本地开发的 profile 名为 `dev`）。凭证不进入镜像、`.env` 或 Git |
| 模型切换 | `MODEL_PROVIDER=bedrock`（默认）或 `fake`。`fake` 只用于自动化测试、CI 和 E2E |
| 日志 | 每条消息记录：耗时、步数、token 数、是否重试、结果（成功、校验失败、超时、额度不足）。不记录消息内容、Profile、饮食记录和模型原始输出 |

## 实施里程碑

### 里程碑 1：Python 服务与 agent 循环（只用 mock 模型）

- `ai/`：`uv init`，加入 FastAPI、LangGraph、Pydantic 等依赖，提交 `pyproject.toml`、`uv.lock`、`.python-version`。
- Pydantic 模型：初始上下文、`submit_reply` 的参数（reply 必填，recommendations 为空或正好 3 个，每个包含名称、说明、理由）、`ask_meal_type`。
- LangGraph 图，按 project-review 第 4 节和流程图实现：`agent` → 路由 → `tools` / `force_finish` / `validate`；步数上限 4，validate 打回一次，每条消息最多调用模型 6 次，`recursion_limit = 20`。
- 工具：`get_meals`、`get_events`，用 HTTP 调用 Go 的内部接口，带请求令牌；在 Python 侧也检查跨度不超过 31 天；结果截断到约 3000 字符。
- 系统提示：写入第 4 节的数据解读、过敏原、对话和记录类问题规则，以及「没说餐次时」的推断规则。
- validate：结构、推荐数量、长度，以及推荐中是否出现用户过敏原的关键词。
- `FakeModel`：按剧本返回预设的工具调用。
- pytest：覆盖正常提交、查询后提交、步数用完转 force_finish、同一步查询加提交只认提交、首次校验失败后重新提交成功、两次失败返回错误、含过敏原的推荐被拦下、模型调用次数不超过 6 次。

### 里程碑 2：Go 端接口、额度与内部接口

- 迁移 `000006`（up 和 down）：每日消息计数。
- `POST /api/assistant/messages`：
  - 校验 `date` 格式和 `message`（1 到 500 字）。
  - 检查并占用额度。
  - 按第 3.6 节组装初始上下文：Profile、目标日期前后各 1 天的行程、近 7 天的饮食窗口、时区和当前时间。
  - 签发令牌，调用 Python；失败时退还额度。
- 内部端口和两个查询接口：校验令牌（签名、过期时间），按令牌里的用户查询，跨度超过 31 天返回 422。
- 新配置：`AI_SERVICE_URL`、`INTERNAL_PORT`、额度上限（有默认值）。
- 更新 docs/api.md：新接口、`503 ai_unavailable`、额度说明。
- 测试：用 `httptest` 模拟 Python 服务，测试额度（第 31 条返回 429 且不调用 Python；全站上限；失败退还），以及令牌被伪造、过期或跨用户时被拒绝。集成测试使用临时数据库。

### 里程碑 3：接入 Bedrock、本地运行与 CI

- `MODEL_PROVIDER=bedrock`：通过 Bedrock Converse 调用 Haiku 4.5（日本 Geo 推理配置），模型 ID 由配置项 `BEDROCK_MODEL_ID` 指定，以后换成 Haiku 5.5 只改配置；设置最大输出长度；前缀达到缓存门槛时启用 prompt caching（已确认：Haiku 4.5 门槛 4096 token，目前整次输入约 3400 token，不启用）。开始前先对照当前文档确认 `langchain-aws` 和 LangGraph 的 API。
- `make dev` 同时启动 Python 服务；新增 `make ai`（单独启动）、`make test` 加入 pytest。
- `ai/Dockerfile`（多阶段构建，用明确版本的 uv，基于锁文件只安装运行依赖，非 root 运行）；compose 的 `app` profile 新增 `ai` 服务，按上表挂载凭证。
- CI 新增 `ai` job：`uv sync --locked` 和 pytest（只用 fake）；`docker` job 加入构建 ai 镜像。
- 评估用例 3 到 5 个，用 `make ai-eval` 手动运行，会调用真实模型并产生少量费用：有过敏原、没有任何记录、问超过 31 天的记录、与饮食无关的问题、目标日期不是今天且没说餐次。

### 里程碑 4：前端聊天面板

- 日历页右下角的悬浮按钮和聊天面板：桌面端是浮动面板，手机端全屏。
- 面板顶部显示目标日期；对话开始前显示 4 个预设问题；换日期时插入分隔线，不清空对话；刷新后清空。
- 消息气泡、推荐卡片、`ask_meal_type` 时的三个餐次按钮、加载状态、`429`（额度用完）和 `503`（显示重试按钮）的提示。
- 移除日历页原有的 Ask AI 按钮，并更新 e2e 中相应的检查。
- e2e：Python 以 fake 模式启动，覆盖「点预设问题 → 显示 3 张推荐卡片」和「换日期后出现分隔线、旧消息还在」。
- 沿用现有的设计变量（颜色、字体、圆角），不新增依赖。

## 验收标准（6 项）

每一项在完成报告中都要标明「实际执行」或「未验证」。写入数据的检查使用临时数据库。

1. **真实端到端**：用真实的 Bedrock，在浏览器里点「今天午餐吃什么？」得到 3 张推荐卡片；输入「我这两周吃了几次鱼」，得到只有文字、并说明了日期范围的回答；日志里能看到这条消息的步数和 token 数，但没有消息内容。
2. **agent 规则**（pytest，fake 模型）：里程碑 1 列出的各种路径全部通过。
3. **边界与安全**：
   - 经过 Nginx 访问 `/internal/...` 得不到内部接口；Python 和 Go 的内部端口都没有映射到宿主机。
   - 伪造、过期或属于其他用户的令牌被拒绝；查询跨度超过 31 天时返回 422。
   - 镜像和 Git 中没有 AWS 凭证。
4. **调用额度**：同一用户第 31 条消息返回 `429 rate_limited`，并且没有调用 Python；全站上限同样生效；Python 失败时额度被退还。
5. **聊天面板**：桌面和手机两种宽度下截图检查布局；换日期时出现分隔线、旧消息还在；刷新后清空；e2e 通过。
6. **原有流程不受影响**：`make test`、`make test-integration`、`make test-e2e` 全部通过；CI 的所有 job 通过；`make app-up` 能启动包含 ai 服务在内的整套服务。

## 完成报告

报告修改内容、运行方式、6 项验收的结果（实际执行还是未验证）、评估用例的运行结果、一次真实调用的平均耗时和 token 数，以及剩余问题。
