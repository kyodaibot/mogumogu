# 阶段 2：基础业务与日历

> 状态：已确认（2026-10-23）。

长期目标和共同约定见 docs/project-review.md，接口约定见 docs/api.md。本文件规定阶段 2 的具体范围和验收标准。

## 前提

- 阶段 1 已合并到 dev，并验收通过。
- docs/api.md 的设计已确认（见其第 10 节）。实现中如需修改接口，先改文档并经确认，再改代码。

## 目标

- 用户可以注册、登录、退出；用户数据按用户隔离。
- 用户可以填写和修改 Profile。
- 用户在日历主页面按月查看，并在所选日期新增、查看、修改、删除行程和饮食记录。
- 数据保存在 PostgreSQL 中，刷新页面、重新登录或重启服务后仍然存在。

## 不在本阶段范围内

AI 与推荐（Ask AI 只显示为禁用的入口，不调用接口，不展示模拟推荐）、Python 项目、Nginx、前后端容器化、CI、云资源、refresh token、重复行程、拖拽排程、第三方日历同步、营养统计与图表、密码重置和修改、账号删除。

## 任务

### 1. 数据库迁移

- 使用 golang-migrate 和编号 SQL 文件，放在 `backend/migrations/`，每个版本都有 `up` 和 `down`。
- 新增 `backend/cmd/migrate`：一个小的 Go 命令，调用 golang-migrate 的库执行迁移，复用后端的数据库配置（同样的 `DB_*` 环境变量），成员不需要另外安装 CLI。不直接使用官方 CLI 的原因：它要加 `postgres` build tag 编译才包含 PostgreSQL 驱动，用起来容易出错。Makefile 提供 `migrate-up`、`migrate-down`（回退一步）、`migrate-create name=...`（只负责生成成对的空 SQL 文件）。
- 后端启动时**不**自动执行迁移；迁移是明确的单独步骤，写进 README。
- 表结构：

  | 表 | 要点 |
  |---|---|
  | users | `id` bigint identity；`username` text，唯一，保存小写；`password_hash` text；`created_at` |
  | profiles | `user_id` 主键，外键指向 users，删除用户时级联删除；字段见 api.md 5.1；`allergens` 为 `text[]`，可为 NULL；`updated_at` |
  | schedule_events | `id`；`user_id` 外键；`title`；`starts_at`、`ends_at` 为 `timestamptz`；`notes` 可为 NULL；`created_at`、`updated_at`；`CHECK (ends_at > starts_at AND ends_at - starts_at <= interval '24 hours')`；索引 `(user_id, starts_at)` |
  | meal_records | `id`；`user_id` 外键；`date` 为 `date` 类型；`meal_type` 用 CHECK 限定为四个值；`content`；四个营养字段为 `numeric(6,1)`，可为 NULL，`CHECK (>= 0)`；`created_at`、`updated_at`；索引 `(user_id, date)` |
  | revoked_tokens | `jti` 主键；`expires_at` `timestamptz`；索引 `(expires_at)`，用于清理 |

- 数据库约束是最后一道防线；接口层仍按 api.md 做完整校验，并返回 422。
- 继续不使用 GORM AutoMigrate。

### 2. 后端：基础设施

- 新增配置项：`JWT_SECRET`（必填，至少 32 字节）、`JWT_TTL`（默认 `168h`，即 7 天）、`JWT_ISSUER`、`JWT_AUDIENCE`、`CSRF_SECRET`（必填，至少 32 字节，不与 `JWT_SECRET` 共用）、`ALLOWED_ORIGINS`（必填，逗号分隔）、`COOKIE_SECURE`（默认 `false`，生产环境为 `true`）。错误信息和日志中不出现 `JWT_SECRET` 和 `CSRF_SECRET`。
- 时区：用 `time.LoadLocation("Asia/Tokyo")` 加载，并导入 `time/tzdata`，这样在没有系统时区数据的环境里（例如阶段 3 的精简容器）也能运行。
- 统一的 JSON 请求解析：限制请求体 64 KB，拒绝未定义字段，错误格式符合 api.md 1.6。
- 统一的校验错误输出（`422`，带 `details`）。
- 请求日志继续只记录 method、path、status、耗时；不记录请求体、Cookie、`Authorization` 和 `X-CSRF-Token`。

### 3. 后端：认证（api.md 第 2、4 节）

