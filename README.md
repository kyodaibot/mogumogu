# mogumogu

以日历为中心的饮食推荐应用。长期目标与约定见 [docs/project-review.md](docs/project-review.md)，当前阶段的范围见 [plans/phase-1.md](plans/phase-1.md)。

当前处于**阶段 1**：PostgreSQL 在 Docker Compose 中运行，Go 后端和 Vue 前端在宿主机运行，前端只有一个开发状态页，用来显示后端和数据库是否正常。

## 目录结构

```
compose.yaml          # 本地 PostgreSQL
.env.example          # 环境变量示例（复制为 .env 使用）
backend/              # Go 后端（Gin、GORM、log/slog）
  cmd/server/         # 程序入口
  internal/config/    # 环境变量读取与校验
  internal/database/  # 数据库连接池
  internal/httpserver/ # 路由、请求日志、健康检查
frontend/             # Vue 3 + TypeScript + Vite
docs/  plans/         # 项目文档与阶段计划
```

## 前置依赖

| 工具 | 版本 | 用途 |
|---|---|---|
| Docker（含 Compose v2） | Docker Desktop 或 Docker Engine | 运行 PostgreSQL |
| Go | 1.26.2 及以上 | 后端 |
| Node.js / npm | Node 24（见 `frontend/.nvmrc`） | 前端 |
| pre-commit | 4.x | 提交前运行 gitleaks |
| gitleaks（可选） | 与 `.pre-commit-config.yaml` 中的 `rev` 一致，当前为 8.30.1 | 手动扫描整个仓库或 Git 历史 |

pre-commit 的 gitleaks hook 第一次运行时，会用本机的 Go 自动编译 gitleaks，所以单独安装 gitleaks CLI 不是必须的。

## 初次配置

1. 安装 pre-commit 并启用 hook（每位成员克隆仓库后都要执行一次）：

   ```sh
   uv tool install pre-commit   # 或者：brew install pre-commit
   pre-commit install
   ```

2. 创建本地 `.env`，然后修改 `DB_PASSWORD`：

   ```sh
   cp .env.example .env
   ```

   - `.env` 已被 `.gitignore` 忽略，**不要提交**。
   - 同一个 `.env` 会同时被 `docker compose` 和 shell（`source`）读取，所以**值里不要包含空格、引号、`$` 或 `#`**。
   - 如果本机已有其他 PostgreSQL 占用 5432，请把 `DB_PORT` 改成其他端口，例如 5433。

3. 安装前端依赖：

   ```sh
   cd frontend
   npm ci
   ```

## 启动

### 用 make（推荐）

在仓库根目录执行：

| 命令 | 作用 |
|---|---|
| `make dev` | 启动数据库，再同时运行后端和前端。按一次 Ctrl+C 停止后端和前端，数据库继续运行 |
| `make db` | 只启动数据库（后台运行，等到 healthy 再返回） |
| `make backend` | 只编译并运行后端（前台） |
| `make frontend` | 只运行前端开发服务器（前台） |
| `make test` | `go vet`、`go test`、`npm run build` |
| `make setup` | 首次配置：`.env` 不存在时从 `.env.example` 复制，然后 `npm ci`、`pre-commit install` |
| `make db-stop` / `make db-down` | 停止数据库 / 删除容器（都会保留数据） |

- 运行 `make` 不带参数会列出全部命令。
- `make dev` 中，后端或前端任何一个意外退出，另一个也会被停止，make 返回错误。
- 只想单独开发某一部分时：先 `make db`，再在不同终端分别运行 `make backend` 或 `make frontend`。
- 删除数据库数据**没有**对应的 make 命令，需要时请手动执行 `docker compose down -v`。

### 手动启动

需要开三个终端，都从仓库根目录开始。

**1. 数据库**

```sh
docker compose up -d
docker compose ps        # STATUS 显示 (healthy) 即可
```

**2. 后端**（默认监听 http://localhost:8080）

```sh
cd backend
go build -o bin/server ./cmd/server
(set -a; source ../.env; set +a; ./bin/server)
```

- 命令放在子 shell `( … )` 里执行，`.env` 中的变量就不会留在当前终端。
- 日志以 JSON 格式输出到 stdout。缺少必需变量时，后端会报出缺少的变量名，然后退出。
- 开发时也可以用 `(set -a; source ../.env; set +a; go run ./cmd/server)`。但这样按 Ctrl+C 停止后，`go run` 自己会打印一行 `exit status 1`，这是 `go run` 的行为，不代表后端出错。

**3. 前端**（http://localhost:5173）

```sh
cd frontend
npm run dev
```

打开 http://localhost:5173 ，状态页会显示后端和数据库的状态，可以点「刷新」重新检查。开发服务器会把 `/api` 代理到 `API_PROXY_TARGET`（在根目录 `.env` 中设置，默认 `http://localhost:8080`）。

## 停止

- **后端、前端**：在各自的终端按 Ctrl+C。后端会停止接收新请求，等进行中的请求完成（最多 10 秒），关闭数据库连接池，然后退出。
- **数据库**：

  | 命令 | 效果 |
  |---|---|
  | `docker compose stop` | 停止容器，可以用 `docker compose start` 恢复 |
  | `docker compose down` | 删除容器和网络，**保留数据**（命名 volume `pgdata`） |
  | `docker compose down -v` | 连同 volume 一起删除，**数据会永久丢失** |

  volume 不能代替备份。

## 健康检查接口

| 接口 | 说明 |
|---|---|
| `GET /api/health/live` | 不访问数据库。进程在运行就返回 `200 {"status":"ok"}` |
| `GET /api/health/ready` | 在 2 秒内 ping 数据库。成功返回 `200 {"status":"ready"}`，失败返回 `503 {"status":"unavailable"}` |

数据库停止后，后端继续运行，ready 返回 503；数据库恢复后，ready 会自动恢复 200，后端不需要重启。

## 测试与验证

```sh
# 后端：静态检查与单元测试（不需要数据库，也不读取 .env）
cd backend
go vet ./...
go test ./...

# 前端：类型检查与构建
cd frontend
npm run build

# 接口（后端运行中）
curl -i http://localhost:8080/api/health/live
curl -i http://localhost:8080/api/health/ready
# 经过 Vite 代理（前端开发服务器运行中）
curl -i http://localhost:5173/api/health/ready
```

### 密钥检查

- `git commit` 时，pre-commit 会运行 gitleaks，扫描**已暂存（staged）的改动**。发现疑似密钥就阻止提交。**不要用 `--no-verify` 跳过**，也不要为了让检查通过去修改 gitleaks 配置或添加忽略规则；遇到误报请先和团队确认。
- 也可以不提交、只对已暂存的改动运行检查：`pre-commit run gitleaks`。注意 `pre-commit run --all-files` 同样只检查已暂存的内容，它不会扫描整个仓库。
- 全量扫描需要 gitleaks CLI：

  ```sh
  gitleaks dir --redact .        # 扫描当前工作区
  gitleaks git --redact .        # 扫描完整 Git 历史
  ```

- 密钥一旦进入 Git 历史（包括已推送的提交），必须立即作废并更换。只删除文件或改写历史是不够的。

## 常见问题

- **`docker compose up` 报端口被占用**：本机已有服务占用 5432，请修改 `.env` 中的 `DB_PORT`。
- **后端停止时，Vite 终端出现 `http proxy error … ECONNREFUSED`**：这是正常现象，状态页会显示「请求失败（HTTP 502）」。
- **`docker compose` 报 `required variable … is missing a value`**：`.env` 缺少对应变量，请参照 `.env.example` 补上。
