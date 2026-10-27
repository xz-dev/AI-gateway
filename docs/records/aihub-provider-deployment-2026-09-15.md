# AIHub 接入 + terra 负载均衡部署 — 2026-09-15

## 结果

**目标**：接入 AIHub（`https://aihub.top/v1`）作为来源 `aihub/`；AISIX 逻辑模型 failover 链按指定顺序追加/替换尾目标。`zakk/` 渠道暂不下线。

**AISIX 逻辑链（`/status`，3 fallbacks / 4 attempts）**：
- `gpt-5.6-terra`：`xl/xz` → `codex` → `axis` → `aihub`
- `gpt-5.6-sol`：`xl/xz` → `codex` → `axis` → `aihub`（替换原 `zakk`）
- `gpt-5.6-luna`：`xl/xz` → `codex` → `axis` → `xl/gpt-5.6-luna`
- `gpt-6-astra`：`xl/xz` → `codex` → `axis` → `aihub`（替换原 `zakk`）

**直连名 vs 逻辑名**：请求 `axis/gpt-5.6-sol` 不会 failover 到 aihub。axis 渠道已手动 disabled，直接打 `axis/*` 会得到 `unknown provider`。要走链必须请求逻辑 ID（`gpt-5.6-sol` 等）。

**现状**：
- ✅ Squid 放行 `aihub.top`（GET `/v1/models` + POST `chat/completions|responses|embeddings`）；`ai.card.lc` 规则保留。
- ✅ CPA 渠道 `AIHub`/prefix `aihub` 有效。直连 `chat/completions` 对 terra/sol/5.5 已返回 `pong`。
- ✅ AISIX 加载 `resources=62`，`aisix`/`aisix-status` healthy，`/status` 200。
- ✅ `Zakk` 渠道保留，未下线。

## 变更明细

### 生产（rainyun-la `/root/AI-gateway`）
- `data/egress-proxy/policy.json`：+2 条 `aihub.top` 规则；`ai.card.lc` 两条恢复保留。
- `data/cpa/conf/config.yaml`：`AIHub` 渠道 `aihub` + 9 个真实上游模型（`gpt-5.3-codex-spark`, `gpt-5.4(-mini)`, `gpt-5.5`, `gpt-5.6-sol/-terra`, `gpt-6-astra`, `gpt-image-1.5/2`）。`Zakk` 渠道保留未动。
- `aisix/resources.yaml`：+ direct `aihub/gpt-5.6-terra`、`aihub/gpt-5.6-sol`、`aihub/gpt-6-astra`、`xl/gpt-5.6-luna`；对应逻辑链尾按上表调整，`max_fallbacks=3`。
- 权限修正：`aisix/` 目录 755 + uid 10001，`resources.yaml` 600，`config.yaml` 444（status 页 nobody 要读）。
- 备份 + hash 锁：`/root/rollout-aihub/`（mode 0700 父目录，0600 文件）。

### Repo（待提交）
- `models-enricher/config.yaml` + `configured_catalog_test.go`：`zakk` → `aihub` 渠道键（`fetch_models: false` 继承全局链）。288 测试通过。
- `ws-alias-proxy/config.yaml` 的最终 provider fallback 已由提交 `c383dc0` 归档；随后 WS alias 边车退役，Sub2API 鉴权后的下游 WS 由 models gateway 直接转发。
- 本文档 + `add-openai-compatible-provider.md`。

## 已验证
- Squid `squid -k parse`/`reconfigure` 通过。
- AISIX `resources loaded ... resources=62`；`/status` 四条逻辑链目标顺序如上。
- AIHub 直连 `chat/completions` 对 terra/sol/5.5 返回 `pong`。

## 待办
1. 调用方请求逻辑模型名（不要直接打 `axis/*`）。
2. 确认调用方已切到 `aihub/` 后再下线 `zakk/`。
3. 提交 repo 侧 enricher/ws-alias/文档变更。
