# 阶段 3：交付与运维基础

> 状态：**已确认（2026-10-24）**。按里程碑逐个实施，每个里程碑开始前由用户安排。之前的第 2、3 版对学生作品来说设计过度，本版只保留学习主线和必要的安全底线，其余推迟（见「推迟到以后」）。
>
> 修订（2026-10-24，里程碑 1 完成后）：安全设计停在现有水平，不再新增；里程碑 2 只跑检查和构建；里程碑 3 的恢复检查改为手动演练一次，不写脚本。阶段顺序调整为 AI（阶段 4）在 AWS 部署（阶段 5）之前，见 docs/project-review.md 第 9 节。

长期目标和共同约定见 docs/project-review.md，接口约定见 docs/api.md。

## 这个阶段要学会什么

- 用 **Docker** 打包前后端（多阶段构建）。
- 用 **Nginx** 提供前端页面，并把 `/api` 转发给后端。
- 用 **Docker Compose** 一条命令启动整套服务：数据库、迁移、后端、Nginx。
- 用 **GitHub Actions** 在每次推送时自动运行测试、密钥扫描和镜像构建。
- 用 **pg_dump / pg_restore** 备份数据库，并实际恢复一次，确认备份能用。

这些也正是阶段 5 部署到 AWS 时要用到的东西。

## 前提

- 阶段 2 已合并到 dev 和 master。
- GitHub 远程仓库已创建并推送（里程碑 2 需要）。
- **不改变任何 API 行为**：路径、请求体大小限制、认证与 CSRF、错误响应格式都与 docs/api.md 一致。
- **日常开发方式不变**：`make dev`、`make test`、`make test-integration`、`make test-e2e` 照常使用。容器方式是新增的，不是替代。

## 不在本阶段范围内

AWS 和任何云资源、HTTPS 证书、推送镜像到仓库、自动部署（都在阶段 5），AI（阶段 4）和新的业务功能。

## 推迟到以后（本阶段刻意不做）

| 推迟的内容 | 为什么现在不需要 |
|---|---|
| 多个 Docker 网络、手动指定容器 IP | 后端端口不对外开放，只有一个默认网络也能保证安全（见安全底线 2） |
| 请求 ID 在 Nginx 和后端之间传递 | 本地规模下用时间和路径就能对上日志；阶段 5 接入 CloudWatch 时再考虑 |
| 自定义 panic 恢复中间件 | 是阶段 2 就存在的小缺口（panic 时返回的 500 没有 JSON 响应体），与 Nginx 无关，记入剩余问题，以后单独修 |
| 隔离的 compose 项目脚本、容器方式的自动 E2E | 容器方式用一次手动检查加 curl 验证（见验收）；自动 E2E 继续使用现有的宿主机方式 |
| CI 中的 E2E、故意制造失败的 CI 测试 | 先让基础 CI 稳定运行 |
| 自动的恢复检查脚本（`db-restore-check`）、逐表逐约束比对 | 手动恢复演练一次，确认「能恢复、迁移版本对、行数对」就足以证明备份可用 |
| CI 中的 `gofmt` 检查、在 GitHub 上把 job 设为合并前必须通过 | 先只跑测试和构建；必须通过的检查属于仓库设置，需要时由你在 GitHub 上配置 |
| 镜像摘要（digest）固定、Action 固定到 commit SHA | 先用明确的版本标签；两者的区别写进文档，以后需要时再收紧 |

## 安全底线（简化后仍然必须满足）

1. **数据库不对外开放**：PostgreSQL 仍然只绑定 `127.0.0.1`（宿主机方式要用）；后端容器**不映射任何端口**，只有 Nginx 映射 `127.0.0.1:8000`。
2. **限流不能被伪造的请求头绕过**。做法分两步，都很简单：
   - Nginx 用 `proxy_set_header X-Forwarded-For $remote_addr;` 把这个头**整个覆盖**成真实的来源 IP，不保留客户端传来的值。
   - 后端新增配置 `TRUSTED_PROXIES`，只在容器方式下由 compose 设置为私有网段（`10.0.0.0/8,172.16.0.0/12,192.168.0.0/16`）。只有当请求来自这些地址时，后端才读取 `X-Forwarded-For`；宿主机方式下不设置，行为与现在相同。

   为什么这样是安全的：后端没有映射到宿主机，外部客户端**只能经过 Nginx** 才能访问后端，而 Nginx 已经把这个头覆盖成了真实 IP。同一个 compose 网络里只有我们自己的容器。因此不需要固定 IP，也不需要多个网络。阶段 5 上云时，可信范围要改成负载均衡器的地址，届时会重新评估。
