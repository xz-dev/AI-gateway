# Provider-prefix：固定来源差异与待决问题

> 历史证据：用户后来纠正了裸名清空链provider的语义；本页旧进度、跨provider歧义和绑定建议不再表示当前合同。新规则及两阶段上线结果见[裸名修复报告](../docs/enricher-bare-chain-deployment-2026-09-08.md)。

> 本页保存配置整理前的历史重放结果。后续用户已授权本地配置整理，并批准新增3项Qwen补全；当前44项字段检查及有界回归通过，完整数据链仍未验收。最新状态见[配置整理与缺项报告](enricher-source-config-cleanup.md)。

配置整理前的历史状态：用户在差异披露后明确选择**保留已修订语义与最终token作用域**；这不表示所有差异已验收。1.1–2.4、3.1、4.2 已完成（8/10）。3.2保留数据验收缺口，4.1因原配置补全验收仍失败而不标全绿。以下是有限实现收尾与离线证据，不是配置迁移或生产验收。

## 输入与方法

- 同一份本地 `models-enricher/config.yaml`，修改前后逐字节一致；SHA256 `e7d750149d47196cd2fa0d1977ba2481157e6d42cac11d796b61c44337140efe`。
- 固定 189 成员，9 个配置渠道；没有重新获取生产数据。
- 使用修改前源码快照的真实 `mergeManifest`、索引适配器、`lookupOne`，与新源码在相同输入上分别重放；不是 Python 模拟查找。Python 仅比较 Go 生成的 JSON。
- 两侧均以保存目录作为已有元数据，重放 bulk 来源。不把目录已有值当成新查询仍生效的证明。
- 另在独立源码副本内，仅于内存中删除单条模型配置做反事实比较；比较来源层本身，不借用目录已有字段。不写实际配置，不把实验算作迁移。

固定输入目录：`/root/.cache/catalog-explicit-bindings-20260908/fixture/`。

| 文件 | SHA256 |
| --- | --- |
| catalog.json | f6b10cf8f62f5b2c786a44639ee9cefa3b65e407c8a7b92f35dbc5670a1960f0 |
| api.json | 3335e4ab406bcb3fb8519d81ae53c8934c750a3359f75139795049bd7ced0612 |
| flat.json | e52b30487a8e48c6c582c3a3df8cf7608f2cf4f9cd1c0786221863df746c1f37 |
| params.json | 10d5494c3eb4ebbe1cbb87b0375c35ad8f1ae627c5742dde72f70d6ff847d9aa |

## 查询变化与字段变化分开统计

- 成员集合仍为 189，但 **125 个模型的查询序列改变**。
- **14 条已有显式绑定未被采用**；它们原先全部命中，现在全部失去 bulk 命中。
- **60 个模型失去全部原有 bulk 命中**，6 个模型从无命中变为有命中。
- 79 条查询歧义，涉及 78 个模型。这不是 HTTP 失败。
- **42 个模型的完整输出记录变化**，涉及 35 种顶层字段；其中 **36 个模型的九项核心元数据变化**。
- 九项核心字段：context_window、max_input_tokens、max_tokens、max_completion_tokens、max_output_tokens、input_modalities、output_modalities、supported_reasoning_levels、default_reasoning_level。
- `max_tokens`、`max_completion_tokens` 没有新旧差异；不能由此概括其他字段没有变化。

| 渠道 | 成员 | 查询变化 | 九项核心字段变化 | 原有全部 bulk 命中丢失 |
| --- | ---: | ---: | ---: | ---: |
| axis | 9 | 9 | 0 | 2 |
| commandcode | 67 | 42 | 18 | 29 |
| congee | 7 | 7 | 6 | 6 |
| nim | 3 | 3 | 0 | 2 |
| ollama-cloud | 19 | 19 | 2 | 11 |
| shuaiapi | 13 | 13 | 0 | 2 |
| xl | 22 | 22 | 3 | 6 |
| zcode | 10 | 10 | 7 | 2 |

其余成员没有本次 bulk 查询变化。更正：固定目录包含1个 `gmicloud/MiniMaxAI/MiniMax-M3` 成员；`gmicloud: {}` 未启用bulk来源，不能把无补全查询写成没有成员。

## 14 条显式绑定为何失效

当前顺序先生成最终 provider token，再读取该 token 的 `lookup_ids`。因此旧 token 的显式 ID 不会复制到新 provider。这符合现有“最终 token 作用域”解释，却使既有补全失效。用户已选择保留此语义；不改成旧绑定优先，也不迁移配置掩盖差异。

