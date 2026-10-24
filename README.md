# mogumogu

以日历为中心的饮食推荐应用。长期目标与约定见 [docs/project-review.md](docs/project-review.md)，API 约定见 [docs/api.md](docs/api.md)，各阶段的范围见 [plans/](plans/)。

当前完成到**阶段 2**：
- 可以注册、登录、退出。
- 可以填写 Profile。
- 可以在日历上记录行程和饮食。
- 数据保存在 PostgreSQL 中，并按用户隔离。

AI 推荐（Ask AI）要到阶段 5 才实现，目前只显示为禁用的按钮。PostgreSQL 在 Docker Compose 中运行，Go 后端和 Vue 前端都在宿主机上运行。

## 目录结构

```
compose.yaml            # 本地 PostgreSQL
.env.example            # 环境变量示例（复制为 .env 使用）
Makefile                # 常用命令，运行 make 查看
backend/                # Go 后端（Gin、GORM、log/slog）
  cmd/server/           # 后端入口
  cmd/migrate/          # 数据库迁移命令
  migrations/           # 编号 SQL 迁移文件
  internal/auth/        # 密码哈希、会话 JWT、CSRF 令牌、限流
  internal/config/      # 环境变量读取与校验
  internal/database/    # 连接池与迁移
  internal/httpserver/  # 路由、中间件、各接口
  internal/store/       # 数据访问（每个查询都按用户过滤）
  internal/tz/          # Asia/Tokyo 日期与时刻
  internal/testdb/      # 集成测试用的临时数据库
frontend/               # Vue 3 + TypeScript + Vite + vue-router
  src/api/              # API 客户端（CSRF、错误处理）与接口类型
  src/pages/            # 登录、Profile、日历
  src/components/       # 行程和饮食记录表单
  src/lib/              # 日期（Asia/Tokyo）、餐次、错误文案
docs/  plans/           # 项目文档与阶段计划
```

## 前置依赖

| 工具 | 版本 | 用途 |
|---|---|---|
| Docker（含 Compose v2） | Docker Desktop 或 Docker Engine | 运行 PostgreSQL |
| Go | 1.26.2 及以上 | 后端、迁移 |
| Node.js / npm | Node 24（见 `frontend/.nvmrc`） | 前端 |
| openssl | 系统自带即可 | `make setup` 用它生成密钥 |
| pre-commit | 4.x | 提交前运行 gitleaks |
| gitleaks（可选） | 与 `.pre-commit-config.yaml` 中的 `rev` 一致，当前为 8.30.1 | 手动扫描整个仓库或 Git 历史 |

pre-commit 的 gitleaks hook 第一次运行时，会用本机的 Go 自动编译 gitleaks，所以单独安装 gitleaks CLI 不是必须的。

## 初次配置

1. 安装 pre-commit（每位成员克隆仓库后都要做一次）：

   ```sh
   uv tool install pre-commit   # 或者：brew install pre-commit
   ```

2. 执行初始化：

   ```sh
   make setup
   ```

   - 如果还没有 `.env`，会从 `.env.example` 生成一份，并自动填入随机的 `JWT_SECRET` 和 `CSRF_SECRET`。
   - 会执行 `npm ci` 安装前端依赖，并执行 `pre-commit install`。

3. 打开 `.env`，把 `DB_PASSWORD` 改成你自己的值。

   - `.env` 已被 `.gitignore` 忽略，**不要提交**。
   - 同一个 `.env` 会同时被 `docker compose` 和 shell（`source`）读取，所以**值里不要包含空格、引号、`$` 或 `#`**。`DB_PASSWORD` 还会拼进测试用的连接 URL，所以最好只用字母、数字、`-` 和 `_`。
   - 如果本机已有其他 PostgreSQL 占用 5432，请把 `DB_PORT` 改成其他端口，例如 5433。
   - 不用 make、手动执行 `cp .env.example .env` 的话，需要自己把两个密钥换成 `openssl rand -base64 48` 的输出，两个密钥分别生成。占位值故意写得太短，不替换的话后端会拒绝启动。

## 启动

### 用 make（推荐）

```sh
make dev
```

这条命令会依次：启动数据库 → 执行尚未执行的迁移 → 同时运行后端和前端。启动后打开 **http://localhost:5173**，第一次使用的流程是：**注册 → 填写 Profile → 进入日历**。按一次 Ctrl+C 会停止后端和前端，数据库继续运行。

