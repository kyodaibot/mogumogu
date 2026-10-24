# 运维说明

本文件说明两种运行方式、容器方式的安全设计、CI、备份与恢复，以及常见问题。日常命令见 [README](../README.md)。

## 1. 两种运行方式

|  | 宿主机方式 `make dev` | 容器方式 `make app-up` |
|---|---|---|
| 用途 | 日常开发 | 确认打包后的整套服务能运行（与阶段 5 部署到 AWS 的方式最接近） |
| 访问地址 | http://localhost:5173 | http://localhost:8000（`WEB_PORT`） |
| 后端 | 宿主机上的 Go 进程，监听 `:8080` | 容器，**不映射端口** |
| 前端 | Vite 开发服务器，改代码立即生效 | 构建好的静态文件，由 Nginx 提供；改代码后要重新 `make app-up` |
| `/api` 转发 | Vite 代理 | Nginx |
| 迁移 | `make dev` 中的 `make migrate-up` | `migrate` 容器，执行完退出 |
| 停止 | Ctrl+C（数据库继续运行） | `make app-down`（数据库也停止） |

两种方式使用**同一个** PostgreSQL 容器和同一份数据（volume `mogumogu_pgdata`），可以同时运行。注意 `make app-down` 会停止共用的数据库，之后 `make dev` 的后端会连不上，执行 `make db` 即可恢复。

容器方式的启动顺序由 compose 的 `depends_on` 控制：数据库 healthy → `migrate` 成功退出 → 后端 healthy → Nginx。

## 2. 容器方式的安全设计

| 做法 | 理由 |
|---|---|
| PostgreSQL 只绑定 `127.0.0.1`；后端不映射端口，只有 Nginx 映射 `127.0.0.1:8000` | 局域网里的其他机器访问不到；外部请求只能经过 Nginx |
| Nginx 用 `$remote_addr` **覆盖** `X-Forwarded-For`；后端只在请求来自 `TRUSTED_PROXIES`（容器网络的私有网段）时才读取它 | 登录限流按客户端 IP 计数。如果后端相信客户端自己填的头，攻击者每次换一个假 IP 就能绕过限流。后端只能经过 Nginx 访问，而 Nginx 已经把这个头换成真实 IP，所以这样是安全的。宿主机方式不设置 `TRUSTED_PROXIES`，后端直接使用 TCP 连接的地址 |
| `.dockerignore` 排除 `.env`；配置全部通过环境变量传入；后端镜像是 distroless，以非 root 用户运行，只包含两个二进制文件 | 镜像里没有密钥和源码；即使后端被攻破，容器里也没有 shell 可用 |
| Nginx 访问日志只记录时间、来源 IP、方法、路径（不含 query）、状态码和耗时 | query、Cookie、`Authorization`、CSRF 头和请求体都不会进入日志 |
| `server_tokens off`，以及 `X-Content-Type-Options`、`X-Frame-Options`、`Referrer-Policy` 三个响应头 | 不暴露 Nginx 版本；防止浏览器猜测内容类型、防止页面被嵌入其他网站 |
| 请求体超过 1 MB 时，Nginx 返回与后端相同的 JSON `413 payload_too_large` | 保持 API 行为一致（docs/api.md）。64 KB 到 1 MB 之间的请求交给后端判断 |

阶段 5 上云时，`TRUSTED_PROXIES` 要改成负载均衡器的地址，届时重新评估。

## 3. 镜像版本

| 用途 | 镜像 |
|---|---|
| 后端编译 | `golang:1.26.2-alpine` |
| 后端运行 | `gcr.io/distroless/static-debian12:nonroot` |
| 前端构建 | `node:24-alpine` |
| 前端运行（Nginx） | `nginx:1.28-alpine` |
| 数据库 | `postgres:17.11` |

**版本标签和镜像摘要（digest）的区别**：标签（如 `nginx:1.28-alpine`）是一个名字，镜像发布方可以把它重新指向新的内容，例如修复安全问题后重新构建，所以同一个标签在不同时间拉取，得到的镜像可能不同。摘要（如 `nginx@sha256:…`）是镜像内容的哈希，永远指向同一份内容，可以保证构建完全可复现，但不会自动获得修复，需要手动更新。本阶段只使用明确的版本标签，不使用 `latest`。需要更严格的可复现性时（例如阶段 5 的 AWS 部署），再改为固定摘要。

`distroless/static-debian12:nonroot` 没有更细的版本标签，`nonroot` 只说明以非 root 用户运行。它只包含证书、时区等少量系统文件，没有 shell。

## 4. CI（GitHub Actions）

配置在 `.github/workflows/ci.yml`。push 到 `dev`、`master`，以及向它们发 PR 时运行，只有读取仓库的权限。

| Job | 做什么 |
|---|---|
| `secrets` | 用 gitleaks（与 pre-commit 同版本）扫描全部提交历史，输出中隐去发现的内容 |
| `backend` | `go vet`、`go test`；带一个临时 PostgreSQL，集成测试会真正执行。CI 中缺少测试数据库时，集成测试会失败而不是跳过 |
| `frontend` | `npm ci`、`npm run build`、`npm test`（UTC 和 Asia/Tokyo 各一次） |
| `docker` | 构建后端和前端镜像，不推送 |

CI 失败时 GitHub 默认仍允许合并。要强制「通过后才能合并」，在仓库的 Settings → Branches 中为 `dev` 和 `master` 添加保护规则，把这 4 个 job 设为必须通过的检查。

