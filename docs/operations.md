# 运维说明

本文件说明两种运行方式、容器方式的安全设计、CI、备份与恢复、云上部署，以及常见问题。日常命令见 [README](../README.md)。

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

云上没有负载均衡器，最前面是同一台机器上的 Caddy：Nginx 只相信来自容器网段的 `X-Forwarded-For`，后端的 `TRUSTED_PROXIES` 也是这个网段（`deploy/nginx-realip.conf`、`deploy/compose.yaml`）。

## 3. 镜像版本

| 用途 | 镜像 |
|---|---|
| 后端编译 | `golang:1.26.2-alpine` |
| 后端运行 | `gcr.io/distroless/static-debian12:nonroot` |
| 前端构建 | `node:24-alpine` |
| 前端运行（Nginx） | `nginx:1.28-alpine` |
| AI 服务构建与运行 | `python:3.14.7-slim`；uv 取自 `ghcr.io/astral-sh/uv:0.12.23` |
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
| `ai` | 安装固定版本的 uv，`uv sync --locked`（锁文件与 pyproject.toml 不一致时失败）、`pytest`。只用假模型，不需要 AWS 凭证 |
| `docker` | 构建后端、前端和 AI 服务的镜像，不推送 |

CI 失败时 GitHub 默认仍允许合并。要强制「通过后才能合并」，在仓库的 Settings → Branches 中为 `dev` 和 `master` 添加保护规则，把这 5 个 job 设为必须通过的检查。

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

## 6. 云上部署（AWS）

线上地址：https://mogumogu.teiyou416.ninja 。设计和取舍见 [plans/phase-5.md](../plans/phase-5.md)。

### 6.1 组成

| 部分 | 内容 | 定义在 |
|---|---|---|
| 服务器 | 一台 EC2 t4g.micro（arm64），固定公网 IP；安全组只开放 80/443，没有 SSH | `infra/main/compute.tf`、`network.tf` |
| 容器 | Caddy（HTTPS 证书自动申请和续期）→ Nginx → 后端 / AI 服务；`migrate` 每次部署先执行一次 | `deploy/compose.yaml`、`deploy/Caddyfile` |
| 数据库 | RDS PostgreSQL 17，私有子网，只允许服务器访问，强制 TLS；每天自动备份，保留 7 天 | `infra/main/database.tf` |
| 镜像 | ECR 的 `mogumogu/{backend,web,ai}`，标签是 12 位提交号，不可覆盖，保留最近 10 个 | `infra/main/registry.tf` |
| 配置和密钥 | SSM Parameter Store `/mogumogu/*`：3 个密钥是 SecureString，域名、数据库地址、模型 ID 是普通参数 | `scripts/put-secrets.sh`、`infra/main/settings.tf` |
| 权限 | 服务器的 IAM 角色：Session Manager、指定的 Bedrock 模型、拉取镜像、读取 `/mogumogu/*` 和部署桶、向日志组写入日志。**没有任何长期 Access Key** | `infra/main/iam.tf` |
| 日志和告警 | 所有容器的日志写入 CloudWatch Logs `/mogumogu/app`，保留 14 天；访客收到 5xx 时发邮件（6.6） | `infra/main/logs.tf`、`deploy/compose.yaml` |
| 费用告警 | AWS Budgets，每月 50 美元，超过 50%、75%、100% 和预测超过 100% 时发邮件 | `infra/main/budgets.tf` |

本机执行的 AWS 命令都使用管理员 profile：先 `aws login --profile Teiyou`，再在命令前加 `AWS_PROFILE=Teiyou`。

### 6.2 首次搭建

只需要做一次，按顺序执行。环境被删除后重建时也按这个顺序：

1. **状态桶**：`cd infra/bootstrap && AWS_PROFILE=Teiyou terraform init && AWS_PROFILE=Teiyou terraform apply`。它的状态文件留在本地，不提交。
2. **密钥**：`AWS_PROFILE=Teiyou scripts/put-secrets.sh`。数据库密码也在其中，必须在第 3 步之前创建。
3. **其余资源**：把 `infra/main/terraform.tfvars.example` 复制为 `terraform.tfvars`（已被 gitignore），填上收告警的邮箱，然后在 `infra/main` 里执行 `terraform init` 和 `terraform apply`。最后会输出公网 IP。
4. **DNS**：在 name.com 添加 A 记录 `mogumogu` → 公网 IP。生效后 Caddy 才能申请到证书。
5. **发布**：按 6.3 执行 `make push` 和 `make deploy`。
6. **演示数据**：见 6.8。