3. **镜像里没有密钥**：`.dockerignore` 排除 `.env`，所有配置都通过环境变量传入；后端以非 root 用户运行。
4. **日志不泄露敏感信息**：Nginx 访问日志只记录路径（不含 query），**不记录** query、Cookie、`Authorization`、CSRF 头和请求体；后端日志保持现状。
5. **API 行为不变**：
   - 请求路径原样转发：`proxy_pass` 后面不带路径，所以不会被改写。
   - Cookie、`Origin`、CSRF 头原样转发。
   - 后端返回的错误 JSON 原样传给客户端。
   - 请求体超限时仍然返回 JSON 格式的 `413 payload_too_large`，见里程碑 1 的 Nginx 配置。

## 实施里程碑

### 里程碑 1：Docker、Nginx、Compose

- **`backend/Dockerfile`**（多阶段构建）：
  - 第一阶段用 `golang` 镜像编译 `server` 和 `migrate` 两个二进制；
  - 第二阶段用 distroless 镜像（`gcr.io/distroless/static-debian12:nonroot`），只放这两个文件，以非 root 用户运行；
  - 给 `server` 加一个 `healthcheck` 子命令（访问自己的 `/api/health/live`），因为 distroless 镜像里没有 curl，compose 的健康检查要用它。
- **`frontend/Dockerfile`**（多阶段构建）：第一阶段用 `node` 镜像执行 `npm ci && npm run build`；第二阶段用 `nginx` 镜像，放入构建好的页面和配置文件。
- **`frontend/nginx.conf`**：
  - 前端页面：找不到的路径都返回 `index.html`（SPA 回退），这样直接打开或刷新 `/profile` 不会 404。`/assets/` 设置长期缓存，`index.html` 设置不缓存。
  - `/api/`：转发给 `backend:8080`；用 `$remote_addr` 覆盖 `X-Forwarded-For`；设置 `Host` 和 `X-Forwarded-Proto`。
  - 请求体大小：`client_max_body_size 1m`，所以 64 KB 到 1 MB 的请求会交给后端，由后端返回 JSON 格式的 413。超过 1 MB 时由 Nginx 直接拒绝，此时用 `error_page 413` 返回与后端**相同的 JSON**，否则 Nginx 会返回 HTML 页面，改变 API 的行为。
  - 基本安全：`server_tokens off`，以及 `X-Content-Type-Options: nosniff`、`X-Frame-Options: DENY`、`Referrer-Policy` 三个响应头。
  - 访问日志：JSON 格式，只包含时间、来源 IP、方法、路径（`$uri`）、状态码和耗时。
- **`compose.yaml`**：
  - 原有的 `postgres` 服务不变；
  - 在 profile `app` 下新增 `migrate`（跑一次 `migrate up` 后退出）、`backend`、`web` 三个服务；
  - 启动顺序用 `depends_on` 控制：数据库 healthy → 迁移成功 → 后端 healthy → Nginx；
  - 容器内的 `DB_HOST` 改为 `postgres`；`ALLOWED_ORIGINS` 加上 `http://localhost:8000`。
  - `docker compose up -d` 和 `make db` 仍然只启动数据库，所以 `make dev` 不受影响。
- **`.dockerignore`**：排除 `.env`、`node_modules`、`dist`、`bin`、`.git`、`backups`。
- **镜像版本**：全部写明确的版本标签，例如 `golang:1.26.2-alpine`、`node:24-alpine`、`nginx:1.28-alpine`、`postgres:17.x`，不使用 `latest`。docs/operations.md 中用一段话说明「版本标签」和「镜像摘要（digest）」的区别：标签可以被重新指向新的内容；摘要是内容的哈希，永远不变，但需要手动更新。本阶段只用标签。
- **后端**：新增 `TRUSTED_PROXIES` 配置（见安全底线 2），并补充单元测试：可信来源的 `X-Forwarded-For` 生效，不可信来源的被忽略。
- **Makefile**：`app-up`、`app-down`、`app-logs`。

**状态：已实施，验收第 1–6 项已实际执行并通过（2026-10-24）。** 实施时的调整：

- 未知的 `/api/...` 路径原来返回纯文本 404，改为返回 docs/api.md 规定的 JSON `404 not_found`。
- 后端只从 `X-Forwarded-For` 读取客户端 IP，不读 `X-Real-IP`。
- Nginx 日志的路径取自 `$request_uri` 去掉 query 的部分，因为 `$uri` 在 SPA 回退后会变成 `/index.html`。
- 新增 `WEB_PORT`（默认 8000）；容器方式的 `ALLOWED_ORIGINS` 为 `http://localhost:${WEB_PORT}`。
- Nginx 健康检查使用 `/nginx-health`（不记日志）。
- 两个镜像都以仓库根目录为构建上下文，使根目录的 `.dockerignore` 生效。
- `make app-*` 先读取 `.env` 再调用 compose，因为 compose 会优先使用 shell 中已导出的同名变量。`make app-down` 会同时停止数据库（数据保留）。

### 里程碑 2：GitHub Actions（只跑检查和构建）

