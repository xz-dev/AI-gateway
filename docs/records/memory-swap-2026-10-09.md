# 生产容器内存与 swap 调整 — 2026-10-09

## 已执行

用户批准所有 Compose 受管容器恢复 Docker 默认有界 swap，并放宽两个曾 OOM 的容器内存上限。操作通过任务专用 Ansible playbook 完成，复用仓库 `ai_ops` 的 `preflight`、`prepare-file`、`deploy-file`；在线 `docker update` 同样由 Ansible 执行。

- 从生产配置、仓库模板及 provider-sidecar 生成模板中删除 `memswap_limit == mem_limit` 的设置。这种等额配置实际禁止 swap。
- 未指定 `memswap_limit` 时，Docker 默认允许额外使用最多与 `mem_limit` 等量的 swap；不是无限 swap，也不是预留内存。
- cloudflared：RAM 上限由 48 MiB 调至 **96 MiB**，额外 swap 上限 **96 MiB**。
- ai-sse-keepalive-proxy：RAM 上限由 64 MiB 调至 **128 MiB**，额外 swap 上限 **128 MiB**。
- CPA：RAM 上限保持 **1024 MiB**，额外 swap 上限 **1024 MiB**。
- AISIX、egress-proxy 原有 RAM 192 MiB＋额外 swap 64 MiB 保持不变。
- 在线更新 46 个容器。48 个 Compose 受管容器均允许 swap；未修改非 Compose 的 `tailnet-apisix-relay`。
- 没有拉取或切换镜像、重建或重启容器、修改 `.env`、部署 GOMEMLIMIT、调整 PostgreSQL 参数或修改内核设置。

配置来源仍是 `ansible/private-config/{compose.yaml,compose.override.yaml}`。生产同名文件与 `.ai-ops-state/*.baseline` 的 SHA-256 已核对一致。其他会话暂存的 `.env` 镜像版本候选没有下发。

## 现场证据

调整前不仅发生容器 OOM，还存在持续回收与缺页抖动：

- `kswapd0` 是短时采样中 CPU 用量最高的进程，约半个 CPU 核。
- `containerd`、`sub2api` 线程现场栈包含 `filemap_fault → folio_wait_bit_common`；`tailscaled` 包含 `do_swap_page → zram → zcomp_stream_get`。
- 46 个容器禁用 swap，约 931 MiB 匿名内存不能换出。宿主的 dockerd、containerd、tailscaled 则有大量内存已换出。
- cloudflared 在约 6 秒内发生 84 次 `memory.events.local` 的 `max` 事件，但该次采样没有新增 OOM kill。触限次数不是 OOM kill 次数。

2026-10-09 08:08 UTC，应用后的三个连续 5 秒被动采样：

| 指标 | 调整前短时采样 | 调整后短时采样 |
|---|---:|---:|
| 系统盘读入 | 约 526 MiB/s | 1.5–7.6 MiB/s |
| CPU 空闲 | 接近 0% | 84.1%–92.6% |
| CPU steal | 58.5%–60.7% | 0%–0.2% |
| 文件页 refault | 约 96,000 页/秒 | 140–1,346 页/秒 |
| 可用内存 | 约 326 MiB | 约 757 MiB |

调整后 CPA 已实际使用约 292 MiB swap。磁盘交换文件也开始被使用；允许 swap 不代表保证所有交换都留在 zram。以上是不同时间的现场观测，不是同负载受控基准，但结果强烈支持禁 swap 策略是抖动的重要因素。不能据此认定某个 Ubuntu 内核版本有缺陷，也不能将此前高 steal 直接归因为云厂商超卖。

## 验证与边界

- Ansible plan：`changed=0, unreachable=0, failed=0`。
- Ansible apply：`changed=7, unreachable=0, failed=0`。
- 实际 cgroup RAM/swap 上限与计划一致。
- Docker API 独立复核：48/48 运行，11/11 已配置的健康检查为 healthy；容器 ID、启动时间和镜像全部保持不变。
- 本机 CPA `/healthz`、Sub2API `/health` 均返回 HTTP 200。
- 公网未认证 `/v1/models` 探测返回 404，不是业务验收，也不能据此宣告推理请求成功。
- 调整后复核时，本次启动累计 OOM kill 仍为 3；不能据短期观察承诺今后不再 OOM。
- `git diff --check`、生成脚本语法检查、`scripts/test-provider-sidecar-compose-candidate.sh` 通过；四份 Compose 的 YAML 语义差异检查确认仅改变批准的资源策略，保留已有组件版本修改。

## 备份与后续部署

生产备份、计划及执行回执位于：

```text
/root/AI-gateway/.ai-ops-state/memory-swap-20261009/
```

其中 `before/` 保存操作前配置、Ansible baseline、容器身份和限额；`ansible-plan.json`、`completed.json`、`ansible-applied.json` 记录本次在线更新。

后续继续使用常规 Ansible 部署流程，不能把旧的禁 swap Compose 覆盖回来。在线更新未重建容器，Compose 的创建期配置哈希不会同步改变，因此后续常规 `compose up` 可能因资源配置变化而重建，需按日常部署流程安排。

不要在容器已经使用 swap 后盲目恢复旧的零 swap 限额；回退需要先检查实际用量及容量，不能直接强制缩小。此次没有实施回退或内核对照试验。
