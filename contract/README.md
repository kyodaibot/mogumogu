# contract/

饮食助手跨三层（浏览器、Go、AI 服务）共用的限制和消息样例。**只有测试读取这里的文件**，运行时各层仍用自己的常量；测试保证这些常量和这里一致（plans/assistant-contract.md）。

| 文件 | 内容 | 谁写 | 谁检查 |
|---|---|---|---|
| `limits.json` | 长度、条数、天数、餐次、营养数值上限、AI 服务的硬上限 | 手写 | Go `internal/httpserver/contract_test.go`、`internal/assistant/client_test.go`；pytest `tests/test_contract.py`；Vitest `src/lib/contract.test.ts` |
| `request.json` | Go 为一条消息发给 AI 服务的请求 | Go 测试生成（`-update`） | Go 逐字比较；pytest 用 `ReplyRequest` 读取 |
| `reply.json` | AI 服务的回复，含各种变更提案和记忆变更、来源 | 手写，变更部分必须等于工具实际暂存的结果 | pytest 用 `Staging` / `MemoryChanges` 生成后比较；Go 用 `DisallowUnknownFields` 解码 |

## 修改限制
1. 改 `limits.json`。
2. 改各层的常量：Go 的 `handlers_*.go`、AI 服务的 `tools.py` / `schemas.py` / `time_budget.py`（提示词里的数字由 `tools.py` 的常量生成）、前端的 `lib/assistant.ts` / `lib/memories.ts`。
3. `make test`：没改到的那一层会失败。

## 修改请求或回复的格式
- Go 发送的内容变了：`cd backend && go test ./internal/httpserver -run TestRequestMatchesTheContract -update`，再跑 pytest，确认 AI 服务仍能读取。
- AI 服务回复的格式变了：改 `reply.json`，`tests/test_contract.py` 和 Go 的 `TestReplyContractIsRead` 都要通过。
