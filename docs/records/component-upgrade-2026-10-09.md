# 生产组件升级与 530 排查 — 2026-10-09

## 结果与边界

本次更新了 provider-sidecar、cli-proxy-api 和 sub2api 三个组件。收尾时只读核对了运行容器的完整镜像引用与 digest，三个容器均为 `running / healthy`。用户已用原来出现 530 的客户端重试，并确认真实请求恢复正常。

升级后曾发生公网 HTTP 530（Cloudflare 1033）。CPA 的高读取量尚未归因，不能宣告长期稳定，也不能断言清理备份解决了故障或新版本就是故障原因。容器健康检查与未授权请求响应不等于端到端验收。

其他组件本次未更新。早期盘点没有逐项完成权威发布与兼容性核验，撤回“其余组件均为最新”的结论。

用户最终指示停止后续处理、清理备份和无用文件，并将本次升级结案。资源异常以及后续试验暴露的配置监听缺失作为已知限制保留，不再自动排查、重试或重启。

## 已部署镜像

| 组件 | 升级前版本 | 本次版本 |
|---|---|---|
| provider-sidecar | zcode-proxy 4.7.2 | 4.7.6 |
| cli-proxy-api | v8.0.4-pr6189-8f82146 | v8.0.22-pr6189-c718f4d |
| sub2api | 0.2.10-pr7157-pr7217-pr3172-9a21b93 | 0.2.14-pr7157-pr7217-pr3172-d00e464 |

运行容器核验使用的完整引用：

```text
PROVIDER_SIDECAR_IMAGE=ghcr.io/tridefender/zcode-proxy:4.7.6@sha256:b6c4e70b0222eeb39ef1dc6ee910b89933de923e345072c1f6f29d4b1ecea6f9
CLI_PROXY_IMAGE=ghcr.io/xz-dev/cli-proxy-api:v8.0.22-pr6189-c718f4d@sha256:e02efefde0e7272d0f2655b29ee4ae2316badf66d08de1ef9b50e2223eb2caf5
SUB2API_IMAGE=ghcr.io/xz-dev/sub2api:0.2.14-pr7157-pr7217-pr3172-d00e464@sha256:4d88d14eee6bcf0674d34a3181b420f3d379e04bfb05562ea0e822e061ab0b89
```

## Fork 构建证据

### cli-proxy-api