- 注册、登录、退出、`me`、`csrf` 五个接口。
- 用 Argon2id 哈希密码，参数写在代码常量里并附说明（建议采用 OWASP 推荐值 m=19 MiB、t=2、p=1）。
- 签发和校验 JWT，校验签名、`exp`、`iss`、`aud`、`sub`、`jti`，并检查撤销表。
- 退出时把令牌写入 `revoked_tokens`，并顺带清理已过期的撤销记录。
- CSRF（api.md 2.4）：
  - 签发：匿名令牌和会话令牌都使用 `crypto/rand` 生成的 32 字节随机数，带签发时间和 HMAC 签名；匿名令牌 2 小时过期。
  - 中间件作用于所有非 GET 请求，校验 Origin 允许列表、双重提交、格式与签名（常量时间比较）、会话绑定与有效期。
  - 注册、登录成功后签发绑定新会话的令牌，覆盖 Cookie，并在响应体中返回 `csrf_token`；退出后覆盖为新的匿名令牌。
- 登录和注册限流，计数保存在进程内存中。
- 认证中间件把用户 ID 放进请求上下文；业务代码只从上下文取用户 ID。

### 4. 后端：Profile、行程、饮食记录（api.md 第 5–7 节）

- 按 api.md 实现全部接口。
- **每一条查询、更新、删除的 SQL 都带 `user_id = 当前用户` 条件**。访问其他用户的资源时，返回与「不存在」相同的 404。
- 行程的日期范围查询使用重叠条件（api.md 6.2），区间边界按 Asia/Tokyo 计算。
- 营养字段区分 `null` 和 `0`，存取时都不能把 `null` 转成 `0`。
- 响应中的时刻统一用 `+09:00` 输出。

### 5. 前端

- 引入 vue-router。页面：
  - `/login`：登录和注册，用两个表单或切换显示。
  - `/profile`：Profile 页。
  - `/`：日历主页面。
- 未登录时访问受保护页面，跳转到 `/login`；登录后 `has_profile` 为 `false` 时，跳转到 `/profile`。
- 状态管理：不引入 Pinia，用少量 composable 管理当前用户等状态。如果实现中确实需要，再提出来讨论。
- 不引入 UI 组件库。
- API 客户端（对 `fetch` 的统一封装）：
  - 每次写请求前读取 `mogumogu_csrf` Cookie 的当前值，放进 `X-CSRF-Token`（登录、退出后令牌会变化）；遇到 `csrf_failed` 时重新获取令牌并重试一次。
  - 统一解析错误结构；遇到 `401` 跳转到登录页。
  - 把 `validation_failed` 的 `details` 映射到表单对应字段，显示中文提示。
- 日历组件：**自己实现月视图网格**，不引入日历库（理由：只需要月份切换、日期摘要和点击选择，见 project-review.md 3.3）。
  - 每周从**周一**开始（周一到周日）。
  - 日期计算统一按 Asia/Tokyo 进行，不依赖浏览器所在时区。
- 日历主页面：
  - 切换月份，默认选中今天。
  - 每个日期格子里显示行程数和饮食记录数的摘要。
  - 选中日期后显示当天的行程和饮食记录列表，可以新增、编辑、删除；删除前需要确认。
  - 跨午夜的行程在它覆盖的每一天都显示。
  - Ask AI 按钮为禁用状态，显示「即将开放」。
- 每个页面都要处理加载中、无数据、错误、保存成功几种状态。营养字段为 `null` 时显示「未填写」，不显示 0。
- 用业务页面替换阶段 1 的开发状态页。后端和数据库的健康状态改为用 `curl` 或 README 中的命令检查。

### 6. 测试

后端（`go test ./...`）：

- 单元测试：
  - 各接口的校验规则，包括边界值（例如行程时长恰好 24 小时允许、多 1 秒拒绝；身高 50 和 300 允许、超出拒绝）。
  - JWT 校验：篡改签名、令牌过期、`iss`/`aud` 错误、缺少 `jti`、已撤销。
  - CSRF：
    - 拒绝：缺少或不在允许列表中的 Origin、缺少令牌、请求头与 Cookie 不一致、签名被篡改、字面量 `anonymous` 或自己编造的字符串、过期的匿名令牌、绑定另一个会话的令牌、退出前的会话令牌、匿名令牌用于需要登录的接口。
    - 签发：连续两次签发的匿名令牌不同（确认使用了随机数）；登录和注册后返回的令牌是 `kind = s`、绑定新会话的 `jti`，并且与登录前的匿名令牌不同。
  - 限流。
  - 日期区间计算：月初、月末、闰日、跨午夜。
