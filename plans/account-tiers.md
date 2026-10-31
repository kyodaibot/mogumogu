# 账号等级：Normal 和 Pro

> 状态：**已完成**（2026-10-31）。从 dev 切出 `feat/account-tiers`，三个里程碑一次完成。全站上限保持 100。上线后的检查（见「验证」）待部署后进行。

这是 plans/roadmap-tiers.md 路线图的第 2 步。术语见 GLOSSARY.md（账号等级、调用额度）。

## 这个阶段要学会什么

- 用数据库迁移给已有的表加字段，并给已有数据设默认值。
- 只能在服务器上执行的管理命令：不开放 API，就不会多出可以从网上访问的入口。
- 权限判断放在服务端，并且每次请求都读取最新的数据。

## 已确认的决定（2026-10-31 讨论）

- **存储**：`users` 表新增一列 `tier`，取值为 `normal` 或 `pro`，默认 `normal`；已有用户自动成为 normal。不记录等级变更历史。
- **授予**：新增 Go 命令 `cmd/admin`，和 `/migrate` 一起打包进 backend 镜像，直接连接数据库。不做管理员 API，应用中也没有管理员角色。
  - `admin users`：列出用户名、等级、注册时间；
  - `admin tier <username> <normal|pro>`：修改等级。
  - 本地：`make admin ARGS="tier busy pro"`；线上：通过 SSM 登录服务器后执行 `sudo docker compose run --rm migrate /admin …`。
- **读取**：每条发给饮食助手的消息都从数据库读取用户的等级，不写进 JWT，所以修改后立即生效。同一天内升级或降级时，已经用掉的次数照样计算。
- **额度**：
  - 配置 `ASSISTANT_USER_DAILY_LIMIT` 拆成 `ASSISTANT_NORMAL_DAILY_LIMIT`（默认 30）和 `ASSISTANT_PRO_DAILY_LIMIT`（默认 100），旧的变量名删除；
  - 全站上限不变（线上 100）。Pro 的上限等于全站上限，一个 Pro 用户可能用完当天的全站额度；Pro 只由管理员授予，接受这一点；
  - 每分钟 6 条的限流对两个等级相同。
- **界面**：
  - `GET /api/auth/me` 的 `user` 中新增 `tier`；
  - 导航栏用户名旁边，只有 Pro 用户显示「Pro」标记；
  - 额度用完的提示中写出当天的上限，例如「今日の上限（30回）に達しました」。上限随 `quota_exceeded` 错误返回（`error.limit`），前端不自己保存这个数字。
  - 只有用户自己的上限用完时才返回 `limit`。全站上限用完时不返回，前端显示现有的通用提示，避免出现「你的 30 次用完了」这种不准确的说法。
  - 不做「今天还剩几次」的显示。
- **Pro 目前只有额度上的区别**。等级不传给 AI 服务，提示词不变。变更提案和长期记忆是路线图第 3、4 步。
- **账号**：线上的演示账号保持 Normal，因为它的密码是公开的。`make seed-local` 完成后把 `busy` 设为 Pro，`normal`、`light` 保持 Normal。

## 里程碑 1：数据库与 Go

- 迁移 `000007_add_user_tier`：
  - up：`ALTER TABLE users ADD COLUMN tier text NOT NULL DEFAULT 'normal' CONSTRAINT users_tier_check CHECK (tier IN ('normal', 'pro'))`；
  - down：删除这一列。
- `store`：
  - `User` 模型新增 `Tier`；
  - 新增 `SetUserTier(username, tier)` 和 `ListUsers()`，用户名不存在时返回 `ErrNotFound`。读取等级直接用已有的 `UserByID`（实施时决定，不另加 `UserTier`）。
- `config`：`AssistantConfig` 的 `UserLimit` 改为 `NormalLimit`、`ProLimit`，读取新的环境变量。`.env.example` 和 `deploy/compose.yaml` 同步修改（线上没有设置过旧变量，使用默认值）。
- `store.TakeAssistantQuota`：区分「用户上限」和「全站上限」两种错误。
- `handlers_assistant.go`：扣额度前读取等级，按等级选择上限。用户上限用完时，`quota_exceeded` 的错误对象中带上 `limit`（`details` 是字段校验错误的列表，不放在那里）。
- `GET /api/auth/me`、注册和登录的响应：`user.tier`。
- `cmd/admin`：上面两个子命令。数据库设置和 `migrate` 一样，来自 `DB_*` 环境变量。
- `backend/Dockerfile`：构建并复制 `/admin`。
- `Makefile`：`make admin ARGS="…"`。
- 测试（集成测试，使用一次性数据库）：
  - 迁移后已有用户为 normal；数据库拒绝其他取值；
  - Normal 第 31 条消息返回 `quota_exceeded`，`limit` 为 30；全站上限用完时没有 `limit`；
  - 改为 Pro 后，同一天可以继续发送，直到第 100 条；
  - `/auth/me` 返回 `tier`；
  - `cmd/admin` 的参数检查和用户名不存在的情况。

## 里程碑 2：前端与脚本

- `api/index.ts`：`User.tier`。
- `App.vue`：Pro 用户在用户名旁显示「Pro」标记，375px 宽度下也不被挤坏。
- `AssistantChat.vue` 和 i18n（ja、en）：额度用完的提示带上上限。
- `scripts/seed-local.mjs` 或 Makefile：完成后把 `busy` 设为 Pro。
- 测试：
  - Vitest：额度用完的文案（有 `limit` 和没有 `limit` 两种）；
  - E2E：注册的新用户没有「Pro」标记；用 `admin` 把测试用户改为 Pro，刷新后显示「Pro」标记。

## 里程碑 3：文档

- `docs/api.md`：
  - 4.5 节 `user.tier`；
  - 1.6 节和 9.2 节 `quota_exceeded` 的 `limit`，以及额度按等级区分。
- `docs/project-review.md`：
  - 3.1 节：账号等级，以及由管理员通过服务器上的命令授予；
  - 3.6 节：按等级的额度；
  - 第 7 节：`users` 表新增等级。
- `docs/operations.md`：在服务器上修改等级的步骤。
- `README.md`：`make admin`。

## 实施记录（2026-10-31）

- 375px 的日语界面中，「Pro」标记会把语言切换挤到第三行。手机宽度下缩小了工具栏的间距和标记的内边距，现在和 Normal 一样是两行。
- 线上通过 `docker compose run --rm migrate /admin …` 执行，输出（包含用户名）也会写入 CloudWatch Logs。
- 本地的 `.env` 如果还有 `ASSISTANT_USER_DAILY_LIMIT`，它已经不再被读取，可以删除；需要时改用 `ASSISTANT_NORMAL_DAILY_LIMIT` 和 `ASSISTANT_PRO_DAILY_LIMIT`。

## 验证

- 每个里程碑：`make test`、`make test-integration`、`make test-e2e`。
- 手动检查：本地把 `busy` 设为 Pro，确认导航栏显示「Pro」标记。
- 上线后：
  - 确认迁移已执行；
  - 用 SSM 执行 `admin users`；
  - 把自己的账号设为 Pro，确认标记显示。

## 不在范围内

- 支付和订阅；
- 管理员 API 和管理页面；
- 等级变更历史；
- 「今天还剩几次」的显示；
- 变更提案、长期记忆（路线图第 3、4 步）；
- 把等级传给 AI 服务。
