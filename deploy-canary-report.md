# Canary report: apisix-models + models-enricher

Date: 2026-09-03
Host: RainYun-S5mc8BXc (`100.94.238.35`)
Scope: OpenSpec `add-apisix-models-gateway` tasks 2.9 + 4.1
Cutover (4.3): **not done**. Legacy `ai-gateway-sub2api-cpa-relay-1` still Up 45h.

## Deployed artifacts

| Piece | Evidence |
|---|---|
| Image `models-enricher:v0.1.0` | host `sha256:cdb89b6eb0bc...` size ~11 MB. Built locally (static Go binary + distroless `gcr.io/distroless/static-debian12:nonroot@sha256:52dcfbabb7...`) then `docker load`. Host `docker build` failed: docker0 missing (`Device does not exist`). |
| APISIX | `apache/apisix:3.18.0-debian@sha256:84e6b5e787e9...` (same digest as public APISIX) |
| Bind | `127.0.0.1:9081 -> :9080` on `apisix-models-host-netns` |
| CPA_MANAGEMENT_KEY | already present in host `.env`; not printed |

New containers (all running; none of the pre-existing 30 names disappeared):

- `ai-gateway-apisix-models-1` healthy
- `ai-gateway-apisix-models-{netns,host-netns,host-relay,cpa-relay,enricher-relay}-1`
- `ai-gateway-models-enricher-1` (listen `:8090`)
- `ai-gateway-models-enricher-netns-1`
- `ai-gateway-enricher-{cpa,squid}-relay-1`

`cpa-netns` was `docker network connect`'d (not recreated) onto:

- `ai-gateway_apisix-models-cpa-target` `172.30.36.2`
- `ai-gateway_enricher-cpa-target` `172.30.30.2`

Existing `cli-proxy-api`, public `apisix-1`, `sub2api-cpa-relay` untouched.

## Compose IP remap (required)

Host `compose.override.yaml` already occupies `172.30.21.0/29`–`172.30.24.0/29` (provider-sidecar / zcode squid). Tracked compose originally reused those subnets.

Remapped in `compose.yaml` before sync:

- `172.30.21` → `33` (apisix-models-host-source)
- `172.30.22` → `34` (host-apisix-models-target)
- `172.30.23` → `35` (apisix-models-cpa-source)
- `172.30.24` → `36` (apisix-models-cpa-target)

`172.30.27`–`32` were free and kept.

`docker compose up -d --no-deps --no-build --pull never` for the ten new services only. No existing container recreated.

## Canary (client_version=0.55.0)

Queries from host loopback (no secrets in this file):

- baseline: `GET http://127.0.0.1:8317/v1/models?client_version=0.55.0` (CPA host bind)
- new hop: `GET http://127.0.0.1:9081/v1/models?client_version=0.55.0`

| | CPA baseline | apisix-models hop |
|---|---|---|
| HTTP | 200 Codex | 200 Codex |
| models | 114 | 62 |
| prefixed slugs | 62 | 62 |
| bare slugs | 52 | **0** |
| intersection of prefixed | | 62/62 identical set |
| only-in-baseline | 52 bare ids (`claude-opus-5`, `glm-4.7`, …) | dropped as specified |
| only-in-enriched | 0 | |
| missing `max_tokens` (prefixed) | 28 | 28 (no fill vs baseline) |
| latency | 0.11s | 1.63s |

Passthrough `GET /v1/models` (no `client_version`): SHA-256 of bodies **equal** (`e7ca8551fb373224`, 10181 bytes, n=114 OpenAI list).

Chat: `POST /v1/chat/completions` model `shuaiapi/claude-fable-5` `max_tokens=8` → CPA 200 and new hop **200**.

## Enricher logs (sanitized)

Channel fan-out partial failures (spec: tolerated):

- 403: Ollama Cloud, 旋律, OpenRouter, Command Code, Nvidia NIM, 帅API, gmicloud, 硅基流动
- 404: ZCode

No native-manifest failure. Native CPA Codex list is the merge base; prefixed entries preserved.

## What did **not** happen

1. **models.dev / modelparams.dev fill**: zero extra `max_tokens` vs CPA native prefixed rows. Host squid has `models.dev` / `modelparams.dev` allowlists only on the **CPA** listener (`172.30.18.2:3128`, client `172.30.18.3/32`). Enricher egress is `172.30.31.3 → 172.30.32.2:3128`, and `egress-proxy` was **not** attached to `enricher-squid-target` (would recreate squid). So source HTTP cannot succeed until:
   - add an `enricher` service to host `data/egress-proxy/policy.json` (`listen: 172.30.32.2:3128`, `client: 172.30.31.3/32`, GET `models.dev` `{api,models,catalog}.json` + `modelparams.dev` `/api/v1/models.json`)
   - render + reload squid
   - `docker network connect --ip 172.30.32.2 ai-gateway_enricher-squid-target ai-gateway-egress-proxy-1`
2. **Channel live lists** mostly 403/404 via CPA `api-call` (provider-side blocks `/v1/models` or wrong path). Manifest still complete from CPA native + prefix filter.
3. Host `docker0` bridge missing: blocked in-daemon `docker build`; image loaded from local podman archive instead. Custom compose networks still work.

## Rollback (no cutover, so trivial)

```
docker compose -f /root/AI-gateway/compose.yaml stop \
  apisix-models apisix-models-host-relay apisix-models-host-netns \
  apisix-models-netns apisix-models-cpa-relay apisix-models-enricher-relay \
  models-enricher models-enricher-netns enricher-cpa-relay enricher-squid-relay
docker network disconnect ai-gateway_apisix-models-cpa-target ai-gateway-cpa-netns-1
docker network disconnect ai-gateway_enricher-cpa-target ai-gateway-cpa-netns-1
```

