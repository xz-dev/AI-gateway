# models-table：统一后台刷新

## 行为与配置

`models-enricher` 的启动、定时、进入页面、手动按钮共用一个全链刷新任务。采集覆盖 CPA/AISIX 原始目录、启用的渠道目录和元数据源，并绕过输入缓存新鲜期。已有任务运行时只加入，不再采集一轮。

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

测试位于 `services/routing/model-catalog/scheduled_refresh_test.go`，复用现有 `httptest` 假 CPA/AISIX/元数据源，不访问生产。覆盖：冷页不等上游、窗口内不触发、超窗返回旧数据、进入/手动/并发加入单任务、启动/定时、取消/停机、成功时间与独立时长配置。原 SSR、手动刷新、持久输入缓存测试改为显式预热/等待，不再假设 GET 同步采集。

变更前，`TestModelsTableColdEntryDoesNotWaitForCollection` 因 `cold GET waited for the blocked upstream` 失败；变更后通过。本地同一假元数据源等待 200ms 的单样本如下，非生产延迟或分位数：

| 阶段 | 冷 GET | 紧接的手动 POST | 热 GET |
|---|---:|---:|---:|
| 旧实现 | 205.25ms | 3.58ms | 57.34µs |
| 新实现 | 98.08µs | 202.84ms | 69.13µs |

新实现将等待移到明确提交的 POST，而不是从系统中消除上游耗时。

复现（工作目录仓库根，临时目录在 `/var/tmp`，不继承真实 CPA key）：

```sh
(cd models-enricher && TMPDIR=/var/tmp env -u CPA_API_KEY go test ./...)
(cd models-enricher && TMPDIR=/var/tmp env -u CPA_API_KEY go test -race ./...)
(cd models-enricher && TMPDIR=/var/tmp env -u CPA_API_KEY go vet ./...)
(cd models-enricher && TMPDIR=/var/tmp env -u CPA_API_KEY go test -count=1 -run TestModelsTableFakeUpstreamLatency -v .)
TMPDIR=/var/tmp env -u CPA_API_KEY ./ops/scripts/validate.sh .env.example
openspec validate --all --strict
```

## 部署交接：未执行

此改动只提交源码和文档；没有发布 models-enricher 镜像、访问生产或部署。以下步骤交给中控，在明确授权及单一生产写入 lease 下执行；不重启 VM，也不动 keeper、bridge、Sub2API 或整个上游栈。

1. 确认最终源码提交和其 Validate CI 成功。镜像由 `.github/workflows/publish-models-enricher.yml` 手动触发构建，服务名 `models-enricher`，仓库 `ghcr.io/xz-dev/ai-gateway-models-enricher`，构建上下文 `./services/routing/model-catalog`。例如在最终提交位于 `main` 后：

   ```sh
   version="scheduled-refresh-$(git rev-parse --short=12 HEAD)"
   gh workflow run publish-models-enricher.yml -R xz-dev/AI-gateway \
     --ref main -f version="$version"
   ```

   使用全新不可变版本名；核对运行的完整 `headSha` 等于批准源码提交，等待构建成功并读取镜像 digest。不要用其他 main tip 的构建代替，也不要仅凭标签断言镜像来源。

2. fresh-read 生产配置、容器/镜像/netns/健康状态，与本地 gitignored `ops/ansible/private-config` 核对。远端备份目录 `0700`、文件 `0600`；保留旧镜像 pin、本地回滚收据和 CAS 基线。不需要远端 Git checkout。

3. 把经核验的不可变镜像 pin 写入 private-config 中 `MODELS_ENRICHER_IMAGE` 的实际配置源，不覆盖其他环境项。默认时长无需修改生产 YAML。若需自定义，在镜像升级后使用既有 `deploy-models-enricher-config` 流程，保留渠道、static_models 和私密凭证：

   ```sh
   cd ansible
   ansible-playbook ops.yml --tags deploy -e service=models-enricher -e ai_ops_mode=plan
   # 单独批准 plan 后才 apply：
   ansible-playbook ops.yml --tags deploy -e service=models-enricher
   # 可选：经审查的 YAML 配置另行 plan/apply
   ansible-playbook ops.yml --tags deploy-models-enricher-config -e ai_ops_mode=plan
   ```

   先按现行 Ansible 文档复核 profile 的 pull/recreate 行为。仅重建 `models-enricher`，不能用通用 up 重建依赖。配置部署要求目标二进制支持 `validate-config`；不要在旧镜像上宣称新键已生效。

4. 读回实际镜像 ID/digest、环境、`/readyz`/容器健康及配置 hash，再同步回 private-config。`/healthz` 只代表进程存活，不能替代目录采集验收。通过 localhost/Tailscale `:9083/models-table` 验证：冷页即时返回、时间戳、超窗异步、手动等待/加入、定时更新；公开标准/Codex 目录仍按原鉴权和准入规则返回。现场应另外量一次公开目录延迟，不能引用本地 µs 数作为生产结果。

5. 错误时按已审查的单服务回滚方案恢复旧 pin/受保护配置并读回。保留命名卷及 bbolt 输入缓存；不做破坏性清理、守护部署或 VM 重启演练。
