# Sub2API 错误透传配置指南

> 用途：让上游（CPA / Kimi / Codex 等）的真实错误原样回到客户端，替代通用的
> `upstream_error: Upstream request failed`。配置后可退役 fork 补丁 `1c6661949`（错误透传）
> 与 `b35b45c`（WS 桥 4xx 诊断透传），符合"生产追官方镜像 + 配置"的政策。

## 背景

sub2api 默认把所有上游失败映射成通用 502/`upstream_error`，真实 4xx（如 Kimi 的
`Invalid request: text content is empty`）被吞掉，客户端无法定位问题且会无效重试。
上游维护者明确拒绝把"4xx 透传"写死进代码（PR #4176 被指路用配置），官方答案就是
**Error Passthrough 规则**（v0.2.4 起内置，表 `error_passthrough_rules`）。

相关上游 issue（持续开放，等官方方案）：#2054、#6506。

## 规则模型（`error_passthrough_rules` 表）

| 字段 | 类型 | 说明 |
|---|---|---|
| `name` | string | 规则名（必填） |
| `enabled` | bool | 是否启用 |
| `priority` | int | 数字越小优先级越高 |
| `error_codes` | int[] | 匹配的上游状态码（OR） |
| `keywords` | string[] | 匹配的错误消息关键词（OR） |
| `match_mode` | `"any"`/`"all"` | 任一条件 or 全部条件 |
| `platforms` | string[] | 适用平台（如 `["openai"]`） |
| `passthrough_code` | bool | 透传原始状态码 |
| `response_code` | int? | `passthrough_code=false` 时的自定义码（必填其一） |
| `passthrough_body` | bool | 透传原始错误消息 |
| `custom_message` | string? | `passthrough_body=false` 时的自定义消息 |
| `skip_monitoring` | bool | 跳过 ops 监控记录 |

校验约束（来自 `backend/internal/model/error_passthrough_rule.go`）：
`match_mode` 必填；至少一个 `error_codes` 或 `keywords`；
`passthrough_code=false` 时 `response_code` 必填；`passthrough_body=false` 时 `custom_message` 必填。

## 推荐配置（AI-gateway 生产）

目标：Kimi/CPA 的确定性 4xx 原样回客户端，5xx 保持现状（网关语义不泄露）。

| 字段 | 值 |
|---|---|
| name | `openai-4xx-passthrough` |
| enabled | `true` |
| priority | `10` |
| error_codes | `[400, 404, 409, 422]` |
| keywords | `[]` |
| match_mode | `any` |
| platforms | `["openai"]` |
| passthrough_code | `true` |
| response_code | `null` |
| passthrough_body | `true` |
| custom_message | `null` |
| skip_monitoring | `false`（保留 ops 记录，便于追踪） |

**为什么不透传 401/403**：上游凭证问题属于网关内部，不应暴露给客户端。
**为什么不透传 429**：sub2api 自己做 failover/退避，透传会破坏调度语义。

## 配置方法

### 方法 A：Admin 面板（推荐）

1. 打开 sub2api admin（生产：`https://<gateway-host>:8086`，登录管理员账号）
2. 进入 **错误透传 / Error Passthrough** 设置页
3. 新建规则，按上表填写，保存

### 方法 B：REST API（自动化/备份恢复）

```bash
# 登录拿 token
TOKEN=$(curl -s https://<gateway-host>:8086/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"<admin-email>","password":"<admin-password>"}' | jq -r .token)

# 创建规则
curl -X POST https://<gateway-host>:8086/api/v1/admin/error-passthrough-rules \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "openai-4xx-passthrough",
    "enabled": true,
    "priority": 10,
    "error_codes": [400, 404, 409, 422],
    "keywords": [],
    "match_mode": "any",
    "platforms": ["openai"],
    "passthrough_code": true,
    "response_code": null,
    "passthrough_body": true,
    "custom_message": null,
    "skip_monitoring": false,
    "description": "Passthrough deterministic 4xx from CPA/Kimi upstreams; keeps 5xx/429 gateway-mapped."
  }'
```

### 方法 C：直接写 DB（应急，绕过面板）

```bash
docker exec ai-gateway-postgres-1 psql -U sub2api -d sub2api -c "
INSERT INTO error_passthrough_rules
  (name, enabled, priority, error_codes, keywords, match_mode, platforms,
   passthrough_code, response_code, passthrough_body, custom_message,
   skip_monitoring, description, created_at, updated_at)
VALUES
  ('openai-4xx-passthrough', true, 10, '{400,404,409,422}', '{}', 'any', '{openai}',
   true, NULL, true, NULL,
   false, 'Passthrough deterministic 4xx from CPA/Kimi upstreams', NOW(), NOW());"
```

## 验证

配置前（生产 2026-09-16 实锤）：
```
客户端收到: upstream_error: Upstream request failed   ← 400 被吞
pi-retry 重试 10 次全部同样失败                        ← 无效重试
```

配置后预期：
```
客户端收到: invalid_request_error / 400 / "Invalid request: text content is empty"
```

验证步骤：
1. 触发一个确定性 400（如向 kimi-k2.8-code 发空 content 请求，或临时构造非法字段）
2. 客户端应看到真实上游 message，而非 `Upstream request failed`
3. `ops_error_logs` 表中 `upstream_error_message` 字段应有值
4. 重试不应再盲目放大（确定性 4xx 客户端应立即失败）

## 补丁退役检查清单

配置生效后，以下 fork 补丁可以退役（等上游 release 覆盖后换官方镜像）：

- [ ] `1c6661949` fix(openai): propagate terminal upstream errors
- [ ] `b35b45c` fix(openai): surface upstream 4xx diagnostics on WS HTTP bridge
- [ ] ~~`02218b52`~~ session_id 显式透传 —— **不在此列**，无规则替代，需等上游

退役验证：切回官方镜像后重跑验证步骤，行为一致即退役成功。

## 注意事项

- 规则**只影响 failover 耗尽后的兜底错误**（`handleFailoverExhausted` 路径），流式中途错误
  另有 `codexFailureTerminal` 机制，不受此配置控制。
- `skip_monitoring=false` 时错误仍进 `ops_error_logs`，不影响排障。
- 规则改动即时生效，无需重启。
- 生产 .env / compose 已开 `GATEWAY_LOG_UPSTREAM_ERROR_BODY=true`（8KB），与规则互补：
  规则管"客户端看到什么"，日志管"ops 记什么"。

## 关联文档

- 补丁谱系审计：见 `git cherry v0.2.4` 输出（当前 3 个 fork 补丁）
- CPA 升级记录：`docs/cpa-v7.2.153-deployment-2026-09-07.md`（同款格式，后续升 v7.3.4 补一篇）
- manifest-memory PR：[Wei-Shaw/sub2api#7215](https://github.com/Wei-Shaw/sub2api/pull/7215)（独立于本配置）