`.github/workflows/ci.yml`。在 push 到 `dev`、`master`，以及以它们为目标的 PR 时运行；权限只设为 `contents: read`；Action 使用明确的主版本标签，例如 `actions/checkout@v4`。

| Job | 做什么 |
|---|---|
| `secrets` | 拉取完整历史，运行 gitleaks 扫描全部提交，版本与 `.pre-commit-config.yaml` 中的一致 |
| `backend` | `go vet`、`go test`。配置一个 PostgreSQL 服务容器并设置 `TEST_DATABASE_URL`，这样集成测试会真正执行，不会被跳过 |
| `frontend` | `npm ci`、`npm run build`、`npm test` |
| `docker` | 构建后端和前端两个镜像，不推送 |

**状态：已实施（2026-10-24），各 job 的命令已在本地模拟通过；在 GitHub 上的运行结果待推送后确认。** 实施时的调整：

- Action 使用当前的主版本 `@v7`（checkout、setup-go、setup-node），checkout 设置 `persist-credentials: false`。
- gitleaks 加 `--redact`，即使发现问题也不会把密钥打印到公开的 CI 日志里。
- `internal/testdb`：设置了 `CI` 环境变量（GitHub Actions 会自动设置）而没有 `TEST_DATABASE_URL` 时，集成测试直接失败而不是跳过。这样「集成测试真的执行了」由 CI 本身保证，不必每次翻日志。
- 前端测试在 UTC 和 Asia/Tokyo 两个时区各跑一次，与 `make test` 一致。

### 里程碑 3：备份与文档

- **`make db-backup`**：用 `pg_dump -Fc` 导出数据库，保存到 `backups/mogumogu-<时间>.dump`。`backups/` 加入 `.gitignore`，因为备份里有个人资料和密码哈希。
- **恢复演练（手动一次，不写脚本）**：按 `docs/operations.md` 中的步骤，把备份恢复到一个**新建的临时库**（库名以 `mogumogu_restore_` 开头，**不覆盖开发库**），确认迁移版本是 5 且 `dirty = false`、各表行数与源库一致，然后删除临时库。
- **文档**：
  - README 补充容器方式的启动与停止、CI、备份的入口；
  - `docs/operations.md` 补充：两种运行方式的区别、安全底线各项的理由（简短）、备份和恢复的步骤（正式恢复会覆盖数据，需要手动执行，不提供一键命令）、常见问题（例如 shell 中已导出的变量会覆盖 `.env`）。

## 验收标准（9 项）

每一项在完成报告中都要标明「实际执行」或「未验证」。会写入测试数据的检查，在单独的 compose 项目中进行（`docker compose -p mogumogu-verify`，使用自己的 volume 和端口），检查完执行 `down -v` 删除，**不在开发库里留下测试数据**。

1. **启动**：在空的 volume 上用容器方式启动，所有服务 healthy，`migrate` 正常退出；只有 Nginx 映射到 `127.0.0.1`，后端没有映射任何端口。
2. **页面**：通过 Nginx 完成注册 → 填写 Profile → 日历增删改查 → 退出 → 再登录；直接打开并刷新 `/profile` 不出现 404。
3. **API 行为不变**：用 curl 检查以下几项：
   - 未知的 `/api/...` 路径返回 JSON 404；
   - 请求体略大于 64 KB 和超过 1 MB 时，都返回同样的 JSON `413 payload_too_large`；
   - Origin 不对时返回 JSON `403 csrf_failed`；
   - 登录后 `Set-Cookie` 的 `Path=/api`、`HttpOnly`、`SameSite` 属性不变。
4. **限流不能被绕过**：经过 Nginx，用错误密码登录 5 次，每次带不同的伪造 `X-Forwarded-For`，第 6 次返回 429。
5. **镜像与日志安全**：
   - 后端镜像以非 root 用户运行，镜像里没有 `.env` 和源码；
   - Nginx 响应带有安全响应头，并且不暴露版本号；
   - 发送一个 query 和 Cookie 里都带特征字符串的请求，然后在 Nginx 日志里搜索这个字符串，搜不到。
6. **停止与数据保留**：`make app-down` 之后再 `make app-up`，数据仍然存在；后端日志里有优雅退出的过程。
7. **CI**：推送后 4 个 job 都通过；在 `backend` job 的日志中确认集成测试真的执行了，没有被跳过。
8. **备份恢复**：在应用停止、没有写入时，执行 `make db-backup`，再按文档手动恢复到临时库：恢复成功，迁移版本为 5，每张表的行数与源库一致，临时库已被删除，开发库没有变化。
9. **原有流程不受影响**：`make test`、`make test-integration`、`make test-e2e` 全部通过；按照 README，从一个新的 clone 可以用容器方式把服务启动起来。

## 完成报告

报告修改内容、运行方式、9 项验收的结果（实际执行还是未验证）、使用的镜像版本和大小，以及剩余问题。剩余问题包括「推迟到以后」表中的各项，以及 panic 时返回的 500 缺少 JSON 响应体。
