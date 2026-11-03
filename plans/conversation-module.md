# 计划：前端对话模块（refactor/conversation）

状态：已确认（2026-11-03），进行中

## 背景
架构评审（2026-11-03）的候选 2（并入候选 6）：「对话」的规则分散在 `AssistantChat.vue`、`lib/assistant.ts` 和 `lib/chatStore.ts`；提案卡片和记忆撤销直接修改 props，靠 deep watch 保存；失败、重试、刷新后恢复的路径没有测试。历史字段的 `validation_failed` 被显示成「太长了」且不能重试。

## 已定决定（2026-11-03 讨论）
- 纯 TS 模块 `lib/conversation.ts`（GLOSSARY：对话），内部用 `reactive`，在 Vitest（node）中测试，不引入组件测试工具。
- API（发送、执行提案、删除记忆）和保存方式从外部传入，默认用真实的 `api` 和 `chatStore`。
- 每个操作结束时显式保存，去掉 deep watch。
- 模块自己检查非法操作（等待回复时发送或新对话、重试非最后一轮），界面的禁用只是显示。
- 语言直接读 i18n 的 `locale`。
- 提案卡片和记忆撤销的状态只由模块修改；组件只读 props。
- 历史字段出错：提示开始新对话，不能重试。
- 补齐文档里有、前端没有文字的错误码，并用 Vitest 对照 `docs/api.md` 1.6。
- `chatStore` 读取旧数据时补 `sources: []`，`AssistantReply.sources` 改回必填。

## 里程碑 1：对话模块
- `lib/conversation.ts`：`turns`、`busy`、`full`、`send`、`retry`、`canRetry`、`startNew`、`markAdded`、`setChecked`、`runProposal`、`undoMemory`。
- `lib/proposals.ts`：`proposalFailure`（文字和是否关闭卡片）。`lib/memories.ts`：`deleteMemoryIdempotent`。
- Vitest：发送、失败分类和重试、新对话、提案、记忆撤销、刷新后恢复。

## 里程碑 2：组件改用模块，补齐错误码
- `AssistantChat.vue` 只保留面板、滚动、焦点、`adding` 和预设问题；`ProposalCard.vue`、`MemoryNote.vue` 不再修改 props；`MemoryList.vue` 用 `deleteMemoryIdempotent`。
- i18n：`assistant.historyBroken`，`errors.codes` 补 `invalid_request`、`pro_required`、`proposal_executed`、`proposal_expired`、`ai_unavailable`、`timeout`。
- `src/lib/errorCodes.test.ts`。

## 验证
- `make test`、E2E 58/58（不改）。
- 375px 手动检查：成功的回复、AI 服务停止时的失败、刷新后重试。

## 不在范围内
- 候选 7（Profile 的过敏原）；组件测试工具；E2E 和 DemoModel；API 和后端。

## 进展
- 里程碑 1–2 完成（未提交）：Vitest 105（新增 `conversation.test.ts` 26 个、`errorCodes.test.ts` 20 个、`chatStore.test.ts` 1 个；删掉 `sources.test.ts` 中「旧回复没有 sources」1 个，改由 chatStore 补齐）、vue-tsc、pytest 190、E2E 58/58 通过。
- 手动检查（375px，用完即删的数据库 `mogumogu_manual_*`，假模型）：预设问题得到 3 张卡片；停掉 AI 服务后发送，显示「食事アシスタントが回答できませんでした」和「再試行」；重启 AI 服务并刷新页面后，失败的轮和重试按钮仍在，重试成功。数据库已删除。
- 未手动检查：历史出错时的「新しい会話」按钮（只由 Vitest 覆盖，正常操作下难以触发）。
