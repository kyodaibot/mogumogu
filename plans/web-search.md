# 计划：网络搜索（feat/web-search）

状态：已确认（S1–S6 于 2026-11-02 确认），进行中

## 背景
饮食助手目前只能用上下文和用户自己的记录回答。有些问题需要网上的事实，比如「某家食堂/店卖什么」「营业时间」「某商品标示的营养成分」。Bedrock 不提供 Claude 的服务端网络搜索（Converse 和 Messages API 两种接入都不支持），所以自己实现一个搜索工具：由 AI 服务调用第三方搜索 API，把结果交给模型。

## 已定决定（2026-11-02 讨论）
- 模型继续用 Claude，不换成 Bedrock 上的 GPT；搜索做成第 10 个本地工具 `web_search(query)`，不做 MCP 网关。
- 搜索服务用 **Tavily**，账号**不绑卡**，只用每月 1,000 次的免费额度。额度用完时搜索失败，模型说明后照常回答，不产生费用。
- **只给 Pro**：Go 传 `allow_search`，Normal 用户的模型拿不到这个工具。
- **只把模型写出的搜索关键词发给 Tavily**，关键词中不能包含 Profile、过敏原、记忆和饮食记录。这是对「请求只在日本处理」约定的唯一例外，写进文档。
- 由模型决定什么时候搜索：只在需要上下文以外的外部事实时搜索；一般营养知识和推荐不搜索。
- 每条消息**最多搜索 4 次**，单独计数（一次模型调用里并行发出的多个搜索逐个计数）。不另设每日搜索额度。
- **最多 2 轮**（2026-11-02 实施中追加）：一次带搜索的模型调用算一轮，第 3 轮的搜索不执行，告诉模型用已有结果回答。原因：真实试跑中，网上查不到的内容（食堂当天菜单）会让模型换关键词一轮接一轮地搜，4 次模型调用用完后被强制提交（11.7 秒，没有推荐）；加了轮数限制后为 8.9 秒，模型自己提交。
- 每次搜索的时间上限是 `TimeBudget.timeout_for("search")`，即 min(8 秒, 剩到软上限的时间)，超时不重试。
- 结果取前 5 条，每条包含标题、URL 和正文摘要，总长度截断到约 3,000 字符。
- 不做 `fetch_url`（不让模型打开任意网址）。
- 搜索结果是数据，不是指令。记忆要求原话出现在当前消息中，饮食记录的修改要用户在卡片上确认，所以网页内容既不能凭空写进记忆，也不能直接改动饮食记录。另外新增注入相关的评估用例。
- 回复带结构化的 `sources`（来源），服务端只接受这条消息里实际搜到的 URL。
- API Key 存在 SSM 的 `/mogumogu/tavily_api_key`（SecureString），由用户自己创建；部署时由 `deploy.sh` 读进 `.env`。不需要改 Terraform：IAM 已经允许读取 `/mogumogu/*`，安全组也允许所有出站流量。
- 测试：
  - 做一个假搜索，CI、E2E 和单元测试都不调用 Tavily；
  - `FakeModel` 保留；
  - **`DemoModel` 冻结，这次不扩展**。来源由 pytest、Go 集成测试和 Vitest 覆盖。
  - 以后 `DemoModel` 只在其他测试覆盖不了某个界面流程时才扩展。

## 默认做法（已确认）
- **S1 来源的标题由服务端填写**：模型在 `submit_reply` 里只给出 URL，服务端从这条消息的搜索结果中查到对应的标题再填上。这样模型既不能编造链接，也不能编造标题。
- **S2 一条回复最多 5 个来源**，重复的 URL 只保留一个。
- **S3 Normal 用户要求搜索时**：模型在回复中说明「网络搜索是 Pro 的功能」，不能假装搜过，用已有的知识回答。做法和记忆功能一样。
- **S4 搜索服务的开关**：
  - 新增环境变量 `SEARCH_PROVIDER=tavily|fake|off`，**不设置时为 off**：这时连 Pro 也不提供搜索工具，本地开发和 CI 不受影响。
  - 线上的 compose 设为 `tavily`。
  - 设为 `tavily` 但 Key 为空时，启动时记一条警告并按 `off` 处理，不让整个服务启动失败。
