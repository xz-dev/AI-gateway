# models-table：统一后台刷新

## 行为与配置

`model-catalog` 的启动、定时、进入页面、手动按钮共用一个全链刷新任务。采集覆盖 CPA/AISIX 原始目录、启用的渠道目录和元数据源，并绕过输入缓存新鲜期。已有任务运行时只加入，不再采集一轮。

| 触发 | 行为 |
|---|---|
| 启动 | 后台运行一次，HTTP 服务不等采集完成 |
| 定时 | 按 `scheduled_refresh_interval` 触发，默认 `30m` |
| `GET /models-table` | 在 `entry_refresh_window`（默认 `10m`）内不触发；超过窗口或尚无成功刷新时后台触发。立即返回最近发布的目录；冷启动立即显示加载中/暂无数据 |
| `POST /models-table/refresh` | 启动或加入同一任务，等待后返回 SSR 结果；客户端取消只结束其等待 |
| 停机 | 停止 ticker，取消并等待活动任务，再关闭缓存 |

两个 YAML 时长独立，只要求正值。允许进入窗口 `2h`、定时 `1m`；显式 `0s`、负值和非法时长会拒绝加载。省略字段使用默认值。示例位于 `services/routing/model-catalog/config.yaml`：

```yaml
scheduled_refresh_interval: 30m
entry_refresh_window: 10m
```

页面显示最后成功刷新完成时间与下一次定时触发时间，均为 UTC。完成的 HTTP 200 结果若使用了旧输入响应，或配置的 AISIX 补充采集失败，不推进成功窗口；失败尝试也不推进。原有管线对单渠道缺少可用元数据的容错规则不变。成功窗口不等于最后访问时间，也不由手动失败重置；定时节奏不因访问或手动刷新顺延。

后台失败保留最近发布的诊断表格并提示失败时间；它不是 JSON 目录的准入/路由权威。手动刷新无可用 CPA 权威时仍返回错误页面，而不把旧表格冒充成功。元数据有最后成功输入时，按当前 CPA 成员重新合成并保留旧输入警告。

`/v1/models` 标准投影和 Codex 目录仍使用同一原始快照 owner，保留原有版本键、五分钟读缓存/投影缓存、成员准入和请求路径行为。既有原始路由快照定时器不变。后台任务预热启用的输入，但不预构建所有客户端版本的投影；冷版本请求、授权交集等仍可能耗时。没有声称公开 `/models` 已加速。

## 本地验证

测试位于 `services/routing/model-catalog/tests/server.test.ts`（冷页加载、后台刷新、POST 结果与并发合并、无 CPA 权威时的错误页、后台失败保留旧表、启动/定时/停机）和 `tests/config.test.ts`（两个时长的校验），使用 `tests/fake-cpa.ts` 假上游，不访问生产。

复现（工作目录仓库根，临时目录在 `/var/tmp`，不继承真实 CPA key）：

```sh
(cd services/routing/model-catalog && TMPDIR=/var/tmp env -u CPA_API_KEY npm test)
TMPDIR=/var/tmp env -u CPA_API_KEY ./ops/scripts/validate.sh .env.example
openspec validate --all --strict
```

Go 实现时期的冷 GET / 手动 POST 单样本测量和当时的部署交接属于历史记录，不代表当前 TypeScript 实现。

## 部署

镜像由 `.github/workflows/publish-model-catalog.yml` 手动触发构建（`ghcr.io/xz-dev/ai-gateway-model-catalog`，构建上下文 `./services/routing/model-catalog`），按 digest 写入 private-config 的 `MODEL_CATALOG_IMAGE`。镜像升级走通用组件流程（`--tags deploy -e service=model-catalog`），配置改动走 `deploy-model-catalog-config`，两者都先 plan 再 apply，见 [Ansible operations](ansible-operations.md#model-catalog-config-safety)。部署后通过 localhost/Tailscale `:9083/models-table` 验证冷页即时返回、时间戳、超窗异步、手动等待/加入和定时更新；`/healthz` 只代表进程存活，不能替代目录采集验收。
