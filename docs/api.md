# API 约定

> 状态：**设计已确认（2026-10-23）**，见文末「设计决定」。实现中如需修改接口，先改本文件并经确认，再改代码。
>
> 本文件定义前端与 Go 后端之间的 HTTP 接口。业务背景见 [project-review.md](project-review.md)，阶段范围见 [plans/phase-2.md](../plans/phase-2.md)。
> Go 与 Python AI 服务之间的内部接口在阶段 4 另行约定，不在本文件中。

## 1. 通用约定

### 1.1 基本规则

- 所有接口位于 `/api` 下，不加版本前缀（只有一个自有前端，同源部署；需要不兼容修改时前后端同步发布）。
- 请求和响应体均为 JSON（`Content-Type: application/json; charset=utf-8`），字段名使用 **snake_case**，与数据库字段一致。
- 请求体大小上限 64 KB，超出返回 `413`。
- 请求体中出现未定义的字段，返回 `400 invalid_request`（便于尽早发现拼写错误）。
- 浏览器只与同源的 `/api` 通信（开发时经 Vite 代理，部署时经 Nginx），因此不配置 CORS。

### 1.2 响应结构

- 单个资源：直接返回对象，不额外包一层。
- 列表：`{"items": [...]}`。不分页（按日期范围查询，范围有上限，见各接口）。
- 无内容的成功操作（如删除、退出）：`204`，无响应体。
- 错误：统一结构，见 1.6。

### 1.3 ID

- 资源 ID 为正整数（PostgreSQL `bigint` identity），JSON 中为 number。
- 路径中的 ID 不是正整数时，返回 `404 not_found`。

### 1.4 时间与日期

产品时区为 **Asia/Tokyo（UTC+09:00，无夏令时）**。

| 类型 | 格式 | 示例 | 用于 |
|---|---|---|---|
| 时刻 | RFC 3339，**必须带时区偏移** | `2026-10-23T18:30:00+09:00` | `starts_at`、`ends_at`、`created_at`、`updated_at` |
| 日期 | `YYYY-MM-DD`，按 Asia/Tokyo 解释 | `2026-10-23` | `date`、查询参数 `from` / `to` |

- 请求中的时刻必须带偏移（`Z` 或 `+hh:mm`）；不带偏移的时刻返回 `422 invalid_format`。服务端以 UTC 时刻（`timestamptz`）存储，响应中统一以 `+09:00` 输出。
- 时刻精度到秒；请求中的小数秒会被舍去。
- 日期必须是真实存在的日历日期（如 `2026-02-30` 无效）。

### 1.5 null、缺省与字符串

- 可空字段（如 `notes`、营养字段、`allergens`）在 `null` 时有业务含义，响应中**总是输出**该字段，不省略。
- 更新使用 `PUT`，表示**整体替换**：请求必须包含全部必填字段；可空字段省略时视为 `null`。
- 目前不提供 `PATCH`。以后如需部分更新，可以新增 `PATCH` 接口（语义为只修改请求中出现的字段），届时在本文件中定义，不改变现有 `PUT` 的行为。
- 字符串字段去除首尾空白后再校验；可空的文本字段（如 `notes`）去空白后为空串时存为 `null`。
- 字符串长度按 Unicode 字符数计算。

### 1.6 错误响应

```json
{
  "error": {
    "code": "validation_failed",
    "message": "Request validation failed.",
    "details": [
      { "field": "ends_at", "reason": "must_be_after_starts_at" }
    ]
  }
}
```

- `code`：机器可读，前端据此分支处理。
- `message`：给开发者看的英文说明，**前端不直接展示**，由前端按 `code` / `reason` 显示本地化文案。
- `details`：仅 `validation_failed` 时出现；`field` 使用请求体中的字段名，数组元素用 `allergens[2]` 形式。
- 错误响应不包含内部错误、SQL、堆栈等细节。

