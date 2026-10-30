# 阶段 5：AWS 部署

> 状态：**已确认**（2026-10-27）。按里程碑逐个实施，每个里程碑开始前由用户安排。

长期目标和共同约定见 docs/project-review.md（第 5、8、9 节），容器运行方式和运维约定见 docs/operations.md。

阶段 4a 完成后先上云，4b（多轮对话）在云上迭代（见 project-review 第 9 节）。

## 这个阶段要学会什么

- 用 **Terraform** 描述一套最小但完整的 AWS 环境：VPC、安全组、EC2、RDS、ECR、IAM、Budgets；状态文件放在 S3。
- 理解 **IAM 角色**和临时凭证：EC2 用实例角色调用 Bedrock、拉取镜像；GitHub Actions 用 OIDC 换取临时凭证。全程不创建长期 Access Key。
- 在一台 EC2 上用 Docker Compose 运行整套服务，用 **Caddy** 自动申请和续期 HTTPS 证书。
- 用 **SSM**（Session Manager、Run Command、Parameter Store）登录、部署和保存密钥，不开 SSH 端口。
- 一条可回滚的发布流程：镜像按提交号打标签，发布就是让服务器切换到某个标签。

## 前提

- 阶段 4a 已合并到 dev（`d7aed0a`）。dev 已 push，GitHub CI 全部通过。
- **由你完成的准备**：
  - **域名**：使用已有域名的子域名 `mogumogu.teiyou416.ninja`。teiyou416.ninja 的 DNS 托管在 name.com，里程碑 1 完成后，在 name.com 手动添加一条 A 记录，指向 Terraform 输出的 Elastic IP。这个域名没有 CAA 记录，Let's Encrypt 可以签发证书。
  - **准备运行 Terraform 的身份。** 现在的 `dev` 只有 Bedrock 权限，不够用。新建一个名为 `Teiyou` 的 profile，与 IAM 用户同名：执行 `aws login --profile Teiyou`，在浏览器中用 IAM 用户 Teiyou（admins 组，拥有 AdministratorAccess）登录，区域填 ap-northeast-1。运行 Terraform 时使用 `AWS_PROFILE=Teiyou`。`dev` 继续只给本地 AI 服务使用。同样不创建长期 Access Key。
  - **本机安装 Terraform。** 版本至少 1.11，因为数据库密码要用 write-only 参数，避免写进状态文件。安装当前的稳定版，在代码里固定小版本。
  - **确认接收预算告警的邮箱。** 它写在本地的 `terraform.tfvars` 里，这个文件不提交。
- 仓库是公开的，下面几样不写进 Git、也不打印到 CI 日志：AWS 账号 ID、角色 ARN、邮箱。

## 不在本阶段范围内

- 多轮对话（阶段 4b）。
- 多可用区、自动伸缩、ALB、CloudFront、WAF、蓝绿部署。
- 单独的 staging 环境。
- 自定义 CloudWatch 告警和仪表盘，只做日志收集。（后来在 plans/observability.md 中加了一个 5xx 告警，仪表盘仍然不做。）
- 把本地数据库的数据迁移到 RDS：上线后从空库开始，再写入演示数据。
- 开启 RDS 证书校验（`verify-full`）：本阶段只加密连接。

## 关键设计（实现前确认）

