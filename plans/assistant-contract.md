# 计划：饮食助手的跨层契约（fix/assistant-contract）

状态：已确认（2026-11-03），进行中

## 背景
架构评审（2026-11-03）的候选 1：同一个限制在 Python 常量、提示词、Go 和前端各写一份，只有成功路径的 E2E 保护。漂移时好几种情况会让整条消息失败；「内容没变的修改」已经是一个现存的缺口：Go 拒绝（`sameMeal`），Python 的 `Staging` 只记了记录 ID，无法提前发现，结果整条回复返回 `ai_unavailable`。

## 已定决定（2026-11-03 讨论）
- 契约文件 `contract/` 只在测试里读，运行时各层仍用自己的常量；不做运行时共用，不做代码生成。
- 契约包括限制表和两份黄金 JSON（Go 生成的请求、手写并由工具输出核对的回复）。候选 3 的类型化这次不做。
- 前端也对照契约。
- 提示词里的数字由 `tools.py` 的常量生成。
- 「内容没变的修改」只在 Python 提前拦住，交给模型改正；Go 保持「拒绝 = 整条消息失败」。

## 里程碑 1：契约文件和各层的测试
- `contract/limits.json`、`contract/README.md`。
- Go：`internal/contracttest`（只供测试），`httpserver/contract_test.go` 检查常量；原来写在函数里的数字（消息 500、回复 1000、菜名 80、说明 200、内容 200、营养上限、餐次）改为命名常量；`client_test.go` 的硬上限读契约。
- pytest：`tests/test_contract.py` 检查 `tools.py` 常量和 Pydantic 字段的限制。
- Vitest：`src/lib/contract.test.ts`（`import.meta.glob`，前端镜像的构建只复制 `frontend/`）；`MAX_LENGTH` 从组件移到 `lib/assistant.ts`。

## 里程碑 2：黄金 JSON
- `askAssistant` 中组装上下文的部分抽成纯函数 `assistantContext`，Go 单元测试用它和固定记录生成 `contract/request.json` 并逐字比较（`-update` 重新生成）；pytest 用 `ReplyRequest` 读取。
- `contract/reply.json`：pytest 用 `Staging` / `MemoryChanges` 执行固定的工具调用，结果必须等于其中的变更；Go 用 `DisallowUnknownFields` 解码，检查新增、修改、删除和营养「不写 / null / 有值」的区别。

## 里程碑 3：提示词和「内容没变的修改」
- `prompt.py` 用 f-string 填入常量，生成的文字与原来逐字相同。
- `Staging` 记下见过的记录内容（上下文和 `get_meals` 的结果），修改后与原记录相同时返回「Not staged: … nothing changes」。
- Go 的注释和 project-review 写明：Go 拒绝提案只在两层不一致时发生。

## 验证
- `make test`、`make test-integration`、`make test-e2e`；逐层故意改一个值，确认对应的测试失败。
- `make ai-eval`：「修改」「删除」「推荐 → 记录」等用例。

## 不在范围内
- 运行时共用配置、代码生成；
- 候选 3（Python 的 `ProposedChange`、Go 上下文里的 `any`）；
- 修改 Go 拒绝提案时的处理；
- 前端的类型生成。

## 进展
- 里程碑 1–3 完成（未提交）：pytest 190（新增 `test_contract.py` 28 个，`test_staging.py` 新增 8 个）、Vitest 59（新增 2 个）、Go 单元测试（新增 3 个）、`make test-integration`、E2E 58/58、gitleaks 通过。
- 故意改值的检查：契约改 `memories`（三层都失败）、Python `MAX_CHANGES`、Go 菜名长度、Go 字段改名（请求样例失败）、前端 `MAX_LENGTH`，都由对应的测试报出；改回后通过。
- 提示词改由常量生成后与原来逐字相同。
- 追加：Python 的 `Reply` 继承了 `SubmitReply` 对来源的检查（只接受 URL），所以不能直接校验带标题的来源，`app.py` 因此用 `model_construct`。契约测试分开校验来源；类型化留给候选 3。
- `make ai-eval`（真实模型）：「食べた」要记录、日期推断、修改、删除、推荐 → 记录 5/5 通过。