Sub2API path unchanged.

## Next (not this canary)

- 4.2 Codex CLI / `pi list models` through Sub2API still hits **legacy** relay until 4.3.
- 4.3 cutover: point Sub2API CPA hop at `apisix-models-host` (`127.0.0.1:9081` internally via relay), then remove `sub2api-cpa-relay`.
- Squid enricher listener before expecting models.dev fill.

---

## 2026-09-03 — add-alias-ws-and-source-priority 部署

### 上线内容
- apisix-models：4 条别名路由（gpt-5.6-sol/terra/luna + grok-4.6）全部启用
  `balancer: {algorithm: chash, hash_on: header, key: session_id}`（nginx 变量下划线
  小写，`Session-Id`/`session-id` 都会解析为 nil → 全员单点，已实证修复）；
  实例补 `auth.header.Authorization`（3.18 schema 强制，缺失时路由被静默丢弃）；
  apisix.yaml 改模板渲染（`__CPA_API_KEY__` 占位 + entrypoint sed，密钥不落盘 repo）。
- grok-4.6 双通道 LB：oauth grok（CPA 裸名 grok-4.6）+ xl/grok-4.6，weight 1:1。
- ws-alias-proxy v0.2.0：**托管模式**（别名 response.create → HTTP POST stream →
  SSE 事件转 WS 帧 + sequence_number 注入；非别名帧懒拨号 WS 透传）。
  根因：CPA 的 WS 对 openai-compat 渠道行为不一致（axis 静默断连/zakk 正常），
  别名帧不再依赖 CPA WS。
- models-enricher v0.4.2：三级 source_priority（模型>渠道>全局>默认）、
  modelparams authType 双索引、static inherit `id@<model-id>` 源直查、
  源拉取失败 WARN + refreshed 计数日志。statics 迁移 id@ 优先。
- CPA：axis（元流）渠道补登 gpt-5.6-luna（上游实测支持，渠道声明漏了）。
- egress-proxy：enricher 策略 client 修正 172.30.31.3→172.30.32.3
  （squid 看到的源是 relay target 侧 IP；此前源拉取长期 403，
  models.dev/modelparams 在生产从未生效——channel 声明字段掩盖了该问题）。

### 验证证据
- manifest 前后 diff：744→744、0 增 0 删 0 字段变化；bare 别名恰 4 个。
- 4 别名 HTTP chat 全 200；grok LB 日志实证 spread（grok-oauth/xl 混合）
  与同 Session-Id 粘滞（3/3 同实例）。
- failover 演练（临时路由，roundrobin 必中死端点）：4/4 透明 200，已拆。
- WS：别名（sol/grok-4.6/team-a 斜杠别名）托管模式事件流正常；
  非别名（zakk/）透传回归正常。
- team-a/gpt-5.6-sol 斜杠别名：manifest（ctx 1050000 经 id@）+ HTTP + WS 三链通。

### 已知问题（非本次栈）
- axis 渠道不稳（usage: failed 25+；responses 端点曾整段挂起）— 单成员别名池
  无真冗余，建议后续给 sol/terra/luna 池补第二成员。
- zcode/glm-5v-turbo：CPA auth_unavailable（该模型级冷却），其余 zcode 9 个 +
  kimi-coding 8 个全量 e2e 200。"zcode 挂了"的观感与实际不符——CPA 层健康。
- 本机 pi -p 挂起（不发起任何 HTTP，扩展环境问题），pi e2e 待主人环境验证。

### 2026-09-04 追加 — 元数据正确性（models-enricher v0.5.1）

**根因**：pi 列表 272K 全是两层捏造叠加 — ① CPA rich 端点对非 codex 目录型号克隆
gpt-5.5 模板（272000/120000），enricher fillGaps 信任穿透；② Sub2API 本地目录生成
用硬编码兜底表（fallback 272000），从不抄我们的 manifest。

**修复**：
- enricher v0.5.1：值级剥模板（非 codex 目录型号 && ctx==272000 → 剥 ctx/max/reasoning，
  8 个真实 codex 型号末段豁免；契约字段 shell_type 等保留）；models.dev 索引加官方厂商
  rank（消除 Go map 随机）；max_output_tokens 双发。
- config：zcode/glm-5.3 static 推理 levels（官方 docs.z.ai：1M/128K，low/high/max 默认 max）；
  18 个裸名 statics（kimi/glm 全家）inherit [id@前缀, 前缀池条目]。
- Sub2API：admin sync-upstream 在本网络不可行（内部 CPA hop 不能过 squid，直连无 DNS；
  已回滚临时 proxy_id 改动）→ 改为把 :9081 清单转 upstream_model_metadata 快照直写
  5 个 CPA 账号 extra（/tmp/gen_snap.py 可重跑刷新；不动 model_mapping）。
- egress：sub2api policy 加 models.dev（保留备用）。

**验证**：aiapi 实测 kimi-k3=1048576、k3-256k=262144、glm-5.3=1000000+[low,high,max]/max、
glm-4.6=204800；grok-4.6=500000、gpt-5.6-sol=1050000 回归正常。

**已知残余**：
- Sub2API codex 目录无 max-output 字段，pi 第二列显示为 ctx 回退（上游限制）。
- codex/gpt-5.x 原生条目仍是 Sub2API 硬编码/CPA 模板值（主人决策：oauth provider 的数据本来就不可能全对）。
- 清单更新后快照需重跑 gen_snap.py 刷新（点式拷贝）。
- 旧记忆"GLM-5.3 输出必须 180000"与官方文档（128K=131072）冲突，本次采用官方值。
