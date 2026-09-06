# 模型目录与别名路由：本轮收尾

日期：2026-09-05。

本轮交付是 **AI-gateway 模型目录、别名路由与 Pi 客户端模型注册的收敛**，不是一轮新的 CPA 六插件开发。用户已停止额外开发与生产调整；本次只整理文档、同步主规范、执行已有离线检查，并归档阶段 4。不提交、不推送、不部署，不修改用户刚调整的配置。

## 1. OpenSpec 范围与状态

工作区有两个独立 OpenSpec 根，不能把一处 `openspec list` 当成全部变更：

- 工作区根：`/root/cpa-plugin-mono/openspec/`，承载阶段 1–4；其父目录不是 Git 仓库。
- 网关仓库内：`AI-gateway/openspec/`，保留基础网关选型/实现变更。

| 变更 | 本轮内容 | 收尾状态 |
| --- | --- | --- |
| `add-bifrost-codex-models-gateway` | Bifrost 可行性验证 | **SUPERSEDED**。WS 语义不适配且官方镜像不能按原方案加载 Go 插件；不 apply |
| `add-apisix-models-gateway` | 专用 APISIX、Go enricher、relay/Compose 接线，替换 Sub2API→CPA 直连 hop | 仓库内任务 23/23；仍保留原目录，本次未移动 |
| `add-alias-fallback-routing` | HTTP 模型别名、429/5xx 回退、static metadata 继承、CPA 裸名入口过滤 | 根 OpenSpec 已归档；任务 15/15 |
| `add-alias-ws-and-source-priority` | WS 别名、目标池/粘性、流前故障回退、来源链与 authType 分流 | 根 OpenSpec 已归档；历史文件为 **35/36**，`pi end-to-end check` 仍未勾选，本次不补勾或改写历史 |
| `codex-models-passthrough-cutover` | 前门串行目录交集、catalog sidecar；保留账号 mapping，退役元数据快照注入 | 根 OpenSpec 已归档；任务 27/27。名称中的 passthrough 不代表最终采用删除 mapping 的方案 |
| `catalog-proxy-cache-and-parallel-fetch` | 并发、缓存、元数据权威、完整目录与 Pi 过滤 | 既有任务 19/19；本次完成文档协调与主规范同步，归档状态见第 7 节 |
| `remove-ws-alias-proxy` | 曾提出删除 WS 桥 | **WITHDRAWN**。保留代理；不 apply，不当成待开发任务 |

早期阶段曾使用全局默认来源链、provider rank、`id@`、Sub2API 元数据快照。这些是历史步骤，不是最终配置指南：阶段 3 退役快照；阶段 4 替换默认链、猜测规则和 `id@`。

## 2. 网关具体做了什么

### 目录与权限

```text
客户端带参 /v1/models
  → 前门 APISIX
      1. Sub2API：鉴权，取得账号 mapping 并集 ∩ 组清单的 slug 集合
      2. model-catalog-sidecar → apisix-models/cache → models-enricher
      3. 按 slug 取交集，元数据只取 enriched catalog
```

- Sub2API 仍是 key/组策略管理者，不把它本地生成的模板能力字段作为元数据来源。
- `models-enricher` 先读取有效 CPA native manifest，再发现并抓取已启用 API-key 渠道。OAuth/native 模型不靠外部来源合成。
- 前门无参数清单保留 Sub2API 行为；内部 apisix-models 的 `/v1/models` 不再由 catch-all 直通 CPA。缺 `client_version` 时由 enricher 返回 400。
- 不把元数据回写为 Sub2API DB 快照。新增模型可见性同时受 enriched inventory、账号映射和组清单限制；只增加组条目不够。

### 元数据解析