- 集成测试（需要真实 PostgreSQL）：
  - 测试辅助代码在 compose 的 PostgreSQL 中为每次测试运行创建一个临时数据库，执行迁移，测完删除。不影响开发数据，也不读取 `.env`。
  - 没有可用数据库时，集成测试**跳过并打印说明**，不判定为失败。用 `make test-integration` 显式运行。
  - 必须覆盖：
    - **数据隔离**：用户 B 对用户 A 的每一种资源做读取、列表、修改、删除，全部看不到也改不了。
    - **持久化**：写入后重新连接仍能读出。
    - **行程重叠查询的边界**：跨午夜的行程，以及恰好在 00:00 结束的行程。
    - **营养字段**：`null` 和 `0` 原样往返。
    - **撤销**：退出后旧令牌失效。

前端：

- `npm run build` 通过，包含类型检查。
- 引入 Vitest，测试 API 客户端每次写请求都读取 Cookie 中最新的 CSRF 令牌（模拟登录后 Cookie 变化，下一次请求使用新值），以及日历网格生成（周一开头；月初是周一或周日的月份、2 月、闰年）和 Asia/Tokyo 日期计算。在 UTC 时区下运行也必须通过，例如 `TZ=UTC npm test`。

### 7. 配置与文档

- `.env.example` 加入新的环境变量。`JWT_SECRET` 和 `CSRF_SECRET` 用明显的占位值，并写明生成方法（例如 `openssl rand -base64 48`，两者分别生成）。`ALLOWED_ORIGINS` 默认填 `http://localhost:5173`。
- README 补充：迁移命令、测试数据库的说明、新的 make 命令、首次使用流程（注册 → 填写 Profile → 日历）。
- docs/api.md 与实现保持一致；如有偏差，先改文档。

## 验收标准

每一项都要注明是否实际执行，没有执行的标为「未验证」。

1. 在空数据库上执行 `make migrate-up`，5 张表创建成功；执行 `make migrate-down` 逐步回退到空库，再执行 `migrate-up` 可以重新建好。
2. 注册新用户后自动进入登录状态；`/api/auth/me` 返回该用户，`has_profile` 为 `false`，页面跳转到 Profile 页。
3. 填写 Profile 后进入日历。刷新页面、退出再登录、重启后端后，Profile 仍然存在。
4. 在日历上对行程和饮食记录做增删改查，结果正确显示在对应日期；跨午夜的行程在两天中都显示。
5. 营养字段留空保存后，界面显示「未填写」，数据库中为 NULL；填写 0 时显示 0。
6. 退出后，用退出前保存的 Cookie 直接调用 `curl /api/auth/me`，返回 401。
7. 以下 POST 请求都返回 403 `csrf_failed`：不带 `X-CSRF-Token`；`Origin` 不在允许列表中（例如 `http://evil.example`）或不带 `Origin`；使用另一个账号会话下发的 CSRF 令牌；令牌值为字面量 `anonymous`；登录后仍然使用登录前的匿名令牌。
8. 在浏览器中登录，开发者工具显示登录响应覆盖了 `mogumogu_csrf` Cookie，之后的写请求带的是新令牌，并且全部成功（不依赖 `csrf_failed` 后的重试）。
9. 用错误密码连续登录，触发 429，响应带 `Retry-After`。
10. 用两个账号手动验证：用户 B 用 `curl` 访问用户 A 的行程 ID 和饮食记录 ID，GET、PUT、DELETE 全部返回 404，A 的数据没有变化。
11. 提交非法数据（例如 `ends_at` 早于 `starts_at`、行程超过 24 小时、营养字段为负数、未定义的字段），返回 422 或 400，前端在对应字段下显示提示。
12. Ask AI 入口显示为禁用状态；点击后不发送任何网络请求（在浏览器开发者工具中确认）。
13. `go vet ./...`、`go test ./...`、`make test-integration`、`npm run build`、`npm test` 全部通过。
14. 后端日志中没有密码、JWT、CSRF 令牌、Cookie 值或请求体。
15. README 中的命令与实际代码一致，并且已经按 README 从头执行过一遍（新的 clone、空数据库）。

## 完成报告

报告修改内容、运行方式、上面每一项验收的结果（实际执行还是未验证），以及剩余问题。