| 上游前缀 | 旧绑定来源 | 新自动来源 | 条数 |
| --- | --- | --- | ---: |
| MiniMaxAI | models.dev/minimax | models.dev/minimaxai | 3 |
| Qwen | models.dev/alibaba | models.dev/qwen | 6 |
| z-ai | models.dev/zai | models.dev/z-ai | 1 |
| zai-org | models.dev/zai | models.dev/zai-org | 4 |

明确例子：

- `commandcode/MiniMaxAI/MiniMax-M3`：旧显式查询 `models.dev/minimax → MiniMax-M3` 命中；新查询 `models.dev/minimaxai → minimax-m3` 未命中。context_window 从旧重放的 1048576 回到输入已有的 1000000，max_output_tokens 从 512000 变为缺失。
- `commandcode/Qwen/Qwen3.6-Plus`：context_window 从 1000000 回到 200000；max_output_tokens 从 65536 变为缺失。
- `xl/muse-spark-1.3-contributor` 不是上述14条之一：原模型级 meta 链没有 lookup ID，新裸 ID 查询歧义；context_window 从 1048576 回到 272000，max_output_tokens 从 131072 变为缺失。

## 来源选择与优先级变化

- Nvidia：API 的 max_output_tokens=65536 保持优先于 flat 的128000；但新适配补入了 flat 的 `benchmarks`，因此不是完整记录零变化。
- `nim/moonshotai/kimi-k3`：从 `models.dev/nvidia` 的 `moonshotai/kimi-k3` 转到 `models.dev/moonshotai` 的 `kimi-k3`；九项核心字段未变，完整记录变化。不能以字段相同证明仍采用原来源。
- `zcode/glm-5.3`：原 models.dev/zai-coding-plan、models.dev/zai 命中；新 models.dev 裸查询歧义，modelparams subscription 唯一命中 `opencode-go`。九项核心字段未变，但来源及其他字段改变；这是批准规则的结果，不据“唯一”额外推定适合 Zcode 渠道。
- `congee/gpt-5.6`：裸 ID 歧义，context_window 从1050000回到272000，max_input_tokens=922000、max_output_tokens=128000 不再补入。
- 新命中6项：Commandcode 的 moonshotai/Kimi-K2.5、tencent/hy4-preview、thinkingmachines/inkling、thinkingmachines/inkling-small；XL 的 minimax-m3、omen-alpha。命中本身不是型号/渠道能力核验。

## 配置简化的证据边界

内存反事实比较发现 **25 条 Commandcode 模型配置**可以作为删除候选：删除单条后，自动查得的完整来源层与旧显式查得的来源层相等。分组为 deepseek 3、google 6、meta 5、moonshotai 4、sakana 1、stepfun 2、xai 2、xiaomi 2。实际文件没有删除。

其他情况不能混入25条：

- 上述14条：若后续要求恢复已核对的 provider 对应关系，需要另行批准映射/绑定迁移；仅保留旧 lookup_ids 文本并不能使其生效。本轮不再调整已确认的显式优先语义。
- Nvidia：核心字段等价但完整来源层不等价，新增 benchmarks；单列核对，不计入完整等价候选。
- XL Muse：裸 ID 多命中，不能删除来源约束；需要后续决定明确查询如何表达。

旧39条前缀分组之外还覆盖以下6个既有 Commandcode 成员；它们与“新命中6项”不是同一清单：

- Qwen/Qwen3.7-Flash
- Qwen/Qwen3.8-27B
- Qwen/Qwen3.8-Max-0902
- deepseek/deepseek-v4-flash-fast
- moonshotai/Kimi-K2.5
- zai-org/GLM-5.2-Fast

日期、Fast 等变体保持字面内容，不通过扩搜或删后缀消除 miss。

## 未封闭的边界

- 只有 bulk 输入，没有 Ollama 原生 show 等来源响应，因此 ollama-cloud 的两项字段变化不能当成完整原生来源链结果。
- 两次重放都报告缺少 static 父项 `supergrok/grok-4.7`；没有手填父项或据此声称完成该继承链验收。
- slug 集合不变，两侧都存在的 id 值没有改名；但 **9项 id 字段出现/缺失状态改变**：Congee六项、XL hy4-preview与Muse失去id，XL omen-alpha新增id。根因是原有 `mergeManifest`：先接收来源字段，最后仅在对象已有id时将其改写为原slug；这段身份构造没有修改。既有规格要求拼写、构造来源及准入不变，未要求所有记录始终有id；因此3.1按这些明确边界验证通过，不据此声称响应字段存在性完全兼容，也不擅自增加/删除id规则。
- 限定race共执行75个顶层测试：首轮69个中66通过、3失败；仅修订并复验两个过时断言，均通过；补跑6个直接相关边界测试，均通过。最终74个代码/边界测试通过，1个真实配置补全验收仍失败，没有跳过。数据测试接入与fetch相同的API/flat协调入口后又单独复验，仍失败；不是修改后一整组重跑全绿。
- `go vet ./...`、9个本轮Go文件的gofmt检查、`git diff --check` 均通过。
- 未提交、部署、迁移配置或删除绑定。没有访问生产来源。