- CPA `prefix` 是渠道配置唯一身份；`name` 只用于展示/日志。缺配置、空链、重复身份与 custom/native 冲突明确失败。
- 来源必须显式限定：`models.dev/<provider>`、`modelparams.dev/<provider>/api_key|subscription`、渠道原生 `ollama_cloud`。
- 模型链整体替换渠道链；删除全局默认链、跨 provider 排序猜测、大小写/日期/tag 变体猜测。
- `lookup_ids` 按 source token 指定精确 ID。`ollama_cloud` 经 CPA api-call 查询显式配置的 `/api/show`，渠道凭证留在 CPA。
- 配置来源链先清除 capability 模板，再按字段 first-hit-wins；显式 overrides 覆盖来源结果。models.dev 的 api.json 优先，models.json 只补缺。
- `custom_channels` 只形成隐藏 metadata pool，`static_models` 通过普通 pool slug 继承后输出 alias；`id@` 已拒绝。
- `metadata_from` 显式引用另一完整 slug。`max_input_tokens` 只来自真实声明的输入上限，不做 context-minus-output 推导。
- 网关保留上游完整目录和 null 能力字段，不替 Pi 隐藏图片、视频等非对话模型。

### 性能与可观测性

- 所有出站 HTTP 共用有界池，默认并发 8；渠道并发抓取，输出合并顺序确定。
- 同 `client_version` 的并发构建共享在途结果，不同版本隔离。enricher 不保留跨请求 SourceCache/last-good。
- APISIX 是缓存 owner：URI + client_version、200-only、fresh TTL 120s。仍保留的过期条目可在网络错误、超时、502/503/504 或更新中返回 stale；500 不走 stale。**120s 不是故障期间最大陈旧时间**。容器重建接受 cold cache。
- native manifest 构建失败不合成部分目录；有可用 stale 可返回旧完整目录，无可用缓存才向前门传播 502。
- `/models-table` 只在 localhost/Tailscale 绑定发布，展示排序后的模型、真实 input/output limits、推理等级及 null。
- sidecar 禁止 nginx 临时文件响应缓冲，避免并发大清单耗尽小 tmpfs 导致截断；enricher 的内存限制和同版本并发构建也已收敛。

### 别名与 WS

- HTTP alias 由 `ai-proxy-multi` 承担，支持配置目标池、chash 粘性以及 429/5xx 回退。能力存在不代表每个 alias 当前都有多个启用目标。
- `ws-alias-proxy` 保留：处理帧内 alias、目标选择及流前故障回退，并为需要的渠道提供 WS→HTTP/SSE 桥。
- 不做已经输出 delta 后的跨渠道重放。`remove-ws-alias-proxy` 不再执行。

## 3. Pi 插件做了什么

仓库：`xz-dev/openai-api-pi-extension`；本轮提交 **`2b8edc4` — `fix(models): filter catalog for Pi agent use`**。

- 识别 `id/slug` 与显示名称，过滤明确 embedding/image/video/audio 类型、非文本输出及明确非对话标识。
- 保留 **image input + text output** 视觉对话模型。
- 合法对话模型缺 context limit 时使用 **128,000**；缺 output limit 时使用解析后的 context limit。网关显式有效 limits 优先。
- 不因缺 limits 拒绝整个刷新；非法条目仍拒绝，过滤后零可用模型仍失败，不放宽这些边界。
- 同步修改目录映射与 provider 生命周期测试、README；该提交不包含依赖变更。

128,000 是客户端 fallback 策略，不是对上游真实能力的保证，也不会写回网关元数据。

**不计作本轮新增**：`api: openai-responses`、普通 API-key/base-URL 登录、SSE/WebSocket/WebSocket Cache、旧 cached WS 失败后的 fresh-WS/SSE 恢复和 cooldown。这些是先前提交已有能力，本轮目录过滤沿用它们。

## 4. 提交与工作区边界

| 仓库 | 已有提交 | 范围 |
| --- | --- | --- |
| AI-gateway | `be611de` | 基础 apisix-models、catalog sidecar、enricher、WS alias proxy 集成 |
| AI-gateway | `58dcafd` | 阶段 4 主实现、测试和文档；含删除两份临时 live test |
| AI-gateway | `b59a759` | 移除渠道 include 限制，恢复完整模型 inventory |
| Pi 插件 | `2b8edc4` | agent 模型过滤、缺 limits fallback、测试及 README |