## 5. 备份与恢复

备份文件包含个人资料和密码哈希：保存在 `backups/`（已被 `.gitignore` 忽略），文件权限只允许本人读取，**不要提交、不要上传到公开的地方**。volume 不能代替备份，备份也只有在恢复成功过之后才算可用。

### 5.1 备份

```sh
make db-backup
```

在 postgres 容器内用 `pg_dump -Fc` 导出，写到 `backups/mogumogu-<时间>.dump`。`pg_dump` 来自容器，版本始终与数据库一致，宿主机不需要安装 PostgreSQL 工具。想要一份和当前数据完全一致的备份时，先停止应用（`make dev` 按 Ctrl+C，或者 `make app-down` 后再 `make db`），避免备份过程中有新的写入。

### 5.2 恢复演练（不影响开发库）

把备份恢复到一个新建的临时库，检查后删除。库名以 `mogumogu_restore_` 开头，以免和开发库混淆。在仓库根目录执行：

```sh
F=backups/mogumogu-20261024-161241.dump   # 要检查的备份
R=mogumogu_restore_check                  # 临时库
Q="SELECT 'users', count(*) FROM users UNION ALL SELECT 'profiles', count(*) FROM profiles UNION ALL SELECT 'schedule_events', count(*) FROM schedule_events UNION ALL SELECT 'meal_records', count(*) FROM meal_records UNION ALL SELECT 'revoked_tokens', count(*) FROM revoked_tokens"

# 1. 新建临时库
docker compose exec -T postgres sh -c 'createdb -U "$POSTGRES_USER" "$1"' _ "$R"

# 2. 恢复；出错时立即停止，退出码不为 0
docker compose exec -T postgres sh -c 'pg_restore -U "$POSTGRES_USER" -d "$1" --no-owner --exit-on-error' _ "$R" < "$F"

# 3. 检查：迁移版本应为 5、dirty 为 f；两边各表行数应相同
docker compose exec -T postgres sh -c 'psql -U "$POSTGRES_USER" -d "$1" -At -c "SELECT version, dirty FROM schema_migrations"' _ "$R"
docker compose exec -T postgres sh -c 'psql -U "$POSTGRES_USER" -d "$1" -At -c "$2"' _ "$R" "$Q"
docker compose exec -T postgres sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -c "$1"' _ "$Q"

# 4. 删除临时库（检查失败时也要执行）
docker compose exec -T postgres sh -c 'dropdb -U "$POSTGRES_USER" "$1"' _ "$R"
```

- `sh -c '…' _ "$R"` 的写法把参数传进容器，用户名和库名都取自容器内的环境变量，不需要在命令里写密码。
- 行数不同时，最可能的原因是备份之后又有写入。停止应用后重新备份、再演练一次。

2026-10-24 实际演练的结果：恢复成功，迁移版本 5、`dirty = f`，5 张表的行数与开发库一致，临时库已删除，开发库的行数没有变化。

### 5.3 正式恢复（会覆盖数据，只能手动执行）

正式恢复会**删除开发库现有的全部数据**，所以不提供一键命令。只在数据确实损坏或丢失时使用：

1. 停止应用（`make dev` 按 Ctrl+C，或者 `make app-down` 后再 `make db`）。
2. 先对当前数据再做一次 `make db-backup`，以免恢复错了无法回退。
3. 先按 5.2 演练一次，确认要用的备份是好的。
4. 删除并重建开发库，再恢复：

   ```sh
   F=backups/<要恢复的备份>.dump
   docker compose exec -T postgres sh -c 'dropdb -U "$POSTGRES_USER" --force "$POSTGRES_DB" && createdb -U "$POSTGRES_USER" "$POSTGRES_DB"'
   docker compose exec -T postgres sh -c 'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --no-owner --exit-on-error' < "$F"
   ```

5. `make migrate-version` 确认版本，然后启动应用并登录检查。

## 6. 常见问题

- **容器方式的后端启动失败，日志显示 `JWT_SECRET must be at least 32 bytes`，但 `.env` 里的值是对的**：compose 会优先使用 shell 中已经导出的同名变量，而不是 `.env`。用 `printenv JWT_SECRET | wc -c` 确认（只看长度）。`make app-*` 已经先读取 `.env` 来避免这个问题；直接运行 `docker compose` 时需要注意。
- **改了代码，容器方式下看不到变化**：容器运行的是构建好的镜像，重新执行 `make app-up`（会重新构建）。
- **`make app-down` 之后，正在运行的 `make dev` 后端连不上数据库**：`app-down` 会停止共用的数据库，执行 `make db`（新执行 `make dev` 时会自动启动数据库）。
- **单独重建后端容器后，页面上的请求返回 502**：Nginx 只在启动时解析一次 `backend` 的地址。执行 `make app-up`，或者 `docker compose restart web`。
- **后端停止时，通过 Nginx 访问 `/api` 返回 HTML 格式的 502**：这是 Nginx 自己生成的错误页，后端恢复后即正常。
- **端口 8000 被占用**：在 `.env` 中设置 `WEB_PORT`，例如 `WEB_PORT=8001`，然后访问 http://localhost:8001 。
- **想查看容器日志**：`make app-logs`。后端和 Nginx 的日志都是每行一条 JSON。
- **想删除所有数据重来**：`docker compose --profile app down -v`。这会**永久删除** volume，请先确认是否需要备份。