| 项目 | 做法 |
|---|---|
| 区域 | ap-northeast-1（东京），和 Bedrock 调用的区域相同 |
| 域名 | `mogumogu.teiyou416.ninja`。DNS 留在 name.com，只手动维护这一条 A 记录。Elastic IP 不变，A 记录也就不用改，所以不迁移到 Route 53，也不让 Terraform 管理 DNS |
| 网络 | 新建一个 VPC：1 个公有子网放 EC2（只有一台实例、不用 ALB，一个就够），2 个私有子网放 RDS（子网组要求两个可用区）。**不建 NAT 网关**（每月约 45 美元）：EC2 通过公网 IP 访问 ECR、Bedrock 和 SSM；RDS 不需要访问外网 |
| 安全组 | EC2 只开放入站 80 和 443，**不开 22**，用 SSM Session Manager 登录。RDS 只允许来自 EC2 安全组的 5432 |
| EC2 | t4g.micro（arm64，1 GB 内存，另加 1 GB swap），Amazon Linux 2023，20 GB 加密 gp3。绑定 Elastic IP，DNS 记录不随重启变化。强制 IMDSv2，**跳数上限设为 2**，否则容器里取不到实例角色的凭证。内存不够时改成 t4g.small，只改一个变量 |
| RDS | PostgreSQL 17，db.t4g.micro，单可用区，20 GB 加密 gp3，不公开访问。自动备份保留 7 天，开启删除保护，删除时保留最终快照。连接使用 `DB_SSLMODE=require` |
| 镜像 | ECR 的三个仓库：backend、web、ai。镜像标签使用 Git 提交号，不可覆盖。每个仓库只保留最近 10 个镜像。镜像为 arm64，本地 Mac 可以直接构建，CI 用 GitHub 的 arm runner（公开仓库免费） |
| 入口与 HTTPS | 在现有 Nginx 前面加一层 **Caddy**：Caddy 监听 80 和 443，自动申请证书，HTTP 跳转到 HTTPS，再转发给 web（Nginx）。Nginx 的配置和本地完全一样。生产环境额外挂载一个 realip 配置，让 Nginx 相信 Caddy 传来的 X-Forwarded-For，否则登录限流会把所有用户当成同一个 IP。证书保存在命名 volume 里，重新部署不会重新申请 |
| 生产 compose | 新文件 `deploy/compose.yaml`：服务包括 caddy、web、backend、ai、migrate，没有 postgres，镜像从 ECR 拉取。所有服务都设置 `restart: unless-stopped`，日志文件按大小轮转。ai 不挂载 `~/.aws`，直接使用实例角色 |
| 密钥 | JWT_SECRET、CSRF_SECRET 和数据库密码保存在 SSM Parameter Store，类型为 SecureString。由一个脚本随机生成并写入，**不经过 Terraform**。RDS 的主密码通过 write-only 参数（`password_wo`）传入，不会写进状态文件。部署时，EC2 读取这些参数，生成 `/opt/mogumogu/.env`（权限 600） |
| 生产配置 | `ALLOWED_ORIGINS=https://mogumogu.teiyou416.ninja`、`COOKIE_SECURE=true`、`ASSISTANT_SITE_DAILY_LIMIT=100`。每用户每日额度仍为 30 |
| EC2 的权限 | 只能调用日本 Geo 推理配置下的 Haiku 4.5，包括推理配置本身和它路由到的各区域基础模型；从三个 ECR 仓库拉镜像；读取 `/mogumogu/*` 参数；SSM 托管实例的基础权限；读取部署用 S3 桶 |
| 部署 | 部署文件（compose、Caddyfile、realip 配置、`deploy.sh`）上传到一个专用的 S3 桶。再通过 SSM Run Command 让 EC2 执行 `deploy.sh <提交号>`。`deploy.sh` 的步骤：生成 .env，登录 ECR，拉取镜像，执行迁移，启动服务，等待健康检查通过，最后清理旧镜像。**回滚就是用上一个提交号再部署一次** |
| Terraform | `infra/bootstrap/`：只创建状态桶（开启版本控制和加密），状态文件放本地，只运行一次。`infra/main/`：其余所有资源，状态放在 S3，使用 S3 原生锁。提交 `.terraform.lock.hcl` 和 `terraform.tfvars.example`；不提交 `.terraform/`、`*.tfstate` 和 `terraform.tfvars` |
| 自动部署 | push 到 master 后，CI 全部通过，再运行 deploy job。job 通过 OIDC 扮演一个部署角色，这个角色只信任本仓库的 master 分支，只能往三个 ECR 仓库推镜像、往部署桶写文件、对这台实例执行 Run Command。角色 ARN 保存在 GitHub Secrets。同一时间只运行一个部署 |
| 预算 | AWS Budgets 每月 40 美元：实际花费超过和预测花费超过时各发一封邮件 |
| 演示数据 | 脚本通过公开 API 注册演示账号，写入最近两周的行程和饮食记录，日期以运行当天为准，可以重复运行。演示账号的密码在运行脚本时传入，不进 Git |

**每月成本估算**（东京区域，按公开价格粗算，未核实，里程碑 1 用 AWS 定价确认后写进完成报告）：

| 项目 | 美元/月 |
|---|---|
| EC2 t4g.micro + 20 GB EBS | 约 10 |
| RDS db.t4g.micro + 20 GB | 约 20 |
| 公网 IPv4（Elastic IP） | 约 3.6 |
| ECR、S3、SSM、CloudWatch Logs | 不到 1 |
| Bedrock | 用量决定；全站额度用满时最多约 30，实际远低于此 |
| **合计** | **约 35，不含 Bedrock** |

## 实施里程碑

### 里程碑 1：Terraform 与云资源

- 先对照当前文档确认以下几点，再写代码：
  - Terraform 的 S3 原生锁（`use_lockfile`）怎么用。
  - `aws_db_instance` 的 `password_wo` 怎么用，以及怎样把 SSM 参数传给它而不写进状态文件。
  - 跨区域推理配置的 IAM 写法：需要授权哪些资源 ARN。