| HTTP | code | 含义 |
|---|---|---|
| 400 | `invalid_request` | JSON 格式错误、字段类型错误、出现未定义字段、缺少必需的查询参数 |
| 401 | `unauthenticated` | 未登录、令牌无效、过期或已撤销 |
| 401 | `invalid_credentials` | 登录时用户名或密码错误（不区分是哪一个错） |
| 403 | `csrf_failed` | CSRF 校验失败，见 2.4 |
| 404 | `not_found` | 资源不存在，**或属于其他用户**（两者不区分） |
| 404 | `profile_not_found` | 当前用户尚未创建 Profile |
| 409 | `username_taken` | 注册时用户名已被使用 |
| 413 | `payload_too_large` | 请求体超过 64 KB |
| 422 | `validation_failed` | 字段值不符合规则，见 `details` |
| 429 | `rate_limited` | 触发限流或饮食助手的调用额度用完；响应带 `Retry-After` 头（秒） |
| 500 | `internal_error` | 服务端错误 |
| 503 | `ai_unavailable` | 饮食助手这次没能回答（AI 服务不可用、超时或输出无效）。不计入调用额度，可以重试 |

`details[].reason` 取值：

| reason | 含义 |
|---|---|
| `required` | 必填字段缺失或为 null |
| `too_short` / `too_long` | 字符串长度或数组元素数不在范围内 |
| `out_of_range` | 数值超出范围 |
| `too_many_decimals` | 小数位数超过允许值 |
| `invalid_format` | 格式错误（日期、时刻、用户名字符等） |
| `invalid_value` | 不在枚举值中 |
| `duplicate_item` | 数组中有重复元素 |
| `must_be_after_starts_at` | `ends_at` 不晚于 `starts_at` |
| `duration_too_long` | 行程时长超过 24 小时 |
| `range_too_large` | 查询的日期范围超过上限 |
| `must_not_be_before_from` | 查询参数 `to` 早于 `from` |

## 2. 认证

### 2.1 会话 Cookie

登录成功后，服务端通过 `Set-Cookie` 下发 JWT：

| 项 | 值 |
|---|---|
| 名称 | `mogumogu_session` |
| 属性 | `HttpOnly`、`SameSite=Lax`、`Path=/api`；生产环境加 `Secure`（由配置 `COOKIE_SECURE` 控制） |
| 有效期 | 与令牌一致，默认 7 天（配置 `JWT_TTL`，即 `168h`）。不提供 refresh token，过期后重新登录 |

前端无法读取该 Cookie；请求时由浏览器自动携带（同源 `fetch` 默认 `credentials: 'same-origin'`）。

### 2.2 JWT 内容（后端内部约定，前端不解析）

- 算法 HS256，密钥来自环境变量 `JWT_SECRET`（至少 32 字节随机值）。
- Claims：`sub`（用户 ID，字符串）、`iss`、`aud`、`iat`、`exp`、`jti`（随机唯一 ID）。
- 每个请求校验：签名、`exp`、`iss`、`aud`、`sub` 与 `jti` 存在，`jti` 不在 `revoked_tokens` 中，且 `sub` 对应的用户仍然存在（用户被删除后，其未过期的令牌也立即失效）。任何一项失败返回 `401 unauthenticated`。
- 用户身份**只**来自令牌；任何请求体或查询参数中的 `user_id` 都不被接受（未定义字段，返回 400）。

### 2.3 退出与撤销

`POST /api/auth/logout` 将当前令牌的 `jti` 和过期时间写入 `revoked_tokens`，并下发过期的 `mogumogu_session` Cookie。已过期的撤销记录可以定期清理（例如每次退出时顺带删除）。

### 2.4 CSRF 防护（签名并绑定会话的双重提交 Cookie + Origin 校验）

**令牌格式**（前端视为不透明字符串，不解析）：