### 6.3 发布

```sh
AWS_PROFILE=Teiyou make push     # 构建 HEAD 的 arm64 镜像并推到 ECR；工作区有未提交的改动时拒绝执行
AWS_PROFILE=Teiyou make deploy   # 部署 HEAD；也可以指定 TAG=<12 位提交号>
```

`make deploy` 的过程如下：
1. 确认三个镜像都已在 ECR 中。
2. 把 `deploy/` 上传到部署桶。
3. 通过 SSM Run Command 让服务器下载这些文件，并执行 `deploy.sh`。
4. 等待执行完成，打印输出。失败时返回非 0。

`deploy.sh` 在服务器上执行以下步骤：
1. 从 Parameter Store 生成 `.env`，只有 root 可读。
2. 拉取镜像。
3. `migrate` 成功后，按顺序启动各个服务，等到全部 healthy。
4. 清理不再使用的镜像。

全过程不使用 SSH，本机和服务器都不保存长期凭证。

发布后检查：

```sh
curl -s https://mogumogu.teiyou416.ninja/api/health/ready   # {"status":"ready"}
```

只发布 master 上的提交，这样线上运行的代码和 GitHub 上的 master 保持一致。合并到 master 之前，在本地依次检查：
1. `make test`
2. `make test-integration`
3. 改了界面时跑 `make test-e2e`
4. 改了 AI 时跑 `make ai-eval`
5. 用 `make app-up` 以容器方式手动点一遍

没有单独的 staging 环境。

### 6.4 回滚

```sh
AWS_PROFILE=Teiyou make deploy TAG=<之前的提交号>
```

ECR 只保留最近 10 个镜像，更早的版本需要先切换到那个提交，再执行 `make push`。

**数据库只向前迁移。** 旧版本的 `migrate` 不认识比它新的迁移版本，会报错退出（golang-migrate 找不到当前版本的迁移文件）。这样部署会失败，而不会让旧代码去读写新的表结构。因此：
- 回滚的目标版本不能早于最近一次迁移。目前线上是版本 6，阶段 5 及以后的提交都可以互相回滚。
- 新迁移应只做向后兼容的修改（例如只新增表和列）。需要撤销一次迁移时，写一个新的迁移，然后向前发布。
- 数据本身出问题时，从 RDS 的自动备份恢复到某个时间点。恢复会生成一个新的实例，需要修改 Terraform 指向它，详见里程碑 4 的恢复演练。

### 6.5 登录服务器

服务器没有 SSH。需要时通过 Session Manager 登录（本机要先安装 `session-manager-plugin`）：

```sh
AWS_PROFILE=Teiyou aws ssm start-session --region ap-northeast-1 \
  --target "$(AWS_PROFILE=Teiyou terraform -chdir=infra/main output -raw instance_id)"
```

登录后的用户是 `ssm-user`。部署目录 `/opt/mogumogu` 只有 root 可以访问，所以下面的命令都要用 `sudo`。

### 6.6 查看日志

所有容器的日志都写入 CloudWatch Logs 的日志组 `/mogumogu/app`，保留 14 天（`infra/main/logs.tf`）。每个容器一个日志流，名字是「容器名/容器 ID」，例如 `mogumogu-web-1/3f2a…`。几种日志的格式：
- **web（nginx）**：每个请求一行 JSON，字段为 `time`、`remote_addr`（客户端 IP）、`method`、`path`（不含查询参数）、`status`、`duration_s`。只有这种日志带 `remote_addr`，统计访问量时用它来筛选。
- **backend**：slog 输出的 JSON，请求日志的 `msg` 为 `request`，字段为 `method`、`path`、`status`、`duration_ms`；错误日志的 `level` 为 `ERROR` 或 `WARN`。
- **ai、caddy、migrate**：普通文本或各自的 JSON。

**在终端实时查看：**

```sh
AWS_PROFILE=Teiyou aws logs tail /mogumogu/app --region ap-northeast-1 --follow --since 10m
AWS_PROFILE=Teiyou aws logs tail /mogumogu/app --region ap-northeast-1 --since 1h --log-stream-name-prefix mogumogu-backend
```

