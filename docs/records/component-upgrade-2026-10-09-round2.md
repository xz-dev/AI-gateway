# 生产组件升级：第二轮 — 2026-10-09

本轮按用户新指令更新所有已核实有新发布候选的组件。此前升级、530 排查和清理结案保留于 [第一轮记录](component-upgrade-2026-10-09.md)，不改写其历史结论。

生产目标由私有 Ansible inventory 定义，项目目录为 `/root/AI-gateway`。生产写入均通过 Ansible；没有执行整栈 down/up、数据库备份恢复或 CPA 同版本重建。

## 实际切换

| 组件 | 切换前 | 切换后 | 容器启动时间（UTC） |
| --- | --- | --- | --- |
| cloudflared | 2026.9.3 | 2026.10.0 | 2026-10-09 08:27:20 |
| provider-sidecar-tunnel | tun2proxy v0.8.3 | v0.8.4 | 2026-10-09 08:45:37 |
| sub2api | 0.2.14-pr7157-pr7217-pr3172-d00e464 | 0.2.15-pr7157-pr7217-pr3172-2ddb824 | 2026-10-09 09:08:28 |

provider-sidecar 镜像仍为 zcode-proxy 4.7.6；因共享 tunnel 的网络命名空间，联动重建于 08:45:43 UTC。验证二者 healthy、实际 netns 相同、sidecar 镜像未变，其他容器身份和启动时间未变。

已部署完整引用：

```text
CLOUDFLARED_IMAGE=docker.io/cloudflare/cloudflared:2026.10.0@sha256:9b49eed8f62806d5d45ddf59ecefb5710429598ea6d3fcccd2af938f621b2b07
TUN2PROXY_IMAGE=ghcr.io/tun2proxy/tun2proxy:v0.8.4@sha256:0469219b2f65aa2c80687a783fd788e294e6053c636a71755d5e0c8282860968
SUB2API_IMAGE=ghcr.io/xz-dev/sub2api:0.2.15-pr7157-pr7217-pr3172-2ddb824@sha256:00038989050e7f85c47accb3626fa12301635f65615a1ea5cc223ea331ebb8c6
```

## 未切换的组件

- CPA 仍为 v8.0.22-pr6189-c718f4d；查询时上游最新发布为 v8.0.22。
- keepalive 生产已为 `bd38e50f9fc09e1c446d3573b31cd54ecec583df`。最初将本地 checkout 落后误报为生产可升级，已纠正；本地落后并不代表生产落后。GHCR 中 `da625d8` 的镜像创建于 2026-08-27，是旧候选，没有降级或重建生产。
- AISIX 最新发布查询为 v1.5.0，与生产一致；其他既有服务保持原 pin，没有切到 dev/latest 浮动标签。
- Debian trixie 包页核实 Squid 为 `6.13-2+deb13u3`、Unbound 为 `1.26.1-0+deb13u1`，与 Dockerfile pin 一致，未重建 egress-proxy。

这些核查不代表对任意未来发布、未发布源码或所有内嵌插件作出“永远最新”的保证。

## Sub2API fork 与 CI

上游 v0.2.15 基线：`f2669c8cf62555cd92389b3f55920e9e6e7c6ff2`。

保留 PR7157 rerank（6 个提交）、PR7217 session_id 亲和（1 个）、PR3172 WS keepalive（2 个），共 9 个源码提交，重放时没有文本冲突。range-diff 的主要差异为新增 cherry-pick 来源记录，以及 WS Usage 字段的上游上下文变化；不能以无文本冲突替代行为测试。

