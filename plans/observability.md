# 日志接入 CloudWatch Logs

> 状态：**Terraform 已 apply，待部署**（2026-10-30）。分支 `feat/observability`，从 dev 切出。

这是 `plans/phase-5.md` 里程碑 4 的日志部分（RDS 恢复演练不在本计划内），另外加了一个 5xx 告警。phase-5 原来把「自定义告警和仪表盘」列在范围外，本计划只加这一个告警，仪表盘仍然不做。

## 背景

nginx 已经把每个请求写成一行 JSON（时间、客户端 IP、方法、路径（不含查询参数）、状态码、耗时），Go 后端也用 slog 输出 JSON 请求日志。但这些日志只在 EC2 上的 Docker 里：要登录服务器才能看，重新部署后就没了，也无法统计和告警。

## 已确认的决定

- 用 Docker 自带的 **awslogs 日志驱动**把所有容器（migrate、backend、ai、web、caddy）的输出写到 CloudWatch Logs，服务器上不装 CloudWatch Agent。
- 一个日志组 `/mogumogu/app`，由 Terraform 创建，**保留 14 天**（日志里有客户端 IP）。容器不自动建组（`awslogs-create-group` 不开）。
- 每个容器一个日志流，名字为「容器名/容器 ID」，例如 `mogumogu-web-1/3f2a…`，在 Logs Insights 里可以按服务筛选。
- 实例角色只能在这个日志组里建流和写入（`logs:CreateLogStream`、`logs:PutLogEvents`），不能读、不能删。
- 日志驱动用 **non-blocking** 模式（缓冲 4 MB）：CloudWatch 暂时不可用时最多丢一些日志，而不是让应用卡住。
- 服务器上的 `docker compose logs` 继续可用（Docker 的 dual logging 会在本地缓存一份）。
- 本地开发的 `compose.yaml` 不变，仍然只输出到终端。
- **5xx 告警**：
  - 一个 metric filter，只统计 nginx 的访问日志（有 `remote_addr` 字段的那一行），状态码 ≥ 500 时计数；后端的请求日志不计入，避免同一个请求算两次。
  - 一个 CloudWatch 告警：5 分钟内 5xx ≥ 1 次时，通过 SNS 发邮件到预算告警用的邮箱。
  - 建好后 AWS 会发一封订阅确认邮件，需要点一次链接。
  - 局限：服务器整台宕机时没有日志，这个告警不会触发。外部的存活检查不在本计划内。
- 费用：日志每月预计几十 MB，告警每个每月 0.1 美元，SNS 邮件免费额度内。合计每月不到 1 美元。

## 里程碑 1：Terraform

- 新增 `infra/main/logs.tf`：
  - `aws_cloudwatch_log_group`：`/mogumogu/app`，`retention_in_days = 14`；
  - metric filter、告警、SNS topic 和邮件订阅。
- `iam.tf`：实例角色的策略加一条 `Logs`，资源限定为这个日志组。
- 检查：`terraform fmt`、`terraform validate`（我执行）；`terraform plan` 和 `apply`（你执行，plan 应该只有新增，没有修改或删除其他资源）。

## 里程碑 2：部署配置

- `deploy/compose.yaml`：用一个 `x-logging` 锚点给 5 个服务加上 awslogs 配置（日志组、区域、流名、non-blocking）。
- **顺序很重要**：必须先 `terraform apply`（日志组和权限都已存在），再部署。否则容器会因为写不了日志而启动失败。
- 检查：
  - 本地 `docker compose -f deploy/compose.yaml config` 能通过；
  - 部署后，CloudWatch 中 5 个服务都有日志流，`aws logs tail /mogumogu/app --follow` 能看到实时请求；
  - 日志中没有聊天消息内容、Cookie、令牌或密钥；
  - 服务器上的 `docker compose logs` 仍然可用。

## 里程碑 3：文档

- `docs/operations.md`：
  - 6.6「查看日志」：改为优先在 CloudWatch 查看，同时保留登录服务器查看的方法；
  - 新增常用的 Logs Insights 查询：每小时请求数、每天独立 IP 数、访问最多的路径、5xx 明细、慢请求、饮食助手的调用次数和失败次数；
  - 5xx 告警收到邮件后怎么排查。
- `plans/phase-5.md`：里程碑 4 的日志部分标为完成，并在「不在范围内」里注明 5xx 告警已在本计划中添加。

## 验证

- `make test` 通过（应用代码不变，主要确认 CI 不受影响）。
- 告警可以用 `aws cloudwatch set-alarm-state` 临时设为 ALARM 状态，确认能收到邮件（你执行）。
- 每一项都会在完成报告里标明「实际执行」或「未验证」。

## 不在范围内

- 仪表盘、网站访问分析（Plausible 等）、外部的存活检查；
- 修改应用的日志内容或格式；
- RDS 恢复演练（phase-5 里程碑 4 的另一半）。