**统计：** 在控制台打开 CloudWatch → Logs Analytics，选择 `/mogumogu/app`，设置时间范围后运行查询。控制台的时间默认显示为 UTC，可以在右上角切换为本地时间。

```
# 每小时的请求数
filter ispresent(remote_addr)
| stats count(*) as requests by bin(1h)

# 每天的独立 IP 数（大致的访客数）
filter ispresent(remote_addr)
| stats count_distinct(remote_addr) as visitors by bin(1d)

# 访问最多的路径（不含静态资源）
filter ispresent(remote_addr) and path not like /^\/assets\//
| stats count(*) as requests by path
| sort requests desc
| limit 20

# 5xx 明细
filter ispresent(remote_addr) and status >= 500
| fields @timestamp, method, path, status, duration_s
| sort @timestamp desc

# 后端的错误和警告
filter level = "ERROR" or level = "WARN"
| fields @timestamp, msg, error
| sort @timestamp desc

# 慢请求（超过 1 秒）
filter ispresent(remote_addr) and duration_s > 1
| fields @timestamp, method, path, status, duration_s
| sort duration_s desc
| limit 20

# 饮食助手每天的调用次数，按状态码分开（429 = 额度用完，503 = AI 服务失败）
filter ispresent(remote_addr) and method = "POST" and path = "/api/assistant/messages"
| stats count(*) as calls by bin(1d), status
```

查询按扫描的数据量收费，以现在的日志量每次不到 0.01 美元。

**5xx 告警：** 访客在 5 分钟内收到任何一个 5xx 时，告警 `mogumogu-http-5xx` 会向预算告警的邮箱发邮件；恢复正常后再发一封。第一次 `terraform apply` 后，要先点击 AWS 发来的订阅确认邮件，才能收到告警。收到告警后：
1. 用上面的「5xx 明细」查询找到出错的路径和时间；
2. 用「后端的错误和警告」，或者 `--log-stream-name-prefix mogumogu-ai` 查看同一时间的日志；
3. 如果是部署后才出现的问题，考虑回滚（6.4）。

服务器整台宕机时不会产生日志，这个告警也不会触发。

**在服务器上查看：** CloudWatch 不可用时，也可以登录服务器（6.5）查看 Docker 在本地保留的副本：

```sh
cd /opt/mogumogu   # 先 sudo -i
docker compose ps
docker compose logs --tail 100 backend        # 也可以是 ai、web、caddy、migrate
docker compose logs -f --since 10m            # 持续查看所有服务
```

本地副本只保留最近一部分，重新部署后旧容器的副本会被删除；CloudWatch 中的日志不受影响。

### 6.7 更换密钥

`put-secrets.sh` 只创建不存在的参数。要更换某个密钥，先删除这个参数，再执行一次脚本生成新值，然后重新部署：

```sh
export AWS_PROFILE=Teiyou
aws ssm delete-parameter --region ap-northeast-1 --name /mogumogu/jwt_secret   # 或 csrf_secret
scripts/put-secrets.sh
make deploy TAG=<线上当前的提交号>
```

- 更换 `jwt_secret` 后，所有人都需要重新登录。更换 `csrf_secret` 后，页面需要刷新一次。
- **数据库密码**还需要同步修改 RDS：在删除并重新生成 `/mogumogu/db_password` 之后，把 `infra/main/database.tf` 中的 `password_wo_version` 加 1，然后执行 `terraform apply`，最后 `make deploy`。在 apply 和 deploy 之间，后端连不上数据库，网站会短暂不可用。

### 6.8 演示账号

`scripts/seed-demo.mjs` 只通过公开的 API 创建演示账号 `demo`，写入 Profile、最近两周的行程和饮食记录，以及明天的行程。饮食记录故意留了一些空缺，用来检查 AI 是否会把「没有记录」当成「没吃」。

```sh
read -rs DEMO_PASSWORD && export DEMO_PASSWORD   # 密码不会显示，也不写入 Git
node scripts/seed-demo.mjs                        # 默认作用于线上；SEED_URL=http://localhost:8000 可用于本地
```