## 可重跑证据

证据根：`/root/.cache/provider-prefix-apply.1fg83O/`。

- `source.before/`：修改前源码及独立基线采集测试。
- `replay-old.json`、`replay-new.json`：脱敏查询/九项核心字段。
- `replay-old.json.manifest.json`、`replay-new.json.manifest.json`：受限本地完整结果，不直接输出到报告。
- `replay-diff.json`：逐模型查询、字段前后值及歧义候选。
- `full-field-change-keys.json`：全字段差异的字段名清单。
- `binding-audit.json`：逐条删除反事实与额外6个成员。

已执行命令（分别在旧源码快照与当前 models-enricher 目录）：

```sh
CATALOG_SOURCE_COMPLETION_FIXTURE=/root/.cache/catalog-explicit-bindings-20260908/fixture \
PROVIDER_PREFIX_REPLAY_OUTPUT=<证据输出路径> \
go test -run '^TestProviderPrefixLegacyCapture$' -count=1 .

CATALOG_SOURCE_COMPLETION_FIXTURE=/root/.cache/catalog-explicit-bindings-20260908/fixture \
PROVIDER_PREFIX_REPLAY_OUTPUT=<证据输出路径> \
go test -run '^TestProviderPrefixCapturedReplay$' -count=1 .
```

两侧均成功；成功仅证明采集与已列断言，不意味着新旧数据等价或产品验收通过。

## 限定回归记录

证据根内：

- `regression-tests.txt`：首轮69个顶层测试的逐项清单（保留当时的旧测试名）。
- `race-initial.jsonl`：`go test -race -json -run "^(<清单名称以|连接>)$" -count=1 .`；设置了上述真实fixture环境变量，退出1。
- 两个过时断言：`TestIndexModelparamsAuthTypeSplitAndProviderRequired` 要求丢弃无provider记录；`TestProviderFixtureBatchEnrichment` 期待旧token继续选定来源。分别更新为 `TestIndexModelparamsAuthTypeSplitAndOptionalProvider`、`TestProviderFixtureFinalTokenScope`，验证新规范的正反行为；未更改fixture数据或真实配置。
- `race-recheck.jsonl`：`go test -race -json -run '^(TestIndexModelparamsAuthTypeSplitAndOptionalProvider|TestProviderFixtureFinalTokenScope)$' -count=1 .`，退出0。
- `race-boundary-tests.txt`、`race-boundaries.jsonl`：新增执行的6个现有测试，覆盖共享来源失败、可用旧缓存、并行元数据隔离、单模型Ollama失败、403不可用旧缓存掩盖、native字段保留；同样使用清单生成精确正则，退出0。
- `vet.log`、`format.log`：最终vet和格式检查输出均为空、退出0。
- `regression-current-tests.txt`：包含重命名及边界补跑后的75项当前测试清单，供复现完整选择；本轮未再整组重跑，已知其中旧数据门槛仍失败。
- `openspec-validation.log`：`openspec validate add-enricher-provider-prefix-map --strict` 通过；apply CLI报告8/10，3.2和4.1保持未勾选。

**保留失败：** `TestCapturedSourceCompletion` 仍按以前的数据补全目标要求41项字段落地。首轮报告 `commandcode/Qwen/Qwen3.7-Plus`；仅将源表准备接入真实fetch所用的 `setModelsDev`（并对齐三行gofmt空白）后，单独race复验报告 `commandcode/zai-org/GLM-5` 输出字段未应用；原期望值与断言完全保留。失败模型的首报次序受原测试map遍历影响，不代表只有该一项失败。没有靠提供另一份配置制造通过。它阻止数据验收及整体回归门槛收束，不触发恢复旧查询语义。复验命令为 `go test -race -json -run '^TestCapturedSourceCompletion$' -count=1 .`，设置同一fixture环境变量，退出1；记录在 `race-data-gate.jsonl`。

没有重跑旧版本部署、无关SSR全套、推理压力测试或生产请求。源码比对确认 CPA/准入、原生请求、缓存/并发、SSR代码及来源配置、依赖文件均与apply前一致。简短使用与回退说明见 `docs/enricher-provider-prefix.md`。