| 命令 | 作用 |
|---|---|
| `make dev` | 数据库 + 迁移 + 后端 + 前端 |
| `make db` | 只启动数据库（后台运行，等到 healthy 再返回） |
| `make backend` | 只编译并运行后端（前台，:8080） |
| `make frontend` | 只运行前端开发服务器（前台，:5173） |
| `make migrate-up` | 执行所有尚未执行的迁移 |
| `make migrate-down` | 回退最近一次迁移（每次一步） |
| `make migrate-version` | 显示当前迁移版本 |
| `make migrate-create name=add_xxx` | 生成一对空的迁移文件 |
| `make test` | 单元测试与构建（不需要数据库） |
| `make test-integration` | 连接 PostgreSQL 的后端测试（含集成测试） |
| `make test-e2e` | 用无头 Chrome 测试整个 UI（使用临时数据库） |
| `make setup` | 首次配置，见上文 |
| `make db-stop` / `make db-down` | 停止数据库 / 删除容器（都会保留数据） |

- 运行 `make` 不带参数会列出全部命令。
- `make dev` 中，后端或前端任何一个意外退出，另一个也会被停止，make 返回错误。
- 只想单独开发某一部分时：先执行 `make db` 和 `make migrate-up`，再在不同终端分别运行 `make backend` 或 `make frontend`。
- 后端启动时**不会**自动执行迁移。迁移是单独的步骤，由 `make migrate-up` 或 `make dev` 执行。
- 删除数据库数据**没有**对应的 make 命令，需要时请手动执行 `docker compose down -v`。

### 手动启动

需要开三个终端，都从仓库根目录开始。

**1. 数据库与迁移**

```sh
docker compose up -d
docker compose ps        # STATUS 显示 (healthy) 即可
(set -a; source .env; set +a; cd backend && go run ./cmd/migrate up)
```

**2. 后端**（默认监听 http://localhost:8080）

```sh
cd backend
go build -o bin/server ./cmd/server
(set -a; source ../.env; set +a; ./bin/server)
```

- 命令放在子 shell `( … )` 里执行，`.env` 中的变量就不会留在当前终端。
- 日志以 JSON 格式输出到 stdout，不记录密码、令牌、Cookie 和请求体。缺少必需变量或变量不合法时，后端会列出问题（只显示变量名，不显示值），然后退出。
- 开发时也可以用 `(set -a; source ../.env; set +a; go run ./cmd/server)`。但这样按 Ctrl+C 停止后，`go run` 自己会打印一行 `exit status 1`，这是 `go run` 的行为，不代表后端出错。

**3. 前端**（http://localhost:5173）

```sh
cd frontend
npm run dev
```

开发服务器会把 `/api` 代理到 `API_PROXY_TARGET`（在根目录 `.env` 中设置，默认 `http://localhost:8080`）。

## 停止

- **后端、前端**：在各自的终端按 Ctrl+C。后端会停止接收新请求，等进行中的请求完成（最多 10 秒），关闭数据库连接池，然后退出。
- **数据库**：

  | 命令 | 效果 |
  |---|---|
  | `docker compose stop` | 停止容器，可以用 `docker compose start` 恢复 |
  | `docker compose down` | 删除容器和网络，**保留数据**（命名 volume `pgdata`） |
  | `docker compose down -v` | 连同 volume 一起删除，**数据会永久丢失** |

  volume 不能代替备份。

## 测试与验证

```sh
make test               # go vet、go test、npm run build（含类型检查）、npm test（默认时区 + TZ=UTC 各跑一次）
make test-integration   # 需要数据库在运行；包含下面的集成测试
make test-e2e           # 浏览器端到端测试，见下文
```

- **集成测试**：在 compose 的 PostgreSQL 上为每个测试**新建一个临时数据库** `mogumogu_test_*`，执行迁移，测完删除，所以不会影响开发数据。没有设置 `TEST_DATABASE_URL` 时（例如直接运行 `go test ./...`），集成测试会跳过并打印原因。覆盖的内容包括：数据隔离、持久化、行程跨午夜的边界、营养字段的 null 与 0、退出后令牌失效、CSRF 的会话绑定、登录限流。
- **前端测试**（Vitest）：日历网格（周一开头）、Asia/Tokyo 日期计算、API 客户端每次写请求都读取最新的 CSRF 令牌。
- **端到端测试**（`make test-e2e`，脚本在 `e2e/`）：用无头 Chrome 走完整个流程，包括注册、Profile、日历的增删改查、刷新后数据仍在、退出、再登录。同时检查 CSRF 令牌轮换、请求带有 Origin、Ask AI 不发送请求。
  - 测试**自己创建、自己清理数据**：在 compose 的 PostgreSQL 上新建临时数据库 `mogumogu_e2e_*` 并执行迁移，后端和 Vite 用单独的端口（18080、15173）启动并连接这个库；无论测试成功、失败还是被 Ctrl+C 中断，都会停止进程并删除这个库。开发数据不会被读取或修改，运行中的 `make dev` 也不受影响。
  - 需要安装 Google Chrome 或 Chromium（macOS 上会自动找到；其他系统可以用 `CHROME=/path/to/chrome` 指定），Node 24 以上，不需要额外的 npm 包。