```
v1.<kind>.<issued_at>.<nonce>.<signature>

kind      = "a"（匿名令牌，尚未登录）或 "s"（会话令牌，已登录）
issued_at = 签发时间，Unix 秒
nonce     = 32 字节，由 crypto/rand 生成，base64url 编码
binding   = kind 为 "s" 时为当前会话 JWT 的 jti；kind 为 "a" 时为空串
signature = base64url( HMAC-SHA256( CSRF_SECRET,
              "mogumogu-csrf|v1|" + kind + "|" + binding + "|" + issued_at + "|" + nonce ) )
```

- `CSRF_SECRET` 是独立的环境变量（至少 32 字节随机值），不与 `JWT_SECRET` 共用。
- **匿名令牌和会话令牌一样，每次签发都使用新的安全随机数，并带签名和签发时间**，服务端可以验证它确实由本服务签发、没有被篡改、没有过期。不存在固定值的令牌：字面量 `anonymous`、空串、自己编造的字符串，或签名不正确的值，一律拒绝。
- 有效期：匿名令牌签发后 **2 小时**内有效；会话令牌只在其绑定的会话有效期间有效（会话过期或退出后随之失效）。

**下发**：

- `GET /api/auth/csrf`：请求中有有效会话时签发会话令牌，否则签发新的匿名令牌。令牌写入 Cookie `mogumogu_csrf`（**非** HttpOnly，`SameSite=Strict`，`Path=/`，生产环境加 `Secure`），响应体同时返回该值。
- **注册、登录成功时**：服务端为新会话（新的 `jti`）签发新的会话令牌，**覆盖** `mogumogu_csrf` Cookie，并在响应体的 `csrf_token` 字段中返回同一个值（见 4.2、4.3）。登录前使用的匿名令牌从此不能用于任何需要登录的接口。
- **退出成功时**：撤销会话，覆盖为新的匿名令牌。退出前的会话令牌随会话一起失效。
- **前端如何确保使用新令牌**：API 客户端**不缓存**令牌，每次发送写请求前都从 `mogumogu_csrf` Cookie 读取当前值，放进 `X-CSRF-Token`。因此登录、注册、退出后，下一次请求自动使用新令牌。登录和注册响应里的 `csrf_token` 只用于测试和调试时核对，前端不需要另外保存。

**校验**：所有 `POST`、`PUT`、`PATCH`、`DELETE` 请求（**包括登录和注册**）必须同时满足以下各项，任何一项不满足都返回 `403 csrf_failed`：

1. **Origin**：请求头 `Origin` 存在，且在允许列表中（环境变量 `ALLOWED_ORIGINS`，逗号分隔，例如开发时为 `http://localhost:5173`）。浏览器对非 GET 请求总会发送 `Origin`，缺少时直接拒绝。
2. **双重提交**：请求头 `X-CSRF-Token` 存在，且与 `mogumogu_csrf` Cookie 的值完全一致。
3. **格式与签名**：令牌格式正确，版本为 `v1`，重新计算的 HMAC 与令牌中的签名一致（使用常量时间比较）。
4. **会话绑定与有效期**：
   - 需要登录的接口：先完成认证（失败返回 `401`），再要求令牌是 `kind = s`，并且 binding 等于当前会话的 `jti`。匿名令牌、其他会话的令牌、退出前的令牌都会被拒绝。
   - 不需要登录的写接口（登录、注册）：请求中有有效会话时，按上一条校验；没有时，要求令牌是 `kind = a`，并且签发不超过 2 小时。

前端收到 `csrf_failed` 后，重新调用 `GET /api/auth/csrf`，再重试一次；仍然失败则显示错误。

`GET` 请求不修改数据，不需要 CSRF 令牌，也不校验 Origin。

> 用 `curl` 手动测试写操作时，需要先 `GET /api/auth/csrf` 保存 Cookie，再带上 `-H 'Origin: http://localhost:5173'` 和 `-H 'X-CSRF-Token: …'`。

### 2.5 限流

单台应用服务器，限流计数保存在进程内存中，重启后清零（不引入 Redis）。客户端 IP 取自 TCP 连接；只有经过配置的可信代理（阶段 3 的 Nginx）时才读取 `X-Forwarded-For`，否则客户端可以伪造 IP 绕过限流。

