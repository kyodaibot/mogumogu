# 食堂菜单导入器上线（Digdag）

`plans/cafeteria-menus.md` 交付顺序的最后一步：把 importer 和 Digdag 调度部署到现有 EC2，每天自动导入菜单。

## 已确定

- 调度用 Digdag（`importer/menus.dig`：日本时间 08:00–23:59 每分钟一次），沿用原计划。
- 先在现有 t4g.micro（1 GB）上运行。2026-11-23 夜间空闲时可用内存约 459 MB，swap 已用 108 MB。内存不够时把 `instance_type` 升到 t4g.small（只改一个变量，会短暂停机）。
- 是否到期、重试和跨日由 `menu_import_jobs` 决定，Digdag 只负责每分钟触发；漏跑或重启后下一分钟接着做。

## 要做的改动

1. **镜像** `importer/Dockerfile`：Temurin 11 JRE（Digdag 0.10 只支持 Java 8 或 11）+ Python + 预先安装好依赖的 importer + Digdag 0.10.5.1 jar（GitHub 未公布校验值，固定首次下载的 SHA-256）。容器启动后运行 `digdag scheduler`（本地模式，内存数据库），JVM 堆 160 MB；Digdag 不清理内存中的历史运行记录，堆满时 JVM 退出并由 compose 重启。`menus.dig` 改为用镜像里的 venv 运行 `python jobs.py`。
2. **compose**：`deploy/compose.yaml` 新增 `importer` 服务，只传 `MENU_DATABASE_URL`（时区在镜像里设置）；设置内存上限（约 384 MB）、失败自动重启、日志写入 CloudWatch。
3. **部署脚本**：`deploy.sh` 从 SSM 读 `/mogumogu/menu_db_password`（只存密码，compose 拼出带 `sslmode=require` 的连接串）；`Makefile` 的 `push`、`deploy` 的镜像检查和 CI 的 docker 构建都加上 importer 镜像。
4. **Terraform**：`infra/main/registry.tf` 的 `services` 加 `importer`，新建 ECR 仓库。
5. **文档**：`docs/operations.md` 6.14 补上线上运行方式、内存观察和升级实例的做法。

## 需要你手动做的

逐条命令见 `docs/operations.md` 6.14：

1. `terraform apply`，创建 importer 的 ECR 仓库。
2. 登录服务器，用管理员身份连 RDS，创建 `menu_importer` 角色（6.14 的 SQL）。
3. 把它的密码存为 SecureString `/mogumogu/menu_db_password`。

实际顺序以 `docs/operations.md` 6.14 为准：SSM 参数要在合并到 master 之前建好，角色要等迁移建好菜单表后（即部署之后）再创建。

## 验收

1. 本地构建 arm64 镜像，用只能写菜单表的角色连本地库：一分钟内触发导入并写入快照；该角色查询 `users` 被拒绝。
2. 本地 `docker stats` 观察 importer 容器空闲和导入时的内存，记录数值。
3. 重启容器后，下一分钟从任务表接着导入，不重复发布已有快照。
4. CI 能构建 importer 镜像；`make push` 能推到 ECR。
5. 上线后第二天 08:00 起七家食堂都有当天快照；在午饭前后查一次服务器的 `free -m` 和 `docker stats`。

## 不做

- 不加 Digdag 的服务端模式、Web 界面或 PostgreSQL 存储（本地模式足够，状态在任务表里）。
- 暂不升级 EC2，按验收第 5 步的数据决定。

## 进展（2026-11-23）

- 验收 1：已执行。本地 arm64 镜像连一次性数据库（受限角色），23:11 第一次触发，七家食堂当天快照全部发布（283 道菜，营业时间 7 行），约 3.5 分钟，期间的触发按 `skip_on_overtime` 跳过；该角色查询 `users` 返回 permission denied。这也是第一次实际读取生协官网。
- 验收 2：已执行。容器内存导入中约 217 MiB，空闲 175–190 MiB。
- 验收 3：已执行。重启容器后 23:17 照常触发，快照仍为 7 份、版本均为 1。
- 验收 4：部分执行。本地 `docker build` 成功；CI 和 `make push` 未验证（需要推送和 AWS 登录）。`terraform validate` 通过，`terraform plan` 因本机 AWS 登录过期未执行。
- 验收 5：未验证（上线后）。