### 健康检查接口

| 接口 | 说明 |
|---|---|
| `GET /api/health/live` | 不访问数据库。进程在运行就返回 `200 {"status":"ok"}` |
| `GET /api/health/ready` | 在 2 秒内 ping 数据库。成功返回 `200 {"status":"ready"}`，失败返回 `503 {"status":"unavailable"}` |

```sh
curl -i http://localhost:8080/api/health/ready
```

### 用 curl 调用需要登录的接口

写操作（POST、PUT、DELETE）必须带 `Origin` 和 CSRF 令牌（见 docs/api.md 2.4）。例如：

```sh
B=http://localhost:5173 J=/tmp/mogumogu-cookies.txt
csrf() { awk '$6=="mogumogu_csrf"{v=$7} END{print v}' "$J"; }
curl -s -c "$J" -b "$J" "$B/api/auth/csrf"
curl -s -c "$J" -b "$J" -X POST "$B/api/auth/login" \
  -H 'Origin: http://localhost:5173' -H "X-CSRF-Token: $(csrf)" -H 'Content-Type: application/json' \
  -d '{"username":"your_name","password":"your password"}'
curl -s -b "$J" "$B/api/meal-records?from=2026-10-01&to=2026-10-31"
```

登录后服务端会换发新的 CSRF 令牌，所以每次请求都要重新读取 Cookie（上面的 `csrf` 函数就是这样做的）。

### 密钥检查

- `git commit` 时，pre-commit 会运行 gitleaks，扫描**已暂存（staged）的改动**。发现疑似密钥就阻止提交。**不要用 `--no-verify` 跳过**，也不要为了让检查通过去修改 gitleaks 配置或添加忽略规则；遇到误报请先和团队确认。
- 也可以不提交、只对已暂存的改动运行检查：`pre-commit run gitleaks`。注意 `pre-commit run --all-files` 同样只检查已暂存的内容，它不会扫描整个仓库。
- 全量扫描需要 gitleaks CLI：

  ```sh
  gitleaks dir --redact .        # 扫描当前工作区
  gitleaks git --redact .        # 扫描完整 Git 历史
  ```

- 密钥一旦进入 Git 历史（包括已推送的提交），必须立即作废并更换。只删除文件或改写历史是不够的。
- 本地的 `JWT_SECRET` 或 `CSRF_SECRET` 如果泄露（例如被贴到聊天里），用 `openssl rand -base64 48` 重新生成即可。旧的登录会话会全部失效。

## 常见问题

- **后端启动时报 `JWT_SECRET must be at least 32 bytes`**：`.env` 里还是占位值，请按「初次配置」生成密钥。
- **注册或登录时出现「服务器出错」，后端日志提到 `relation "users" does not exist`**：还没有执行迁移，请运行 `make migrate-up`。
- **写操作返回 `403 csrf_failed`**：通常是 `Origin` 不在 `ALLOWED_ORIGINS` 里。例如用 `127.0.0.1` 访问，但允许列表里只有 `localhost`。请统一使用 http://localhost:5173 ，或把要用的地址加进 `ALLOWED_ORIGINS`。
- **`docker compose up` 报端口被占用**：本机已有服务占用 5432，请修改 `.env` 中的 `DB_PORT`。
- **后端停止时，Vite 终端出现 `http proxy error … ECONNREFUSED`**：这是正常现象，页面会显示「服务器暂时不可用」。
- **`docker compose` 报 `required variable … is missing a value`**：`.env` 缺少对应变量，请参照 `.env.example` 补上。
- **`npm ci` / `npm install` 提示 `allow-scripts … fsevents`**：可以忽略。`fsevents` 是 Vite 在 macOS 上的可选依赖，已经自带编译好的二进制，不执行它的安装脚本也不影响安装、构建和运行。