| 接口 | 规则 |
|---|---|
| `POST /api/auth/login` | 同一 IP + 用户名，15 分钟内失败 5 次后，返回 `429`，直到窗口结束 |
| `POST /api/auth/register` | 同一 IP，每小时 10 次 |

饮食助手的调用额度不是限流，规则见 9.2。

前端目前只显示「尝试次数过多，请稍后再试」。以后可以读取 `Retry-After` 响应头，显示剩余的等待时间或倒计时。

## 3. 接口一览

| 方法 | 路径 | 需登录 | 说明 |
|---|---|---|---|
| GET | `/api/health/live` | 否 | 进程存活（阶段 1 已实现） |
| GET | `/api/health/ready` | 否 | 数据库就绪（阶段 1 已实现） |
| GET | `/api/auth/csrf` | 否 | 获取 CSRF 令牌 |
| POST | `/api/auth/register` | 否 | 注册并登录 |
| POST | `/api/auth/login` | 否 | 登录 |
| POST | `/api/auth/logout` | 是 | 退出 |
| GET | `/api/auth/me` | 是 | 当前用户 |
| GET | `/api/profile` | 是 | 读取 Profile |
| PUT | `/api/profile` | 是 | 创建或替换 Profile |
| GET | `/api/schedule-events?from=&to=` | 是 | 按日期范围列出行程 |
| POST | `/api/schedule-events` | 是 | 新增行程 |
| GET | `/api/schedule-events/{id}` | 是 | 读取一条行程 |
| PUT | `/api/schedule-events/{id}` | 是 | 替换一条行程 |
| DELETE | `/api/schedule-events/{id}` | 是 | 删除一条行程 |
| GET | `/api/meal-records?from=&to=` | 是 | 按日期范围列出饮食记录 |
| POST | `/api/meal-records` | 是 | 新增饮食记录 |
| GET | `/api/meal-records/{id}` | 是 | 读取一条饮食记录 |
| PUT | `/api/meal-records/{id}` | 是 | 替换一条饮食记录 |
| DELETE | `/api/meal-records/{id}` | 是 | 删除一条饮食记录 |
| POST | `/api/assistant/messages` | 是 | 向饮食助手发送一条消息 |

「需登录」的接口在未登录时一律返回 `401 unauthenticated`。

## 4. 认证接口

### 4.1 `GET /api/auth/csrf`

响应 `200`，并设置 `mogumogu_csrf` Cookie：

```json
{ "csrf_token": "b3JX..." }
```

### 4.2 `POST /api/auth/register`

请求：

```json
{ "username": "taro_01", "password": "correct horse battery" }
```

| 字段 | 规则 |
|---|---|
| `username` | 必填；3–32 个字符；只允许 `a-z`、`0-9`、`_`；大写字母会被转成小写后保存和比较（即不区分大小写） |
| `password` | 必填；8–128 个字符；不去除首尾空白 |

- 成功：`201`，设置 `mogumogu_session` Cookie（注册即登录），并按 2.4 重新签发绑定新会话的 CSRF 令牌（覆盖 `mogumogu_csrf` Cookie）。响应体为 `GET /api/auth/me` 的内容，外加 `csrf_token` 字段：

  ```json
  {
    "user": { "id": 1, "username": "taro_01", "created_at": "2026-10-23T18:30:00+09:00" },
    "has_profile": false,
    "csrf_token": "v1.s.1792748400.Zm9v....c2ln..."
  }
  ```
- 用户名已存在：`409 username_taken`。
- 密码以 Argon2id 哈希保存，不记录到日志。

### 4.3 `POST /api/auth/login`

请求体同注册。

- 成功：`200`，设置 `mogumogu_session` Cookie，并重新签发绑定新会话的 CSRF 令牌；响应体与注册相同（包含 `csrf_token`）。
- 用户名不存在或密码错误：`401 invalid_credentials`（不区分）。
- 限流：`429 rate_limited`。

