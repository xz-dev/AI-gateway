# 本地来源配置整理与剩余数据缺口

> 历史证据：本页“裸名歧义”及XL Muse绑定是清空链provider的旧规则下结论。公共入口已修复，Muse同名绑定经等价验证删除、Meta链保留；当前结果见[两阶段修复](../docs/enricher-bare-chain-deployment-2026-09-08.md)。

> 以下是本地整理阶段的固定输入结果。后续已经授权部署；当前在线字段变化和人工review入口见[上线记录](../docs/enricher-provider-prefix-deployment-2026-09-08.md)，不能以本页统计代替实时目录。

2026-09-08。记录当时仅本地配置整理，未提交、部署或修改生产配置。目标是删掉重复配置、核对已批准来源及报告缺项，不是恢复旧配置形状、追平旧补全增量或填满所有字段。

## 已整理

`models-enricher/config.yaml` 从165行缩为51行：

- Commandcode的40条单模型配置（含39条 `lookup_ids`）替换为4条渠道级映射：`MiniMaxAI → minimax`、`Qwen → alibaba`、`z-ai → zai`、`zai-org → zai`。
- 无须改名的厂家由默认首斜杠拆分查询。没有恢复旧token后备规则，也没有增加来源类型或人工字段覆盖。
- XL Muse保留模型级来源链，并明确绑定 `models.dev/meta: muse-spark-1.3-contributor`，避免裸ID歧义。
- 其他渠道、原生端点、五条静态映射及权限边界配置未改。
- Qwen映射额外覆盖三个已有型号；用户单独批准保留这些补全。其余缺项只报告，不继续扩大配置范围。

配置SHA256：`e7d750149d47196cd2fa0d1977ba2481157e6d42cac11d796b61c44337140efe` → `88b8559a24393673e047ec19313f2ef1342ec8ed6149a4311d42715e8e76d44d`。

## 来源与完整影响验证

使用[前次报告](enricher-provider-prefix-replay.md)中的同一份189成员、API、flat、modelparams固定输入，四个SHA256均未变化。固定Go实现，比较整理前配置与候选配置；适配、查询、合并均运行真实Go代码。候选配置与最终写入文件逐字节相同。没有重新抓取生产数据。

- 原41项（Commandcode 40项、XL Muse 1项）均命中预期来源，来源身份及九项核心参数符合原始绑定。
- 其中40项的完整来源层与原始显式来源层相同。Nvidia自动协调包含flat的 `benchmarks`，API输出仍为65536；这是此前代码改动已有的差异，删除其模型配置没有再改变来源层。
- 恢复14项Commandcode及XL Muse的命中；另外新增3项Qwen命中，共18个完整目录记录变化。
- 额外六个受前缀分组核对的成员中：三个Qwen新增命中；Kimi-K2.5仍命中；`deepseek-v4-flash-fast` 和 `GLM-5.2-Fast` 仍未命中，没有删后缀猜型号。
- 44个模型的查询序列变化，18个模型的完整记录变化。完整字段比较覆盖27种顶层字段，包括名称、描述、价格、限制、模态和推理相关字段，不只比较输出限额；未发现字段删除。
- 189个公开成员及名称拼写保持不变。XL Muse的 `id` 从缺失恢复为公开slug；其他记录的 `id` 存在性及已有值未变。这不代表前次新旧代码比较中的其他兼容风险已获验收。

新增Qwen记录全部来自 `models.dev/alibaba`，查询ID仅按既定规则转小写，日期/版本后缀保留：

| Commandcode型号 | 来源id | 最大输出 | 来源输入类型 |
| --- | --- | ---: | --- |
| Qwen3.7-Flash | alibaba/qwen3.7-flash | 65536 | text、image、video |
| Qwen3.8-27B | alibaba/qwen3.8-27b | 32768 | text、image、video |
| Qwen3.8-Max-0902 | alibaba/qwen3.8-max-0902 | 131072 | text、image、video、pdf |

这些是厂家目录资料，不证明Commandcode实测支持这些能力。

## Commandcode与XL还缺什么

下表列出这两个渠道仍缺 `max_output_tokens` 的全部9个模型。输入限额等其他字段的缺失另见字段总表和逐模型附件。

