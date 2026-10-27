# 生产组件更新 — 2026-10-11

> 历史回执：下述版本、备份和验证结果均截至本轮收尾。后续版本变更及线上备份、旧镜像清理见 [2026-10-15 回执](component-upgrade-2026-10-15.md)；本文列出的生产备份和回滚引用现已失效。

## 实际结果

本轮完成四类更新，涉及 **23 个容器**：三个单服务和 20 个 socat 中继。收尾核实 **48/48 受管容器 running，11/11 配置了健康检查的容器 healthy**。其余 25 个容器身份未变。

| 组件 | 更新前 | 更新后 |
| --- | --- | --- |
| model-catalog-sidecar | nginx-unprivileged 1.31.6，旧构建 | 1.31.6，同版本新构建 |
| postgres | PostgreSQL 18.6，旧构建 | 18.6，同版本新构建 |
| 20 个 TCP 中继 | socat 1.8.1.3，旧构建 | 1.8.1.3，同版本新构建 |
| provider-sidecar | zcode-proxy 4.7.6 | 4.8.0，预发布版 |

zcode 镜像 OCI 标签指向的源码仓库是 `TriDefender/zcode-api`，不是同名的 `zcode-proxy` 仓库。[v4.8.0](https://github.com/TriDefender/zcode-api/releases/tag/v4.8.0) 被 GitHub 标为预发布版；用户在得知这一新证据后单独确认了升级。

生产 `.env` 的有效值只变更四个镜像字段：`MODEL_CATALOG_SIDECAR_IMAGE`、`POSTGRES_IMAGE`、`SOCAT_IMAGE`、`PROVIDER_SIDECAR_IMAGE`。没有改账号、密钥或模型路由。两份生产 Compose 文件保持原 SHA256；本地私有配置与生产 `.env`、两份 Compose、CPA 配置的 SHA256 全部匹配。

已部署引用：

```text
MODEL_CATALOG_SIDECAR_IMAGE=docker.io/nginxinc/nginx-unprivileged:1.31.6-alpine@sha256:b9241c6e7b8e9a862f129d8d4199ab64b10390949a78bdd5603379b32c844083
POSTGRES_IMAGE=docker.io/library/postgres:18.6-alpine3.24@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873
SOCAT_IMAGE=docker.io/alpine/socat:1.8.1.3@sha256:fb3862c949af7c79996d0e514ee96128644d840835ed5368eeecb14a869de361
PROVIDER_SIDECAR_IMAGE=ghcr.io/tridefender/zcode-proxy:4.8.0@sha256:5f267e4f6b09dca4600e3f244b6c4486eac398a421ce7c56ca7af1299b510101
```

## 验证

- nginx `nginx -t` 通过，容器内 `/models-table` 探测成功。
- PostgreSQL `pg_isready` 接受连接，实际二进制仍为 18.6；Sub2API 容器内及主机 `127.0.0.1:8086/health` 均成功。
- socat 专用 Ansible playbook 逐个重建 20 个中继，每次核对实际镜像及 running 状态；该批操作没有改变任何非中继容器身份。
- zcode 先在无网络容器中使用私有数据副本验证启动与 `/health=200`，再仅重建 provider-sidecar。生产健康检查通过，显式认证 `/health=200`，restart_count=0；tun2proxy 容器身份和版本未变。
- 收尾 CPA 认证 `/v1/models=200`，模型数 323；Sub2API `/health=200`。
- 没有残留本轮临时验证容器。

这些检查不等于已认证公网推理、SSE/WS 全协议回归、长期稳定性或数据库恢复演练；本轮没有进行这些验证。

## 未升级的两项候选

### CPA 8.0.23

[上游 v8.0.23](https://github.com/router-for-me/CLIProxyAPI/releases/tag/v8.0.23) 有新发布，PR6189 仍未合入。本地在下载的精确上游源码快照上复用了当前生产补丁，没有更改 CLIProxyAPI 工作分支，也没有提交或推送。

候选的 `sdk/api/handlers/openai` 包有两项测试失败：

- `TestResponsesInterruptStopsHTTPUpstream`：`http upstream was not canceled`。
- `TestResponsesLateInterruptDoesNotPoisonNextCreate`：预期 error，却收到 `response.incomplete`。

同一 Go 1.26 工具链上，无 PR6189 补丁的官方 8.0.23 源码也复现这两项失败。这个对照说明不能把失败归因于生产补丁，但没有证明是测试陈旧还是运行行为缺陷。因此没有绕过测试部署。

生产保留 `v8.0.22-pr6189-c718f4d`；CPA 容器身份和既有 restart_count=2 均未变。

### quota 插件 0.19.6

[上游 v0.19.6](https://github.com/Autsunset/cpa-quota-estimator/releases/tag/v0.19.6) 会修复图像额度池和历史统计。官方 Linux amd64 ZIP 的发布 checksum 匹配。

早期验证夹具读取了错误的版本字段，并复制了旧 `store.version=0.19.5`。内核会用这个 pin 过滤不匹配的库；这些早期失败不能作为新版不兼容的证据。

修正为 `metadata.version`、同步 `store.version/release-tag` 到 `0.19.6/v0.19.6`、显式使用数据库副本，并改为生产目录布局后，有界隔离验收仍未达到注册门：返回空 path、`registered=false`，副本中也没有 `image_quota_scopes_v1=complete`。真实兼容性原因尚未判定，下一步需要旧版同夹具的阳性对照，而不是继续盲重试或直接切生产。

生产保持 **0.19.5 / registered=true**。没有把新库放入生产插件目录，没有修改生产 CPA 配置，也没有触发 0.19.6 的生产历史修复；收尾 live repair marker 不存在。授权过迁移不代表迁移已经执行。

## 备份与恢复边界

生产保护目录：

```text
/root/AI-gateway/.ai-ops-state/update-20261011/
```

保留：

- `sub2api.pgdump`：在线一致性 custom-format 快照，219474899 字节，约 209.3 MiB；TOC 可读，收尾 SHA256 校验通过。
- `quota.sqlite`：SQLite online backup，551477248 字节，约 525.9 MiB；`integrity_check=ok`，备份时 usage_events 为 1257630 条，收尾 SHA256 校验通过。
- `private-config.tar`：私有配置、旧插件和 sidecar 状态资料，未附入公共文档。
- `baseline.json`、`socat-receipt.json`、候选验证证据及标准组件部署回执。
- `rollback-20261011/<service>:before`：操作前运行镜像的本地保留标签。

没有执行数据库恢复、删除旧镜像或 prune。备份与旧镜像存在不等于已验证无条件回退；恢复业务数据仍需单独评估和授权。

## 执行与检查边界

生产写入通过 Ansible；单服务复用 `ops.yml`，中继采用任务专用的逐服务 playbook，没有整栈 down/up。PostgreSQL apply 上传的共享 `.env` 同时包含 PostgreSQL 与 socat 的新 pin，但当时仅重建 PostgreSQL；socat 后续才滚动切换。因此不能声称每次共享文件 apply 都只有单一字段差异。

核查时 Sub2API、APISIX、AISIX、cloudflared、tun2proxy、keepalive 没有新稳定版需要切换；Squid/Unbound 的发行版版本也与 pin 一致。这不是对所有嵌套依赖或后续发布的“全部最新”保证。

本轮没有 Git 提交、推送或新分支操作，没有主机系统升级，没有借维护之机重启 CPA 修复先前记录的配置监听限制。任务日志和源码快照位于本地 `/var/tmp/ai-gateway-update-20261011/`，凭据不纳入公开回执。