- `infra/bootstrap/`：状态桶。
- `infra/main/`：
  - VPC、子网、路由表、安全组。
  - RDS 和子网组。
  - EC2，包括实例角色、Elastic IP，以及用 user data 安装 Docker 和 compose 插件、添加 swap。
  - ECR 仓库和生命周期策略、部署桶、Budgets。输出 Elastic IP，用于在 name.com 添加 A 记录。
- `scripts/put-secrets.sh`：生成随机密钥写入 SSM。已经存在的参数不覆盖。
- 更新 `.gitignore`。
- 检查：
  - `terraform plan` 再执行一次时没有变更。
  - 能通过 Session Manager 登录 EC2。
  - 在 EC2 上用实例角色调用一次 Bedrock，并确认调用其他模型被拒绝。
  - 在 EC2 上能连接 RDS。
  - 状态文件里搜不到任何密钥。

### 里程碑 2：生产 compose 与手动部署

- `deploy/`：`compose.yaml`、`Caddyfile`、`nginx-realip.conf`、`deploy.sh`。
- Makefile：
  - `make push`：构建 arm64 镜像，打上当前提交号，推送到 ECR。工作区有未提交的改动时拒绝执行。
  - `make deploy [TAG=<提交号>]`：上传部署文件，再通过 SSM 执行 `deploy.sh`，并输出执行结果。
- 第一次部署：打开 `https://mogumogu.teiyou416.ninja`，完成注册、日历的增删改查和一次真实的 AI 对话。
- `scripts/seed-demo.sh`：写入演示账号和数据。
- docs/operations.md 新增「云上部署」一节：首次搭建、发布、回滚、登录服务器、查看日志、更换密钥。README 写上线上地址。

### 里程碑 3：GitHub Actions 自动部署

- Terraform 新增 GitHub 的 OIDC 提供商和部署角色。
- ci.yml 新增 deploy job：
  - 只在 push 到 master 时运行，并依赖其他所有 job。
  - 在 arm runner 上构建并推送三个镜像。
  - 上传部署文件，执行 `deploy.sh`，等待结果。
  - 设置 `concurrency`，同一时间只运行一个部署。
- 不打印账号 ID 和角色 ARN。
- 检查：
  - 一次 dev → master 的合并能自动发布到线上。
  - 用 `make deploy TAG=<上一个提交号>` 能回滚。

### 里程碑 4：日志与备份演练

- （已由 plans/observability.md 实现）容器日志通过 awslogs 驱动写到 CloudWatch Logs，保留 14 天，实例角色只能写这个日志组。在控制台能按服务查看日志，并确认日志里没有消息内容和密钥。
- RDS 恢复演练：
  - 用最近的自动快照恢复出一台临时实例。
  - 核对表和行数。
  - 删除临时实例。
  - 步骤写进 docs/operations.md。
- 这个里程碑可以放到 4b 之后再做。

## 验收标准（6 项）

每一项在完成报告中都要标明「实际执行」或「未验证」。

1. **线上可用**：
   - 浏览器打开 `https://mogumogu.teiyou416.ninja`，证书有效，HTTP 自动跳转到 HTTPS。
   - 注册、登录、日历的增删改查、Profile 都正常。
   - 饮食助手用真实的 Bedrock 返回 3 张推荐卡片。
   - 演示账号能登录并看到两周的数据。
2. **网络边界**：
   - 从外部只能访问 80 和 443，22 和 5432 都连不上。
   - RDS 没有公网地址。
   - 访问 `https://mogumogu.teiyou416.ninja/internal/...` 得不到内部接口。
   - Cookie 带有 Secure 标记。
   - 登录限流按真实的客户端 IP 计算。
3. **没有长期凭证**：
   - 账号里没有 IAM 用户的 Access Key。
   - EC2 用实例角色，CI 用 OIDC。
   - 密钥只在 SSM 和服务器的 `.env` 里，Git、镜像、Terraform 状态文件和 CI 日志中都没有。
   - 实例角色调用其他 Bedrock 模型时被拒绝。
4. **发布与回滚**：
   - 合并到 master 后自动发布，线上运行的镜像标签等于新的提交号。
   - 用上一个提交号能回滚。
   - 重新部署和重启 EC2 后，数据和证书都还在。
5. **成本控制**：
   - Budgets 的两个告警已生效。
   - 全站每日额度是 100。
   - 没有 NAT 网关和 ALB。
   - 完成报告写明按实际资源核算的每月费用。
6. **原有流程不受影响**：本地的 `make test`、`make test-integration`、`make test-e2e`、`make app-up` 都能通过，CI 全部通过。

## 完成报告

报告以下内容：
- 创建了哪些资源，以及每月费用的核算。
- 部署和回滚的步骤。
- 6 项验收的结果，每项标明「实际执行」还是「未验证」。
- 线上一次 AI 调用的耗时。
- 剩余问题。