| 渠道 | 模型 | 当前证据与原因 |
| --- | --- | --- |
| Commandcode | deepseek/deepseek-v4-flash-fast | 当前来源内没有准确ID命中 |
| Commandcode | meituan/LongCat-2.0:free | 当前来源内没有准确ID命中 |
| Commandcode | poolside/laguna-s-2.1-free | 当前来源内没有准确ID命中 |
| Commandcode | tencent/hy3-paid | 当前来源内没有准确ID命中 |
| Commandcode | zai-org/GLM-5.2-Fast | provider已映射到zai，准确型号仍未命中 |
| XL | gemini-3.8-flash-high | subscription与models.dev均无准确ID命中 |
| XL | grok-composer-2.5-fast | subscription与models.dev均无准确ID命中 |
| XL | hy4-preview | subscription唯一命中opencode-go，但记录不含九项核心元数据；models.dev有5个provider候选，不擅自挑选 |
| XL | minimax-m3 | subscription唯一命中minimax，但记录不含九项核心元数据；models.dev有19个provider候选，不擅自挑选 |

没有把 `fast`、`free`、`paid`、`high` 等后缀删掉再找相近型号。后两项是“命中记录却没有所需字段”与“另一个来源查询歧义”同时存在，不能把命中数当作补全数。

## 全目录缺项分类

以下仅描述保存输入重放后的字段存在性，不是要求补满的清单。

| 字段 | 存在 | 缺失 |
| --- | ---: | ---: |
| context_window | 188 | 1 |
| max_input_tokens | 23 | 166 |
| max_tokens | 46 | 143 |
| max_completion_tokens | 0 | 189 |
| max_output_tokens | 125 | 64 |
| input_modalities | 188 | 1 |
| output_modalities | 125 | 64 |
| supported_reasoning_levels | 188 | 1 |
| default_reasoning_level | 188 | 1 |

- 表内已存在字段没有null、空字符串或空数组。`context_window`、`max_input_tokens`各有1个零值；`max_output_tokens`有4个零值，均计为存在，不当成漏字段。
- 四个零输出值位于XL的 `gpt-image-2`、`grok-imagine-image-2.0`、`grok-imagine-image-quality`、`grok-imagine-video-1.5`；不擅自把0改成缺失或其他限额。
- 三个输出token字段独立；Zcode部分subscription记录只提供 `max_tokens`，不能抄成 `max_output_tokens` 或 `max_completion_tokens`。
- 九项均缺失的条目是静态 `grok-4.7`，其继承父项不在固定输入中。

缺少输出限额的64项按渠道分布：

| 渠道/条目 | 成员 | 缺输出限额 | 缺输入限额 |
| --- | ---: | ---: | ---: |
| Commandcode | 67 | 5 | 57 |
| XL | 22 | 4 | 16 |
| Axis | 9 | 2 | 2 |
| Congee | 7 | 7 | 7 |
| Zcode | 10 | 6 | 10 |
| Ollama Cloud | 19 | 1 | 19 |
| NIM | 3 | 0 | 3 |
| ShuaiAPI | 13 | 0 | 13 |
| Codex | 9 | 9 | 9 |
| Kimi Coding | 8 | 8 | 8 |
| SuperGrok | 16 | 16 | 16 |
| GMICloud | 1 | 1 | 1 |
| 五个静态条目 | 5 | 5 | 5 |

其中39项没有直接bulk查询：Codex 9、Kimi Coding 8、SuperGrok 16、GMICloud 1、静态条目5。按原配置透传或继承，不自动启用来源。固定目录确实包含 `gmicloud/MiniMaxAI/MiniMax-M3`；此前“GMICloud没有成员”的表述不准确。

其余渠道缺输出限额的具体项：