- 源码提交：`2ddb824eecde383d3ddb26c70a295d622340b873`。
- CI overlay：`e05bd9027e2455253d6a21c41e4938b469ff6ea6`。
- 临时分支：`ci/pr7157-pr7217-pr3172-0.2.15-image`，推送到 `xz-dev/sub2api`；没有更新其主分支。
- 构建器使用 Go 1.27.2，满足新 go.mod 要求。
- 本地 `go build ./...`、handler/service/openai_ws_v2 测试，以及 migrations/repository 迁移相关测试均通过。
- [镜像构建 37905969990](https://github.com/xz-dev/sub2api/actions/runs/37905969990)、[完整 CI 37905969751](https://github.com/xz-dev/sub2api/actions/runs/37905969751)、[Security Scan 37905969693](https://github.com/xz-dev/sub2api/actions/runs/37905969693) 全部 success。完整 CI 包含 test、golangci-lint、frontend、shell 和 release-helpers。
- Registry OCI revision/source/version 和生产二进制版本均核对到上述源码提交。

**意外标签推送：** 本地既有 `push.followTags=true` 使分支推送附带上游 `v0.2.15` 标签，触发未经计划的原版 Release。已取消 [Release 37905968367](https://github.com/xz-dev/sub2api/actions/runs/37905968367)，最终为 cancelled，release 与 sync-version-file job 未完成。标签没有删除。后续显式分支推送应使用 `git -c push.followTags=false push ...`，避免夹带标签。

## 数据库备份与迁移

用户另行明确确认执行生产迁移 242。切换前：

- 生产数据库大小约 3821 MB。
- `pg_dump --format=custom --compress=1` 在线一致性快照保存在 `/root/AI-gateway/.ai-ops-state/sub2api-0215-20261009/sub2api.pgdump`，大小 **215716588 字节**，约 205.7 MiB。
- 同目录保留 `sub2api.toc`、`sub2api.sha256`、`schema.pgdump` 和 `receipt.txt`。TOC 可读，收尾 SHA256 校验通过；这些不等于完整数据恢复演练。
- 291 条已应用迁移的 checksum 与候选源码全部一致，唯一待执行文件为 `242_drop_platform_check_constraints.sql`。
- 仅将 schema 快照用于本地无网络临时 PostgreSQL 容器；没有复制生产行数据。schema 恢复后执行迁移成功：目标约束数 2 → 0，public 列数保持 1424。临时容器已清理。

迁移 242 删除 `user_platform_quotas_platform_check` 和 `composite_model_routes_target_platform_check`，平台校验改由应用清单承担；不删除表、列或业务行数据。生产切换后核实迁移记录为 1 条、目标约束余数为 0。

**回退限制：** 旧镜像与备份可用不代表数据库可无条件回退。未做完整数据恢复或真实旧应用回退演练；不能自动恢复旧备份而丢失升级后的写入。实际回退应先核对新平台数据、迁移状态及旧应用兼容性，并另获数据库恢复授权。

## Ansible 与恢复经过

- cloudflared：标准 `ops.yml --tags deploy -e service=cloudflared`。一次 apply 和随后的 rollback 在 120 秒期限内超时；后续只读核实仍为旧镜像和旧 pin，没有据超时宣告已切换或已恢复。采用更长有界执行后，apply `ok=50 changed=5 failed=0`，并核实实际新版本。
- tun2proxy：任务专用 playbook 复用 ai_ops preflight/deploy-file 漂移门、快照和 rescue；唯一有效配置差异为 TUN2PROXY_IMAGE。联动 `up --force-recreate --no-deps --no-build provider-sidecar-tunnel provider-sidecar`，plan `ok=22 changed=0 failed=0`，apply `ok=35 changed=5 failed=0`。
- Sub2API：标准单服务 deploy；唯一有效配置差异为 SUB2API_IMAGE，plan `ok=34 changed=0 failed=0`，apply `ok=50 changed=6 failed=0`。部署后比较所有运行容器，仅 Sub2API 身份/启动时间变化。

生产回执：

```text
/root/AI-gateway/.ai-ops-state/receipt-cloudflared-1791534453.txt
/root/AI-gateway/.ai-ops-state/receipt-tun2proxy-1791535581.txt
/root/AI-gateway/.ai-ops-state/receipt-sub2api-1791536920.txt
```

本轮开始时重新从运行镜像打标签；收尾核实以下旧镜像均仍在本地：

```text
rollback-20261009/cloudflared:2026.9.3
rollback-20261009/tun2proxy:v0.8.3
rollback-20261009/keepalive:bd38e50f
rollback-20261009/sub2api:0.2.14-d00e464
```

## 收尾验证与边界

- **48/48 受管容器 running，11/11 配置了健康检查的容器 healthy**，没有 starting/unhealthy。
- cloudflared 四条隧道注册；新 cloudflared、tunnel、sidecar、Sub2API 的 restart_count 均为 0。
- CPA 认证 `/v1/models` 返回 200，模型数 312；Sub2API 本机 `/health` 返回 200。
- 公网无认证 `/v1/models` 返回 404，沿用 APISIX 隐藏未授权响应的策略。这不是公网认证请求或推理验收。
- 对五个相关服务最近 20 分钟、最多 250 行日志作有限关键错误筛选，匹配数均为 0；不声称日志完全无错误。
- 本地 private-config 的 `.env`、两份 Compose 与生产文件 SHA256 一致。

**未执行：** 公网 Sub2API 真实推理、WS/session/rerank 全协议回归、完整数据库恢复演练或长期稳定性验证。第一轮记录的 CPA 配置热监听缺失也没有在本轮修复；没有夹带同版本 CPA 重建。

## 仓库与资源协调

另一会话独立获批并通过 Ansible 在线调整资源，未重启容器，详见 [资源记录](memory-swap-2026-10-09.md)。本轮保留其策略：cloudflared RAM 96 MiB / RAM+swap 192 MiB，keepalive 128/256 MiB，Sub2API 320/640 MiB。

资源模板提交 `4b142a3c0ad087b83f6d95aba2d6168b490d63ee` 已由另一会话签名推送；本轮没有夹带升级 pins。

`.env.example`、`compose.yaml` 同步本轮已部署的 cloudflared、tun2proxy、Sub2API 引用；CPA 的第一轮引用同步也保留在未提交 diff 中。keepalive 公共模板带 `build`/`pull_policy: build`，使用本地源码构建标签，保留原状，不将生产 registry digest 填入 build 标签；本地子模块仍落后于既有生产提交。本轮未更新该子模块，也未重建 keepalive。历史记录不改写为新版本，具体 sidecar 运行配置仍留在忽略跟踪的私有文件。

用户明确项目不需要 GPG 后，AI-gateway 与 Sub2API 的仓库级 `commit.gpgsign=false`，全局设置仍为 true。部署阶段仅获批 Sub2API 临时 CI 分支提交推送；随后用户明确要求 push，授权发布 AI-gateway 公开镜像 pins 与三份运维记录。实际提交与推送结果以 Git 历史和 `origin/main` 为准；私有配置、备份、凭据及工具缓存不纳入提交。