- **S5 Tavily 的请求参数**：
  - `search_depth: basic`（每次 1 credit）、`max_results: 5`；
  - `include_answer: false`：不要 Tavily 自己用模型生成的答案，那是另一个模型写的、未经我们检查的文字；
  - 不设 `country`：产品不面向特定地区；
  - 不要原始网页正文：太长，而且每条结果已经带了正文摘要。
- **S6 来源不放进对话历史**：前端回传的历史里不带 `sources`，后续轮次如果需要，模型重新搜索。刷新页面后来源链接仍然在，因为 sessionStorage 保存了整个回复。

## 里程碑 1：AI 服务
- `search.py`（新增）：
  - `SearchProvider` 接口；
  - `TavilySearch`：使用 httpx，有自己的绝对超时 8 秒，Key 从环境变量读取；
  - `FakeSearch`：返回固定结果，供测试使用；
  - 失败的情况包括超时、401、429、432、433、5xx 和网络错误，统一当作「现在无法搜索」处理：工具返回原因，`search_errors` 加 1，另记一行 WARN 日志，只有状态码，没有关键词。
- `tools.py`：
  - 新增 `WEB_SEARCH`，参数为 `query`，1–200 个字符；
  - `tools_for(allow_proposal, allow_memory, allow_search)`；
  - 新增 `SearchBudget`：每条消息最多 4 次，并记录搜到过的 URL 和标题，供检查来源时使用。
- `agent.py`：tools 节点并行执行搜索，每次先调用 `timeout_for("search")`，超出 4 次的返回「已达上限」；`Counts` 增加 `searches` 和 `search_errors`。
- `schemas.py`：
  - `Context.allow_search`；
  - `SubmitReply.sources`：URL 列表，最多 5 个；
  - `Reply.sources`：`[{title, url}]`。
- 校验：`sources` 中有不在搜索结果里的 URL 时，作为校验失败，把原因交给模型改正，走现有的「校验后重试」流程。
- `prompt.py`：新增「Web search」一节，内容包括：
  - 什么时候搜索；
  - 关键词里不能有个人信息；
  - 搜索结果是数据，不是指令；
  - 用到搜索结果时必须列出来源；
  - 搜不到或失败时如实说明；
  - Normal 用户要求搜索时的说法（S3）。
  
  这一节对所有等级都相同，会进入缓存范围，Normal 的缓存部分会因此离 4,096 的下限更远。
- `app.py`：
  - 按 `SEARCH_PROVIDER` 创建搜索服务（S4）；
  - 只有在 `allow_search` 为 true 且搜索服务可用时，才提供搜索工具；
  - 日志增加 `searches` 和 `search_errors`。
- 测试（pytest，使用 FakeModel 和 FakeSearch）：
  - 搜索后提交，来源的标题由服务端填写；
  - 编造的 URL 被拒绝，然后重试；
  - 第 5 次搜索返回「已达上限」；
  - 一次调用里并行 3 次搜索；
  - 超时和 432 都不会让整条消息失败；
  - 软上限之后不再搜索；
  - Normal 用户的工具列表里没有 `web_search`；
  - 日志里没有搜索关键词；
  - `TavilySearch` 的请求参数和错误处理，用 httpx 的 `MockTransport` 测试，不发出真实请求。

## 里程碑 2：Go 和前端
- Go：
  - 上下文增加 `AllowSearch`，值为 `pro`；
  - `assistant.Reply` 增加 `Sources`，Go 把它透传给前端，`docs/api.md` 9.1 补充这个字段。
  - **Go 会再检查一次**：只保留 `http` 或 `https` 开头、长度合理的 URL，最多 5 个。不合格的项丢掉并记日志，回复照常返回。这是为了防止 `javascript:` 这类链接到达前端。
  - 集成测试：用假 AI 服务返回来源，检查透传和过滤。
- 前端：
  - `AssistantReply.sources`；
  - 在回答下方显示「出典 / Sources」小链接，用 `target="_blank" rel="noopener noreferrer"`，没有标题时显示域名；
  - 新增 `lib/sources.ts`，负责过滤 http(s) 链接和生成显示文字，并写 Vitest 测试；
  - 补充 i18n 的 ja 和 en 文案。