### 4.4 `POST /api/auth/logout`

成功 `204`，撤销当前令牌，清除 `mogumogu_session` Cookie，并把 `mogumogu_csrf` 覆盖为新的匿名令牌。退出属于需要登录的写接口，必须带有效的会话 CSRF 令牌；会话已失效时返回 `401`，前端照常视为已退出。

### 4.5 `GET /api/auth/me`

```json
{
  "user": {
    "id": 1,
    "username": "taro_01",
    "created_at": "2026-10-23T18:30:00+09:00"
  },
  "has_profile": false
}
```

前端在启动时调用此接口判断登录状态；`has_profile` 为 `false` 时引导用户先填写 Profile。

## 5. Profile

每个用户最多一条 Profile。

### 5.1 字段

| 字段 | 类型 | 规则 |
|---|---|---|
| `height_cm` | number | 必填；50.0–300.0；最多 1 位小数 |
| `weight_kg` | number | 必填；10.0–500.0；最多 1 位小数 |
| `age` | integer | 必填；10–120 |
| `has_exercise_habit` | boolean | 必填 |
| `allergens` | string[] 或 null | `null`：尚未填写；`[]`：确认无已知过敏原；非空数组：已填写。最多 20 项；每项去空白后 1–50 个字符；不允许重复（不区分大小写）；保存时保持用户输入的顺序 |
| `goal` | string | 必填；`build_muscle`（增肌）或 `lose_fat`（减脂） |
| `updated_at` | 时刻 | 只读 |

### 5.2 `GET /api/profile`

- 成功 `200`：

  ```json
  {
    "height_cm": 172.5,
    "weight_kg": 65.0,
    "age": 21,
    "has_exercise_habit": true,
    "allergens": ["peanut", "shrimp"],
    "goal": "build_muscle",
    "updated_at": "2026-10-23T18:30:00+09:00"
  }
  ```

- 尚未创建：`404 profile_not_found`。

### 5.3 `PUT /api/profile`

请求体为 5.1 中除 `updated_at` 外的全部字段（`allergens` 省略视为 `null`）。不存在时创建，存在时整体替换。成功 `200`，响应体同 `GET`。

## 6. 行程（schedule events）

### 6.1 字段

| 字段 | 类型 | 规则 |
|---|---|---|
| `id` | integer | 只读 |
| `title` | string | 必填；去空白后 1–100 个字符 |
| `starts_at` | 时刻 | 必填 |
| `ends_at` | 时刻 | 必填；必须晚于 `starts_at`；允许跨午夜；与 `starts_at` 相差**最多 24 小时**（超出返回 `duration_too_long`） |
| `notes` | string 或 null | 可选；最多 1000 个字符 |
| `created_at` / `updated_at` | 时刻 | 只读 |

### 6.2 `GET /api/schedule-events?from=YYYY-MM-DD&to=YYYY-MM-DD`

- `from`、`to` 均必填，**两端都包含**，按 Asia/Tokyo 解释。`to` 不能早于 `from`；范围最多 62 天（够覆盖月视图的 6 周）。
- 返回与区间 **[`from` 00:00, `to` 次日 00:00)**（Asia/Tokyo）**重叠**的行程，即满足 `starts_at < 区间结束 AND ends_at > 区间开始`。因此：
  - 10/23 23:00 – 10/24 01:00 的行程，查询 10/23 或 10/24 都会返回。
  - 10/23 22:00 – 10/24 00:00 的行程，查询 10/24 **不**返回。
- 按 `starts_at` 升序，相同时按 `id` 升序。

```json
{
  "items": [
    {
      "id": 12,
      "title": "上课",
      "starts_at": "2026-10-23T09:00:00+09:00",
      "ends_at": "2026-10-23T10:30:00+09:00",
      "notes": null,
      "created_at": "2026-10-20T08:00:00+09:00",
      "updated_at": "2026-10-20T08:00:00+09:00"
    }
  ]
}
```