- 源码分支：`candidate/v8.0.22-pr6189`，源码提交 `c718f4d3`；CI 分支：`ci-pr6189-v8.0.22-image`。
- 在上游 v8.0.22 上保留 PR#6189 的 prefixed xAI image model 路由补丁。构建子任务报告该 PR 当时仍未合入，补丁应用无冲突。
- 子任务报告 `go build ./...`、`go test -count=1 ./sdk/api/handlers/... ./internal/runtime/executor` 及 XAIImages 定向测试通过；本次文档收尾未重跑这些测试。
- 独立查询确认 [Actions run 37873385985](https://github.com/xz-dev/CLIProxyAPI/actions/runs/37873385985) 为 `completed / success`，CI 提交 `771a6823a0ff2039051ca78343d82f59cc10f1a9`。CI 提交包含构建工作流，与镜像标签中的源码提交不同。

### sub2api

- 源码提交：`d00e4648a6`；CI 分支：`ci/pr7157-pr7217-pr3172-0.2.14-image`。
- 本地 Git 核对显示相对 v0.2.14 共保留 **9 个源码提交**：rerank 功能及适配（PR7157，6 个）、session_id 亲和传播（PR7217，1 个）、WS 下游 keepalive（PR3172，2 个）。
- 子任务报告 PR7217 与上游新增的 `reserveInflightBalance` 代码冲突，处理方式为保留上游预留块，并应用 `resolveOpenAISessionInputs(...)`。
- 子任务报告 `go build ./...` 和 `go test ./internal/handler/... ./internal/service/...` 通过；本地前端构建未单独执行，Docker CI 构建成功不替代真实 WS/session 亲和行为验收。
- 独立查询确认 [Actions run 37873745251](https://github.com/xz-dev/sub2api/actions/runs/37873745251) 为 `completed / success`，CI 提交 `91cec6433f95daa080f52063aaf237073f123ab7`。

## 部署与仓库同步

使用 Ansible 的 `--tags deploy -e service=<name>` 逐服务拉取并重建，三次 apply 的 recap 均为 `failed=0`。部署工具将 receipt 写入远端 `.ai-ops-state/`。

记录中的 plan 包括 provider-sidecar 和 cli-proxy-api。provider-sidecar 的有效变更为镜像 pin；**CPA 的 plan 同时包含 CPA、Sub2API 两个镜像 pin 及注释变更**。共享 `.env` 先被同步，随后分别重建两个服务；不能将这一过程描述成每次 plan 都只有单一服务差异，也不声称做过独立的 Sub2API plan。

- [`compose.yaml`](../../compose.yaml) 和 [`.env.example`](../../.env.example) 的 CPA、Sub2API 默认引用同步到上述 digest。
- provider-sidecar 的具体运行 pin 仍保存在忽略跟踪的私有 `.env`，不新增公共模板默认值；本记录保留其部署版本作为运维证据。
- README 没有本次两个 fork 的旧版本字面引用；历史部署记录和 OpenSpec 归档不改写为新版本。
- 未进行 Git 提交或推送。原始 `.env` diff 可能包含凭据，不作为公开回执附入本文。

## 530 经过与验证

以下事件时间为北京时间（UTC+8），来自当次只读观测：

1. 初步检查观察到 Compose 的 48 个服务容器为 running；不是每个容器都配置了 healthcheck。Sub2API 本地 `/health` 返回 200，未授权 `/v1/models` 返回 401。这些检查不足以验收公网。
2. 用户报告 530。Tunnel 日志记录 QUIC 握手超时和 DNS 超时，约 11:12 最后一条连接断开，11:14:54 四条连接重新注册。
3. 约 11:34 再次连续三次复现公网 **HTTP 530 / Cloudflare 1033**，同期 Tunnel 有断连日志。2 核、约 2 GiB 内存主机的负载约 20，可用内存约 225 MiB；这是相关性证据，不是完整因果证明。
4. 经用户批准完成下述清理；未执行回滚、服务重启或配置修改。随后无凭据公网请求返回空白 404。
5. 核实生产 APISIX 会把 401 改写为空白 404，因此该探测不能证明已认证请求正常。
6. 用户使用原故障客户端重试，明确反馈“请求已恢复正常”。这是本次业务恢复的验证依据，而不是从 404 推断恢复。

撤回早期“启动日志无 error/fatal/panic”的全面结论：当时仅做了有限日志筛选，不能证明没有错误。用户确认恢复也不覆盖全部模型、协议或长期运行条件。

## 经批准的清理

仅执行以下清理，未清理镜像或活跃数据：

- 删除 `/root/AI-gateway/data/cpa/runtime-forensics-20260928/rec.sqlite` 和同目录的 `candidate.sqlite`，合计约 520 MiB。
- `journalctl --vacuum-size=300M`，工具报告释放归档 journal 580.4 MiB；清理后 journal 总占用约 258 MiB。

合计释放约 **1.08 GiB**，根文件系统可用空间由约 9.5 GiB 增至约 11 GiB。保留 `orig.sqlite`、损坏库副本、活跃 SQLite/Postgres 数据、部署备份及升级前镜像。删除前的进程文件描述符检查未发现两个中间文件被打开。

清理前负载已经下降，清理后仍观察到换页和高读取量，不能把请求恢复归因于删除备份。

## 未解决的资源异常

已观察到：

- CPA 进程的短时块设备读取采样约 100 MB/s；进程 syscall 采样捕获到对活跃 `cpa-quota-estimator.sqlite` 的 `pread64`。
- 活跃库约 398 MiB，历史回填标记为 `complete`。一轮采样中最近 100 条用量记录跨约 4 分钟。
- 对照 quota 插件 v0.16.0 上游源码，`insertEvent` 会调用 `refreshSegmentsForEvent`，在满足条件时重建额度区间。
- 对该路径 SQL 执行只读 `EXPLAIN QUERY PLAN`，观察到按账户/额度类型的索引取历史记录，再过滤周期并使用临时排序。采样账户约 19.3 万条历史记录，当前周期约 2 万条；存在读取放大的性能疑点。

尚未证明：具体哪条查询贡献了主要 I/O；该现象是否由新 CPA 引入；资源压力是否直接导致 Tunnel 断连。注册用量插件的适配路径在 v8.0.4 到 v8.0.22 间未变，新核心存在流式交付记账改动，但没有证据证明重复投递。

生产未配置 pprof，核心默认关闭。只读阶段未启用 profiling、附加进程跟踪、创建索引、停用插件或回滚。该阶段结束后，用户另行批准了一次限定范围的单插件隔离试验，结果见下节。

WS keepalive、session 亲和、rerank 和 xAI 图像路径没有全部逐项重做生产回归，仍需独立验证。

## 后续隔离试验与最终结案

用户另行批准：1 分钟基线、停用 quota 插件 5 分钟、恢复后观察 1 分钟；不授权服务重启、镜像回滚或数据库修改。

实际执行结果：

- 基线读取中位数为 41.263 MiB/s。北京时间 12:50:49 尝试将宿主层 `plugins.configs.cpa-quota-estimator.enabled` 置为 false。
- 30 秒后仍有 6 个活跃 SQLite 文件描述符，未满足隔离条件，按预定安全规则提前中止，没有继续满 5 分钟，也不构成有效 A/B 对照。
- 12:51:20 恢复配置。原始字节的 SHA256 一致，退出兜底再次确认已恢复。容器身份、healthy 状态及重启计数与试验基线一致；重启计数原本为 1，不是此次试验产生。
- 随后只读检查发现配置文件监听缺失，仅认证目录监听仍存在。执行脚本使用原子替换写配置，与当前 CPA 的单文件 inode 监听不兼容。本地最小复现产生 `ATTRIB → DELETE_SELF → IGNORED`，而核心配置事件分支不处理这一删除并重新挂载监听。这是试验写入方法的设计疏漏；**恢复文件字节并未恢复完整运行态**。
- 提出了仅重启 CPA 恢复监听的建议，但没有获得执行授权。用户随后要求停止处理并结案，未执行重启或再次切换。配置热监听缺失仍是已知限制，不能声明已修复。

按最终指示清理了本次试验目录 `quota-isolation-20261009T044939Z`（位于远端 `.ai-ops-state/`）中的 10 个文件，包括本次配置备份、临时脚本和采样文件；清理前确认瞬态试验服务已 inactive/not-found。另删除本地本次创建的 10 个临时脚本、下载结果和匿名 registry token 文件。必要的脱敏结论保留于本文。在这一阶段，原有历史备份、原始取证库、活跃数据及升级前镜像尚未删除；用户随后另行批准了下述历史资料清理。

## 追加家目录与 Docker 清理

只读盘点后，用户明确选择“连旧数据库备份也清理”，批准删除固定清单中的 21 个历史目录、6 个临时文件和 5 个无容器引用镜像，包括旧数据库备份及升级前镜像。

执行与验证结果：

- 删除 `/root` 下这 21 个历史部署目录，包括 `rollout-sub2api-029-20260928/sub2api.pgdump` 所在目录；另删除 6 个 force-refresh 临时文件。没有删除活跃 `/root/AI-gateway` 项目或其中的生产数据。
- 删除下节记录的 3 个升级前镜像，以及旧 keepalive 镜像 `sha256:76c8d6e534e62d361eb24e519cb5bf4afcacdb14b07069001e413b040c2607b0`、curl 工具镜像 `sha256:58adaa4e8dca9c988bae2aba4ab3434a0bb2da16bbe3f92dec39ec7785166777`。
- 执行前重新检查文件类型、挂载、进程引用和所有容器的镜像引用；使用明确路径和镜像引用删除，没有执行 prune 或强制删除。
- 执行后确认目标路径和镜像已不存在；49 个容器的身份、镜像、运行状态、启动时间及重启计数均未改变，两个 Docker 卷保留。剩余 18 个镜像均被容器引用。
- 根文件系统可用空间实测增加约 **705.2 MiB**，占用由 65% 降至 62%。没有执行服务重启或新的业务请求回归。

删除的旧数据库备份与本地回滚镜像不再可用。镜像引用仍保留在本文，但需要从 registry 重新获取；此次未验证远端保留情况或替代数据库备份的可恢复性。不得把 Docker 总览的“可回收”数值直接当作可安全继续删除的空间。

## 回滚资料与限制

升级前完整镜像引用记录（追加清理后，生产本地缓存已删除）：

```text
PROVIDER_SIDECAR_IMAGE=ghcr.io/tridefender/zcode-proxy:4.7.2@sha256:83c4f01436c86ed095a5a42a7e353dd5eee43c0fb366b3263ad5d78ac78b60a3
CLI_PROXY_IMAGE=ghcr.io/xz-dev/cli-proxy-api:v8.0.4-pr6189-8f82146@sha256:eeeac396baaaeec7868201eb0b779f423c7d4d3303c49e3d52cc827b1d4ae19b
SUB2API_IMAGE=ghcr.io/xz-dev/sub2api:0.2.10-pr7157-pr7217-pr3172-9a21b93@sha256:8c2f6075379d77ff52457e9b7235d3410d81afca0cb2b1aa31109cd3c3fc0262
```

这些引用是回滚资料，不是已验证的无条件回滚方案。本次未执行回滚演练，也未建立经验证的 Sub2API 数据库回退方案。

实际回滚前须另获批准，确认配置及数据库备份可恢复，检查 schema/插件数据与旧版本兼容性，再对单个镜像变更做 plan 并逐服务部署。尤其不能假定 Sub2API 换回旧镜像即可安全回退；不要盲目恢复共享 `.env` 而覆盖其他服务的新状态，也不要把活跃 SQLite 主文件单独复制当作完整备份（需考虑 WAL 一致性）。
