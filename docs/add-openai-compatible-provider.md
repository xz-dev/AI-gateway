# 新增 OpenAI 兼容来源（provider channel）操作手册

通用流程：把一个新的 OpenAI 兼容商家（如 AIHub、Zakk 类）接入 AI-gateway 生产栈。目标主机 `root@rainyun-la` (100.94.238.35)，工作目录 `/root/AI-gateway`。

涉及四层，顺序固定：**Squid 出口放行 → CPA 渠道注册（含模型列表）→ AISIX 路由 → 验证**。缺任何一层都会无声失败。

## 0. 前置（必备）

- 商家的 **API key**（CPA config 写入，不落 repo）。**先直连验证**：
  - `GET https://<base>/v1/models` 应返回模型列表（非 401）。
  - `POST /v1/chat/completions` 用一个**便宜模型**先打通 —— **models 列表列出 ≠ 推理可用**。本次 AIHub 教训：`/v1/models` 列出 gpt-5.6-terra，但 `chat/completions` 对该 token 下所有 gpt 模型返回 `unknown provider`，是商家分组/权限问题，栈无法修复。先打通再用。
- 决定公开 catalog 前缀（`prefix`，如 `aihub`）。**改动已对外暴露的 `zakk/` 这类前缀是破坏性变更，调用方须同步迁移。**

## 1. Squid 出口放行（`data/egress-proxy/policy.json`）

Squid 是白名单默认拒绝。CPA 容器走 `cpa-squid-relay` → egress-proxy。

在 `services.cpa.destinations` 添加（最小放行，禁整段 `/api/` 前缀）：

```json
{ "domain": "aihub.top", "tls": "bump",
  "methods": ["GET"],  "paths": ["^/v1/models($|[?])"] },
{ "domain": "aihub.top", "tls": "bump",
  "methods": ["POST"], "paths": ["^/v1/(chat/completions|responses|embeddings|images/(generations|edits)(/async)?)($|[?])"] }
}
```

应用（render 只重写 `data/egress-proxy/generated/`，squid `reconfigure` 热加载）：

```bash
python3 scripts/render-egress-policy.py data/egress-proxy/policy.json data/egress-proxy/generated
docker exec ai-gateway-egress-proxy-1 squid -f /etc/squid/generated/squid.conf -k parse
docker exec ai-gateway-egress-proxy-1 squid -f /etc/squid/generated/squid.conf -k reconfigure
```

验证：`docker logs ai-gateway-egress-proxy-1` 应见该域名 `CONNECT` 200，非 `status=403`。被 Squid 拦时 CPA 侧报错会嵌一段 Squid 的 HTML 错误页 —— 见到 HTML 就是 Squid，见到 JSON `{"error":...}` 就是过了 Squid、到上游或 CPA 本身。

**hash 锁**：改前 `sha256sum` 记 hash + `cp` 备份到 mode-0700 目录（本次 `/root/rollout-aihub`），改前校验未漂移。

## 2. CPA 渠道（`data/cpa/conf/config.yaml` 的 `openai-compatibility`）

CPA 热加载此文件（日志 `server clients and configuration updated` 即重载），一般无需重启。

```yaml
- name: AIHub
  prefix: aihub
  base-url: https://aihub.top/v1
  api-key-entries:
    - api-key: <商家 key>
  models:
    - {name: gpt-5.6-terra, alias: ""}
    # ... 每个要暴露的模型一行。CPA 公开 ID 为 <prefix>/<name>
```

**`models` 必须非空、模型名必须是上游真实 ID**（prefix 由 CPA 加，列表里写裸 ID）。空列表有时 CPA 不注册该渠道。可先用商家 `/v1/models` 实际内容填充。

**改后必测**：用 CPA 自身的 `api-keys` key（非 mgmt.key）走 CPA：
```bash
curl -H "Authorization: Bearer $CPA_CLIENT_KEY" http://127.0.0.1:8317/v1/models   # 应见 aihub/gpt-5.6-terra
curl -X POST .../v1/responses -d '{"model":"aihub/<model>","input":"hi"}'         # 通则 CPA 层 OK
```
- `model_not_found / unknown provider for model <裸名>`：CPA 对该 prefix 未注册（或剥前缀后上游不识别 —— 用直连排除上游问题）。
- 嵌 Squid HTML：回第 1 步。

管理 API（只读盘点用，写操作走配置文件更可控）：`mgmt.key` 在 `data/cpa/mgmt.key`，`GET http://127.0.0.1:8317/v0/management/openai-compatibility`。

## 3. AISIX 路由（`aisix/resources.yaml`）

**文件权限坑**（本次踩过）：AISIX 容器跑 uid 10001(`aisix`)，aisix-status 跑 65532(nobody)。bind-mount 文件须 `chown 10001:10001` 目录(755)，`resources.yaml` 600（仅 aisix 读），`config.yaml` 444（status 页要读 token 调 admin API）。否则容器 `Permission denied` 重启崩环、`/status` 页 unavailable。

**直连目标**：每个 `prefix/model` 一个 direct model，`display_name`/`model_name` 与 CPA 名逐字节一致，`provider_key: cpa-pool`。

**逻辑模型 failover 链**（如 `gpt-5.6-terra`）：`strategy: failover`，`targets` 有序候选。`max_fallbacks` 不写 — `deploy-aisix` preflight 会自动跑 `scripts/ops/normalize-aisix-resources.py` 把它改成 `len(targets)-1`，你只改 targets 列表即可。注意 `retries: N` 会放大上游调用：最多 `(N+1) × (max_fallbacks+1)` 次，需有界。

应用：`docker compose up -d --no-deps --no-build --pull never --force-recreate aisix`（只重建 aisix，不动其它）。验证 `docker logs` 见 `resources loaded ... resources=N`。

## 4. models-enricher 目录来源（`models-enricher/config.yaml`）

新 prefix 出现在 CPA 目录后，enricher 决定元数据。默认渠道继承全局 `source_priority` 链。若上游目录无需抓取：`channels.<prefix>: {fetch_models: false}`（zakk/aihub 这类按需开）。改后 `go test ./...`（`configured_catalog_test.go` 硬编码渠道清单需同步）。

WebSocket 入站由 Sub2API 统一鉴权和处理；内部 models gateway 不再维护独立 WS 别名表。

## 5. 验证清单

1. Squid 日志该域名 200 非 403。
2. CPA `/v1/models` 含 `prefix/model`；`/v1/responses` 真实推理通。
3. AISIX `/status` 可见该 direct/链，`eligible`。
4. enricher `/v1/models` 富目录含新 ID 且元数据合理。
5. 容器无重启崩环。

## 替换旧来源时的顺序（重要）

**先新后旧，推理打通才下线。** 本次教训：先把 zakk 的 Squid 放行删了再发现 aihub token 推理不通，导致唯一可用渠道降级。正确顺序：
1. 加新来源全部四层，**一次真实推理成功**为门槛。
2. 确认调用方已迁移到新 `prefix/`。
3. 才移除旧 prefix 的 CPA 渠道 + Squid 规则 + AISIX direct + enricher 配置。