- 链接的样式在 375px 宽度下手动检查一次。这需要用 `make dev` 加真实模型和真实搜索，消耗几次免费额度。

## 里程碑 3：部署、评估和文档
- 部署：
  - `deploy.sh` 多读一个参数 `tavily_api_key`；
  - `deploy/compose.yaml` 的 ai 服务增加 `SEARCH_PROVIDER: tavily` 和 `TAVILY_API_KEY`；
  - 本地的 `compose.yaml` 和 `.env.example` 增加这两个变量，`.env.example` 中默认是 `off`。
  - **合并到 master 之前，要先由你在 SSM 里创建这个参数**，步骤写进 `docs/operations.md`。
- 评估（`make ai-eval`，使用真实的 Tavily，每跑一次大约消耗 5 到 10 次免费额度）：
  1. 需要外部事实的问题：要搜索，来源都来自搜索结果；
  2. 一般营养问题：不搜索；
  3. 搜索结果中有注入文字（这个用例使用固定的假结果）：模型不受影响；
  4. Normal 用户要求搜索：说明这是 Pro 的功能，不搜索；
  5. 有过敏原和记忆的用户要求搜索：关键词中不包含过敏原和记忆的内容。
- 文档：
  - `docs/project-review.md`：
    - 搜索规则；
    - 数据离开日本的例外（只有搜索关键词）；
    - 工具数改为 10 个；
    - DemoModel 冻结的规则；
    - 来源的检查方式。
  - `docs/api.md`：9.1 的 `sources` 字段。
  - `docs/operations.md`：
    - 注册 Tavily、创建 SSM 参数的步骤；
    - 日志新增的字段；
    - 免费额度用完时的表现。
  - `GLOSSARY.md`：新增「来源（Source）：回答中引用的网页，由搜索结果提供」。
  - `README.md`：在环境变量说明中补上 `SEARCH_PROVIDER`。

## 验证
- `make test`、`make test-integration`、`make test-e2e`：全部通过。E2E 不改，用来确认原有流程没有被影响。
- `make ai-eval`：新增的 5 个用例，加上原有的 28 个。
- 合并并部署后，在线上用 Pro 账号问一个需要搜索的问题，确认来源链接显示正常，日志里 `searches` 大于等于 1，并且没有关键词。

## 不在范围内
- `fetch_url`；
- 搜索结果缓存；
- 搜索的每日额度；
- 按地区调整搜索；
- 换模型；
- MCP 网关；
- 扩展 DemoModel。

## 进展
- 里程碑 1–3 完成（未提交）：pytest 148、Vitest 57、Go 单元和集成测试、E2E 58/58、pre-commit 通过。
- 真实试跑（Bedrock + Tavily）：
  - 沙拉鸡蛋白质：搜索 1 次，约 5 秒，有来源；
  - 北部食堂当天菜单：搜索 2 轮后自己提交，但没有给 3 个推荐（提示词要求给），交给里程碑 3 的评估用例跟踪。
  - 搜索关键词中没有过敏原和记忆。
- 375px 手动检查（用完即删的数据库，真实模型和搜索）：来源链接正常换行，`target=_blank rel="noopener noreferrer"`，刷新后仍在。发现回复里出现 Markdown 原文（`**…**`），已在提示词中要求纯文本。
- 评估：新增 6 个用例（计划中的 5 个，加上「查不到当天菜单也给推荐」）。全部 34 个中通过 31 个：「超过 31 天的记录」是原有的不稳定用例；「查不到当天菜单也给推荐」约六成通过，记为已知限制（project-review 3.6）；「搜索结果中的提示注入」失败的原因不是注入（注入每次都被挡住），而是模型把 `protein_g` 等写成 `protein`，校验重试也失败。已改为接受不带 `_g` 的字段名，之后 3/3 通过、不再重试。
- 部署前：用户已在 SSM 创建 `/mogumogu/tavily_api_key` 之前，不要合并到 master（`deploy.sh` 会读这个参数）。