### 6.3 `POST /api/schedule-events`

请求体：`title`、`starts_at`、`ends_at`、`notes`（可省略）。成功 `201`，响应体为创建后的完整对象。

### 6.4 `GET` / `PUT` / `DELETE /api/schedule-events/{id}`

- `GET`：`200`，单个对象。
- `PUT`：请求体同 `POST`，整体替换；成功 `200`，返回更新后的对象。
- `DELETE`：`204`。
- 资源不存在或属于其他用户：`404 not_found`。

## 7. 饮食记录（meal records）

### 7.1 字段

| 字段 | 类型 | 规则 |
|---|---|---|
| `id` | integer | 只读 |
| `date` | 日期 | 必填；可以是过去、今天或未来的日期 |
| `meal_type` | string | 必填；`breakfast`、`lunch`、`dinner`、`snack` |
| `content` | string | 必填；去空白后 1–200 个字符 |
| `calories` | number 或 null | kcal；`null` 表示未知，`0` 表示明确为零；0–10000；最多 1 位小数 |
| `protein_g` | number 或 null | g；同上；0–1000 |
| `fat_g` | number 或 null | g；同上；0–1000 |
| `carbs_g` | number 或 null | g；同上；0–1000 |
| `created_at` / `updated_at` | 时刻 | 只读 |

营养字段表示这一条记录实际食用份量的**总量**，不是每 100 g 的含量。服务端和前端都**不得**把 `null` 当作 `0` 计算或展示。

### 7.2 `GET /api/meal-records?from=YYYY-MM-DD&to=YYYY-MM-DD`

- `from`、`to` 均必填，两端都包含；规则同 6.2（`to` 不早于 `from`，最多 62 天）。查询单日时 `from` 与 `to` 相同。
- 返回 `date` 在范围内的记录，按 `date` 升序 → `meal_type`（breakfast、lunch、dinner、snack 的顺序）→ `created_at` → `id`。

```json
{
  "items": [
    {
      "id": 31,
      "date": "2026-10-23",
      "meal_type": "lunch",
      "content": "鸡胸肉沙拉",
      "calories": 450,
      "protein_g": 35.5,
      "fat_g": null,
      "carbs_g": null,
      "created_at": "2026-10-23T12:40:00+09:00",
      "updated_at": "2026-10-23T12:40:00+09:00"
    }
  ]
}
```

### 7.3 `POST /api/meal-records`

请求体：`date`、`meal_type`、`content`，以及四个营养字段（可省略，省略视为 `null`）。成功 `201`。

### 7.4 `GET` / `PUT` / `DELETE /api/meal-records/{id}`

规则同 6.4。

## 8. 日历页面的数据获取

不提供专门的「日历摘要」接口。月视图每周从**周一**开始，显示的日期范围（从包含当月 1 日那一周的周一，到包含当月最后一天那一周的周日，通常 5–6 周，不超过 42 天）由前端计算，然后并行请求（示例为 2026 年 10 月）：

```
GET /api/schedule-events?from=2026-09-28&to=2026-11-08
GET /api/meal-records?from=2026-09-28&to=2026-11-08
```

再在前端按日期分组显示摘要。跨午夜的行程在它覆盖的每一天都显示。

## 9. 饮食助手（阶段 4a）

术语见 GLOSSARY.md，行为约定见 project-review.md 3.6 和第 4 节。

### 9.1 `POST /api/assistant/messages`

需要登录和 CSRF（同其他写接口）。一次请求就是一轮：发送一条消息，同步返回回复（最长约 30 秒，前端显示加载状态）。

请求体：

| 字段 | 类型 | 规则 |
|---|---|---|
| `date` | 日期 | 必填；目标日期，即日历上选中的日期 |
| `message` | string | 必填；去空白后 1–500 个字符 |
| `locale` | string | 可选；`ja` 或 `en`，即当前界面语言，回复用这种语言。省略时为 `en`，其他值返回 `422 invalid_value` |

成功 `200`：

