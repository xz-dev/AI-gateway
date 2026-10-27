# 目录二次正向准入：本地实现与验收

日期：2026-09-08。本记录描述本地实现与当时验收，覆盖前次“未知身份默认保留”的正常查询策略。**后续已获单独授权并部署**，确切镜像、补充离线链路及生产结果见[部署记录](catalog-positive-admission-deployment-2026-09-08.md)。

## 当前规则

1. 先处理管理数据列出的原名/裸别名，再校验剩余候选去一次前缀后是否能精确对应管理模型。
2. key渠道使用管理端实际prefix和有效原名（alias非空优先，否则name）。例如`hub/nvidia/model`对应hub渠道的`nvidia/model`，不能去全局表撞名或再次剥掉厂商命名空间。同一ID也有其他已声明路由时保留。
3. OAuth先移除原始定义及直接别名列出的原名，包括有斜杠的原名。候选完整ID及其去掉非空前导前缀后的原名，必须同时属于同一账号注册集，原名还须由定义或直接别名声明；满足精确配对才保留。不同账号的同名、静态定义差集、递归别名链不能放行。
4. **管理数据成功读取但无对应，移除**，包括未知动态原名、没有管理模型匹配的前缀名。静态定义不保证覆盖所有动态模型；这不再是默认放行理由。
5. **读取失败或发现不完整不是查无记录**：有合格缓存先用缓存；无可用缓存则保留受影响的现有CPA原记录并告警，不新增未知候选。原有5分钟fresh、503等合格stale、401/403/坏响应不复用旧身份的规则不变。
6. 已匹配OAuth仍只透传CPA字段；故障保留记录同样保留原始id、null、false、0、空值和精确数字。五个显式static独立按原规则物化，缺父记录仍告警、不猜替代。
7. 管理列表不直接新增公开模型。五类受管成员仍要求CPA公开输入；Gemini/Interactions保留原候选来源，但富化不能重新加入被正向准入拒绝的项。

只读取既有管理元数据、账号模型、模型定义和别名端点；不下载OAuth鉴权文件，不生成持久OAuth前缀配置。模型名称配对不等于推理可用性证明。没有新增接口、依赖、服务、最终目录缓存或生产变更；调用、force-model-prefix、Rust、egress和授权不变。

## 验证覆盖

| 规则 | 测试 |
| --- | --- |
| 仅精确渠道匹配保留；未知、跨前缀及成功空模型集移除 | `TestCatalogRetainsOnlyManagementMatches` |
| 同账号OAuth原名/完整注册ID配对，跨账号同名拒绝，字段透传 | `TestCatalogOAuthRetentionRequiresSameAccountPair` |
| Gemini/Interactions旧候选路径不能重新加入拒绝项 | `TestCatalogRetentionCannotBeUndoneByLegacyInventory` |
| 厂商命名空间与重复剥前缀 | `TestCatalogIdentityVendorNamespace`、`TestCatalogIdentityKeepsRepeatedNamespace` |
| 直接别名，不从别名链放行未知模型 | `TestCatalogIdentityOAuthRequiresPositiveMatch` |
| 同名有效路由保留，管理声明不复活未公开项 | `TestCatalogIdentityAliasCollisionAndPublicMembership` |
| 正常无匹配与冷读取故障不同，故障记录精确保留 | `TestCatalogIdentityFailurePreservesOriginal` |
| fresh、长期503 stale、401/403/坏响应及失效缓存 | `TestCatalogIdentityReadCacheAndFailure` |
| 发现不完整及缓存命中继续保留故障信号 | `TestCatalogIdentityIncompleteDiscoveryPreservesBaseline` |
| 五个static及继承关系 | `TestConfiguredSourcesAndFiveStaticModels` |

新增HTTP测试仅访问本地`httptest` CPA替身；旧handler/大字段测试补齐显式OAuth管理注册与定义夹具，没有用生产请求替代测试，也未放宽并发、元数据大小、富化值和缓存断言。

三个新增准入回归先在旧代码上失败：未知项仍放行、OAuth原名/跨账号项仍放行、旧库存重新引入拒绝项；修改后通过。证据目录：`/root/.cache/enricher-positive-retain.WrKrtX/`，包含red/green、两轮完整回归、最终检查及修改前快照。旧排除式阶段证据仍在`/root/.cache/enricher-identity-local.7FOmjR/`，不作为当前策略通过记录。

最终检查（最新代码）：

```sh
cd models-enricher
go test -run 'TestCatalogRetains|TestCatalogOAuthRetention|TestCatalogRetentionCannot|TestCatalogIdentity|TestConfiguredSourcesAndFiveStaticModels' -count=1 .
go test -race -json -count=1 ./...
go vet ./...
```

均通过。race包耗时3.599秒，125个测试/子测试通过事件；1项跳过：`TestCatalogHTTPFixture`需要额外APISIX夹具，本轮未运行。gofmt及当前OpenSpec change严格校验通过。

以上本地阶段不声称APISIX整链路、生产资源、真实OAuth接口集成、持续负载或推理已经验收。该阶段没有构建/部署镜像、修改运行配置或Git提交。随后单独授权的发布已完成补充离线链路及生产目录验收，见顶部部署记录；仍不声称持续负载或推理通过。
