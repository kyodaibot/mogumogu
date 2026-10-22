# 阶段 1：最小本地 infra 与前后端骨架

长期目标和共同约定见 docs/project-review.md。本文件规定阶段 1 的具体范围和验收标准。

## 目标

- PostgreSQL 通过 Docker Compose 在本地运行。
- Go 后端和 Vue 前端在宿主机运行。
- 前端开发状态页通过 Vite 代理显示后端和数据库的状态。
- 提交前自动检测密钥等敏感信息。

## 不在本阶段范围内

账号与认证、日历、业务 CRUD、数据库迁移和业务表、AI（不创建 Python 项目）、Nginx、前后端容器化、CI、云资源、前端路由和状态管理库。

## 目录结构

```
mogumogu/
├── AGENTS.md
├── CLAUDE.md
├── README.md
├── compose.yaml
├── .env.example
├── .gitignore
├── .pre-commit-config.yaml
├── backend/        # Go
├── frontend/       # Vue
├── docs/
└── plans/
```

如果仓库已有不同结构，以现有结构为准，并在完成报告中说明。

## 任务

### 1. 仓库基础

- .gitignore 至少包含：.env 及 .env.*（保留 .env.example）、node_modules、前端构建产物、Go 编译产物、.venv、.DS_Store、个人 AI 工具本地配置（如 .claude/settings.local.json、CLAUDE.local.md）。
- .pre-commit-config.yaml 配置 gitleaks hook，rev 固定为具体版本。
- 用 gitleaks 扫描一次完整 Git 历史，在完成报告中写明结果。如有发现，只报告位置和类型，不在报告中复制密钥内容。

### 2. PostgreSQL

- compose.yaml 定义 postgres 服务，镜像固定主版本号（如 postgres:17）。
- 数据库名、用户名、密码从 .env 读取。
- 使用命名 volume 保存数据。
- 端口只绑定到 127.0.0.1；宿主机端口可通过 .env 配置，默认 5432。
- 配置 pg_isready 健康检查。

### 3. Go 后端（backend/）

配置：
- 通过环境变量读取监听端口和数据库连接参数（主机、端口、用户、密码、库名、sslmode）。
- 缺少必需配置时启动失败，并给出清晰的错误信息；错误信息和日志中不出现密码。

数据库连接：
- 使用 GORM 连接 PostgreSQL，设置基本的连接池参数。
- 数据库不可用时进程仍正常启动，由 readiness 反映状态；数据库恢复后无需重启即可恢复就绪。
- 不使用 AutoMigrate，不创建任何表。

日志：
- 使用 log/slog 以 JSON 格式输出到 stdout。
- 记录请求日志：方法、路径、状态码、耗时。

接口：
- GET /api/health/live：不访问数据库，返回 200。
- GET /api/health/ready：在超时时间内（约 2 秒）ping 数据库，成功返回 200，失败返回 503。响应中不暴露连接串或内部错误细节。

优雅退出：
- 收到 SIGINT 或 SIGTERM 后停止接收新请求，在超时时间内等待进行中的请求完成，关闭数据库连接池后退出。

测试：
- 配置解析的单元测试（包括缺少必需配置的情况）。
- live 和 ready 接口的 handler 测试，ready 需覆盖数据库可用和不可用两种情况，不依赖真实数据库。

### 4. Vue 前端（frontend/）

- 使用 Vite 创建 Vue 3 + TypeScript 项目，使用 npm，提交 package-lock.json，声明 Node 版本。
- 开发状态页调用 live 和 ready 接口，分别显示后端和数据库的状态，包括加载中和请求失败状态，并提供刷新按钮。
- Vite 开发服务器把 /api 代理到后端；代理目标可通过环境变量配置，默认 http://localhost:8080。
- 不引入路由、状态管理库或 UI 组件库。

### 5. 配置与文档

- .env.example 列出所有环境变量，使用明显的占位值，并逐项简要注释。
- README 包含：
  - 前置依赖（Docker、Go、Node、pre-commit）及 pre-commit 安装步骤（如 uv tool install pre-commit、pre-commit install）。
  - 初次配置步骤。
  - 启动数据库、后端、前端的命令。
  - 停止命令。明确说明 `docker compose down` 保留数据，`docker compose down -v` 会删除数据。
  - 测试与验证命令。

## 验收标准

每项注明是否实际执行。未执行的项必须标为“未验证”。

1. `docker compose up -d` 后，`docker compose ps` 显示 postgres 为 healthy。
2. 后端启动后，`curl -i http://localhost:8080/api/health/live` 和 `/api/health/ready` 均返回 200。
3. `docker compose stop` 后，live 仍返回 200，ready 返回 503，后端进程不退出；`docker compose start` 后，ready 无需重启后端即恢复 200。
4. 删除 .env 中的必需变量后启动后端：启动失败，错误信息清晰，且不包含密码。
5. 按 Ctrl+C 停止后端，日志显示优雅退出过程，进程正常结束。
6. `npm run dev` 后打开状态页，后端和数据库均显示正常；停止数据库后刷新，显示数据库不可用。
7. 数据持久化：用 psql 手动创建临时表，执行 `docker compose down` 和 `docker compose up -d` 后表仍存在，验证完毕后删除该表。
8. `go vet ./...`、`go test ./...`、`npm run build`（含类型检查）均通过。
9. 在未提交的文件中写入一个格式符合 gitleaks 规则的假令牌，尝试提交时被 pre-commit 阻止；验证后删除该文件，不提交。
10. 检查后端日志，确认没有输出密码或完整连接串。
11. README 中的命令与实际代码一致，并已按 README 从头执行一遍。

## 完成报告

报告修改内容、运行方式、上述各项验收的结果（实际执行或未验证），以及剩余问题。