```json
{
  "reply": "午後に授業があるので、腹持ちのよい案を3つ挙げます：",
  "recommendations": [
    { "name": "鶏むね肉弁当", "description": "鶏むね肉と玄米", "reason": "午後に3時間の授業がある" }
  ],
  "ask_meal_type": false
}
```

- `reply`：必有的回复文字。
- `recommendations`：空数组，或正好 3 个（示例为了简短只写了 1 个）。
- `ask_meal_type`：为 `true` 时表示饮食助手在问用户想问哪一餐，前端在回复下方显示三个餐次的预设问题；此时 `recommendations` 为空。

错误：`401`、`403`、`422`（`date`、`message` 或 `locale` 不合规则）、`429 rate_limited`（调用额度用完）、`503 ai_unavailable`。

### 9.2 调用额度

- 每条消息（包括预设问题）计 1 次，按 Asia/Tokyo 的自然日计算。每位用户每天 30 条，全站每天 300 条，可通过环境变量调整。
- 额度在调用 AI 服务前检查并占用；用完时返回 `429 rate_limited`，`Retry-After` 为到次日 0 点的秒数，不调用 AI 服务。
- 返回 `503 ai_unavailable` 的消息退还额度。

### 9.3 内部接口（只供 AI 服务调用）

Go 在单独的内部端口（`INTERNAL_PORT`，默认 8081）提供饮食助手查询工具使用的只读接口。Nginx 不转发它们，容器方式下该端口也不映射到宿主机。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/internal/agent/meals?from=&to=` | 同 7.2，但最多 31 天 |
| GET | `/internal/agent/events?from=&to=` | 同 6.2，但最多 31 天 |

- 认证：`Authorization: Bearer <令牌>`。令牌由 Go 在每条消息开始时签发，随请求传给 AI 服务，只对这一个用户有效，60 秒后过期。签名密钥在 Go 启动时随机生成，只保存在内存中。令牌无效时返回 `401 unauthenticated`。
- Go 发给 AI 服务的请求和回复格式见 `ai/src/mogumogu_ai/schemas.py`（`ReplyRequest`、`SubmitReply`）。

## 10. 设计决定

以下各项已于 2026-10-23 确认。

| # | 问题 | 决定 | 备注 |
|---|---|---|---|
| 1 | 是否加版本前缀 `/api/v1` | 不加 | 只有一个同源前端，前后端同步发布 |
| 2 | ID 类型 | 自增 `bigint` | 数据隔离靠服务端按用户过滤，不依赖 ID 难以猜测 |
| 3 | 更新方式 | 目前只用 `PUT` 整体替换 | 以后可以新增 `PATCH`，见 1.5 |
| 4 | 注册后是否自动登录 | 是 | 少一步操作 |
| 5 | 用户名大小写 | 不区分，统一存小写 | 避免 `Taro` 和 `taro` 成为两个账号 |
| 6 | JWT 有效期 | 7 天 | 先以开发效率优先。退出时令牌会被撤销；部署到公网前可再评估是否缩短 |
| 7 | CSRF 方案 | 签名并绑定会话的双重提交 Cookie，同时校验 Origin；登录和注册也校验。匿名令牌同样使用安全随机数并带签名，2 小时过期；登录后重新签发绑定新会话的令牌 | 见 2.4 |
| 8 | 限流阈值 | 见 2.5 | 先用保守值，需要时调整 |
| 9 | Profile 数值范围 | 身高 50–300 cm，体重 10–500 kg，年龄 10–120 | 只用来挡住明显输错的值，不做医学判断 |
| 10 | 饮食日期能否晚于今天 | 可以 | 推荐的饮食历史窗口以「今天」为结束日（project-review.md 3.6），未来日期的记录不会被当作已食用 |
| 11 | 单条行程最长时长 | 24 小时，允许跨午夜 | 超出返回 `duration_too_long` |
| 12 | 数值精度 | 最多 1 位小数 | 用户手动填写，1 位小数足够 |