日期按运行当天计算。重新执行时，会先删除这段时间内的记录再重新写入，所以数据变旧或被访客改乱时，再执行一次即可。演示账号的密码是公开的，所以始终保持 Normal 等级（6.10），每天最多可以问 AI 30 次。全站每天合计 100 次。

### 6.9 本地测试账号

`make seed-local`（`scripts/seed-local.mjs`）在本地创建三个测试账号，行程量各不相同，饮食记录相同，用来在不同忙碌程度下检查日历和饮食助手：

| 账号 | 人物 | 行程（过去 14 天到未来 7 天） |
|---|---|---|
| `busy`（Pro） | 上班族，减脂 | 约 85 条：工作日每天 4–5 个，有紧挨着、重叠和跨午夜的行程，周末也有安排 |
| `normal` | 学生，增肌 | 约 35 条：与演示账号相同的课程、实验和健身 |
| `light` | 自由职业者，减脂，过敏原「えび」 | 约 4–5 条：周六瑜伽，隔周三晚饭 |

```sh
make dev           # 另开一个终端先启动本地服务
make seed-local    # 密码是 .env 中的 SEED_PASSWORD
```

脚本完成后，`make seed-local` 用 `cmd/admin` 把 `busy` 设为 Pro，另外两个账号保持 Normal，用来对比两种等级。和 6.8 一样，重新执行会删除这三个账号在这段时间内的记录再重新写入。脚本默认使用 `http://localhost:5173`，`SEED_URL` 可以改为 `http://localhost:8000`（`make app-up`）；不是本机地址时脚本会拒绝运行。修改 `SEED_PASSWORD` 后，已存在的账号会因为密码不一致而登录失败，这时需要先删除这些账号，或者改回原来的密码。

### 6.10 账号等级

账号等级（Normal、Pro，见 plans/archive/account-tiers.md）只能用 `cmd/admin` 修改。它直接连接数据库，没有对应的 API，也没有管理页面，所以只有能登录服务器的人才能修改。修改后立即生效，用户不需要重新登录。

```sh
# 本地（作用于 .env 中的数据库）
make admin ARGS="users"
make admin ARGS="tier busy pro"

# 线上：先登录服务器（6.5），再执行
sudo -i
cd /opt/mogumogu
docker compose run --rm migrate /admin users
docker compose run --rm migrate /admin tier <username> pro     # 或 normal
```

- 线上借用 `migrate` 服务运行，因为它用的是同一个 backend 镜像，并且已经有数据库的连接设置；`/admin` 替换了它原来的 `/migrate up` 命令，不会执行迁移。
- 输出同样会写入 CloudWatch Logs（日志流 `mogumogu-migrate-run-…`），其中包含用户名。
- 不要把演示账号设为 Pro：它的密码是公开的，任何人都能用它把全站当天的额度用完。

## 7. 常见问题

- **容器方式的后端启动失败，日志显示 `JWT_SECRET must be at least 32 bytes`，但 `.env` 里的值是对的**：compose 会优先使用 shell 中已经导出的同名变量，而不是 `.env`。用 `printenv JWT_SECRET | wc -c` 确认（只看长度）。`make app-*` 已经先读取 `.env` 来避免这个问题；直接运行 `docker compose` 时需要注意。
- **改了代码，容器方式下看不到变化**：容器运行的是构建好的镜像，重新执行 `make app-up`（会重新构建）。
- **`make app-down` 之后，正在运行的 `make dev` 后端连不上数据库**：`app-down` 会停止共用的数据库，执行 `make db`（新执行 `make dev` 时会自动启动数据库）。
- **单独重建后端容器后，页面上的请求返回 502**：Nginx 只在启动时解析一次 `backend` 的地址。执行 `make app-up`，或者 `docker compose restart web`。
- **后端停止时，通过 Nginx 访问 `/api` 返回 HTML 格式的 502**：这是 Nginx 自己生成的错误页，后端恢复后即正常。
- **端口 8000 被占用**：在 `.env` 中设置 `WEB_PORT`，例如 `WEB_PORT=8001`，然后访问 http://localhost:8001 。
- **想查看容器日志**：`make app-logs`。后端和 Nginx 的日志都是每行一条 JSON。
- **想删除所有数据重来**：`docker compose --profile app down -v`。这会**永久删除** volume，请先确认是否需要备份。