本次本地核对：AI-gateway HEAD 与本地 `origin/main` 均为 `b59a7598fcca864d62fcabe46492bd587c15b6f2`；Pi 插件均为 `2b8edc426a6a6e272999fbfa4f560e7f94440972`。这与前次推送记录一致；本次未 fetch、查远端分支或运行 CI，不能据此声明新的远端 CI 结果。

### 保留的两个未提交修正

| 修正 | 文件 | 已知状态 |
| --- | --- | --- |
| GPT-6 native limits | `models-enricher/merge.go`、`models-enricher/enricher_test.go` | 为 `gpt-6-astra` 增加模板清理豁免，保留 272000 context / 128000 output；前次记录曾以 v0.7.5 部署，本次不重验生产，也未提交 |
| WS→HTTP envelope | `ws-alias-proxy/main.go`、`ws-alias-proxy/main_test.go` | HTTP Responses body 移除顶层 `type`，保留 input、tools、reasoning、previous_response_id；仅本地尾项，本次无部署证据 |

四个文件在收尾前后保持原字节。文档整理不是这两组代码的提交、发布或生产验收。网关 README 与本总结是本次新增的未提交文档变更；Pi 插件工作区保持干净。

## 5. 部署证据边界

- 历史现场证据位于 [deploy-canary-report.md](../deploy-canary-report.md) 与工作区各 archived change 的任务/证据文件。不同版本、不同日期的 canary 不可合并成一次全栈最终验收。
- 停止额外工作前的最后一组目录查询记录：Sub2API 92 条、前门 73 条；前门出现 `codex/gpt-6-astra`，context 272000、max_tokens/max_output_tokens 128000。这只证明那次目录查询，不证明 GPT-6 推理成功或当前用户调整后的现场状态。
- 用户已自行完成设置。本次没有 SSH、生产读取/写入、镜像构建/部署、服务重启、推理请求、Pi reload 或 live refresh。
- 阶段 2 历史 `pi end-to-end check` 未勾选，不能用阶段 4 目录刷新或本次 mock 测试冒充补验。

## 6. 本次离线验证

2026-09-05 在当前保留补丁的工作区执行：

| 目录 | 命令 | 结果 |
| --- | --- | --- |
| `models-enricher` | `go test -race -count=1 ./...` | PASS |
| `models-enricher` | `go vet ./...` | PASS |
| `ws-alias-proxy` | `go test -race -count=1 ./...` | PASS |
| `ws-alias-proxy` | `go vet ./...` | PASS |
| Pi 插件 | `node --test tests/map-models.test.ts tests/provider-lifecycle.test.ts` | 27/27 PASS |
| 工作区根 | `openspec validate catalog-proxy-cache-and-parallel-fetch --strict` | PASS（移动前） |
| 工作区根 | `openspec validate --specs --strict` | 3/3 PASS |
| AI-gateway | `git diff --check` | PASS |

Go 检查使用本地工具链、`GOPROXY=off`；未安装依赖。未执行 Pi 全量 transport E2E、在线刷新、生产故障演练、Compose 重建或远端 CI。

## 7. 主规范与归档

已协调阶段 4 proposal/design/tasks 与两份 delta，修正旧整单拒收、渠道模板优先、name 必填、全 query 缓存键、故障一律 502、120s 总陈旧上限，以及只看组清单的错误表述。

同步目标（工作区根）：

- `openspec/specs/codex-models-catalog/spec.md`：15 requirements / 29 scenarios。
- `openspec/specs/enricher-source-priority/spec.md`：8 requirements / 26 scenarios。

逐条比较 delta 与主规范完成，未涉及的主规范条目保留；目录 entitlement 需求明确改名，旧名不再残留于当前主规范。`alias-fallback-routing` 与以前的 archived changes 原样保留。

阶段 4 已归档至 `openspec/changes/archive/2026-09-05-catalog-proxy-cache-and-parallel-fetch/`，包含原 `.openspec.yaml`。归档前后 6 个文件的相对路径/内容摘要一致；主规范已同步并通过 strict 验证。归档不等于所有工作区干净，也不等于重新部署成功。