- Axis：`gpt-5.2-2025-12-11`、`gpt-5.4-2026-03-05`，准确ID未命中，不删除日期重查。
- Congee：`gpt-5.5`、`gpt-5.6`、`gpt-5.6-luna`、`gpt-5.6-sol`、`gpt-5.6-terra`、`gpt-6-astra` 存在裸ID歧义；`gpt-6` 未命中。
- Zcode：`glm-4.5-air`、`glm-4.6`、`glm-5`、`glm-5.1` 命中z-ai subscription记录，但只提供旧token字段；`glm-4.6v`、`glm-5v-turbo` 的models.dev查询歧义，subscription未命中。
- Ollama Cloud：`deepseek-v4-pro:0813` 在bulk来源未命中，原生输入也未采集，不能据此判断完整原生查询结果。

### 歧义和未命中不能由目录已有值掩盖

189项中：94项至少命中一条bulk记录；43项查询歧义且没有其他bulk命中；13项准确查询未命中；39项没有bulk查询。原生输入缺口与这些分类可重叠。

共有78条歧义查询，涉及77个模型，其中34个模型可从其他已配置来源命中记录。没有因歧义修改已批准规则，也不把普通未命中当作HTTP失败。

56项没有任何bulk命中，其中37项仍有目录自带的输出限额。这37项不能记作来源补全成功。逐模型附件保留实际查询、选中来源身份、候选及字段，便于区分。

## 仍未完成的验证

- 19个Ollama Cloud成员的原生列表/show等输入没有保存在这份bulk fixture中；18项已有输出限额不证明原生调用链已验证。
- 静态 `grok-4.7` 的 `supergrok/grok-4.7` 父记录缺失。保留真实警告，没有手填父项或参数。
- 自身渠道数据沿用已保存目录，未重新抓取。未验证生产新鲜度或实际推理能力，也未处理其他渠道剩余来源选择。
- OpenSpec 3.2保持未完成。当前配置的44项目标检查通过，不等于完整数据链验收完成。

## 有界测试与证据

在 `models-enricher` 中执行：

```sh
CATALOG_SOURCE_COMPLETION_FIXTURE="/root/.cache/catalog-explicit-bindings-20260908/fixture" \
PROVIDER_PREFIX_REPLAY_OUTPUT="/root/.cache/provider-config-cleanup.QsWnZP/replay-after.json" \
go test -race -json -run '^(TestConfiguredSourcesAndFiveStaticModels|TestCapturedSourceCompletion|TestProviderPrefix.+)$' -count=1 .
go vet ./...
gofmt -l configured_catalog_test.go
git diff --check
```

11个顶层测试、含子测试37个通过事件，0失败、0跳过；vet、格式、diff检查均通过。配置写入后，旧测试曾因配置形状及新增Qwen范围失败，日志保留。之后仅修订冲突断言：

- 不再要求Commandcode保留40个模型块；检查4条批准映射和XL明确绑定。
- 原41项输出数值逐字保留；增加3项批准的Qwen值，并逐项验证实际来源身份、来源字段和合并结果。
- 优先级检查使用实际最终token查询，不继续模拟旧token查找。
- 删除“全部目标必须本轮新增输出字段”的旧增量假设；全部44项最终值、独立token字段和范围外完整记录不变的检查仍保留。

候选独立Go来源层审计与整理前重放另各运行一次，不混入上述race计数。未重跑无关SSR或全项目全量测试。4.1有界回归完成，3.2的输入及数据链缺口单列保留。

证据目录：`/root/.cache/provider-config-cleanup.QsWnZP/`。

- `config.before.yaml`、`source.before/`：编辑前配置与源码副本。
- `candidate-audit.json`：原41项完整来源层核对及额外六成员的实际查询。
- `replay-before.json`、`replay-after.json`：同一Go实现的配置前后查询与九项字段；完整目录另存受限 `.manifest.json`。
- `gap-inventory.json`：全部189项逐模型九字段缺项、存在值、来源状态、查询身份、歧义候选及原生输入缺口。
- `full-field-changes.json`：全部变化字段路径及id存在性，不只覆盖九项字段。
- `config-tests-before-update.log`、`race-config.jsonl`、`vet.log`、`format.log`、`diff-check.log`：真实失败及后续验证。

本地整理结束时生产仍是旧版本；随后已另行授权并部署，见页首上线记录。这份配置依赖新provider-prefix实现，不能把配置单独交给旧边车。后续补全及数据差异验收没有因部署自动获批。
