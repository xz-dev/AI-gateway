# models-table：189行历史逐项审查

> 本页及原CSV保留2026-09-08T14:41:57Z的观察，不是当前运行规则。用户已纠正裸名清空链provider的错误；76条同名绑定建议撤回，改为公共入口修复，现已部署。当前187项逐行结果和两阶段证据见[新review](../enricher-bare-chain-review-2026-09-08/README.md)。

观察输入：用户贴出的 `189 models · client_version=1 · 2026-09-08T14:41:57Z` 表格。只生成审查文件，未改服务、运行代码或来源配置。

## 文件

- [逐行审查CSV](models-table-review.csv)：原11列，加缺失字段、补全必要性、批量/单项方式、实际查询证据、候选声明、已有值差异及验证边界。每个模型一行，可按“分类”筛选。
- [原表CSV](models-table.csv)：逐格转录用户表格，189行、11列；保留“未知”、JSON数组与数值0，不替换为空字符串。
- [准确来源候选CSV](source-candidates.csv)：来源token、精确lookup_id与实际Go适配后的字段。候选不是已选择或已应用的补全方案。
- [统计及证据索引](summary.json)：输入消息标识/哈希、来源fixture哈希、字段缺失数、类别、CSV哈希及本地复现工具路径。

CSV使用UTF-8 BOM，方便Excel打开；已验证写入/读回与转录内容一致。

## 结论：不是全部都要补，也不是全部都要人工逐个填写

| 分类 | 行数 | 建议 |
| --- | ---: | --- |
| 主要字段已有值 | 60 | 不为了消灭“未知”补齐所有token列；另查已有值一致性 |
| 原来源有准确记录，旧版裸名查询错误 | 76 | 修复公共入口，直接沿用渠道链；不生成重复绑定 |
| NIM查询丢了来源/厂商路径 | 2 | 建议补；渠道provider映射加逐模型完整ID绑定 |
| 仅其他来源有候选 | 3 | 单独决定是否引用，不能自动挑一家 |
| 准确型号没有匹配记录 | 9 | 暂留未知；需要型号/供应方证据，盲加绑定无效 |
| 原生透传或其静态继承 | 37 | 保持既有策略，不默认启用外部补全 |
| GMICloud自有目录获取失败 | 1 | 先排查获取链，不是补字段配置问题 |
| 静态Grok父项缺失 | 1 | 先处理继承关系，不手填整行能力 |
| **合计** | **189** | |

这里的“已有值”不等于渠道实测正确；“建议补”指目录参考资料有可追溯声明，不把厂家或其他服务商资料当作本渠道能力证明。

## 为什么上线后反而空得更多

只读复核确认线上运行 `localhost/models-enricher:provider-prefix-20260908T134822Z`。容器内二进制SHA256为 `ee69a402a791…`，挂载配置为 `88b8559a2439…`，与部署制品一致。完整值见[部署记录](../../docs/enricher-provider-prefix-deployment-2026-09-08.md)。不是只改了仓库、没有部署。

当时的错误版本去掉渠道前缀后又清空了 `source_priority` token中的provider。比如 `shuaiapi/claude-opus-4-7` 被错误地查到所有provider范围，固定来源出现17个候选，导致不补。用户确认来源链本已明确指定Anthropic；现在修正为只查询该provider，而不是要求额外绑定。

**本次找到的78条“原配置来源有声明”的模型集合，恰好等于部署前后失去输出字段的78条集合。** 对这些模型，固定来源下的原查询没有输出字段；在内存里加入所列显式绑定（NIM另加渠道映射）后，真实 `sourceQueries → lookupQuery` 均可到达声明字段。这个结果定位了可处理的查询/配置缺口；仍不是78条线上最终合并已验收的证明。

部署时全目录 `max_output_tokens` 存在数为 **89→60**：78条失去、49条新增；另有29条 `id` 存在性变化。此前44项目标数值通过，只覆盖那44项，不能代表全目录数据完成。固定目录本来包含旧补全值，不能用它的“缺64项”代替本表的“缺129项”。

## 76条沿用已有渠道链，不再增加逐模型绑定

| 渠道 | 条数 |
| --- | ---: |
| Axis | 7 |
| Commandcode | 15 |
| Congee | 6 |
| Ollama Cloud | 11 |
| ShuaiAPI | 13 |
| XL | 14 |
| Zcode | 10 |

此前提出的批量生成 `lookup_ids` 会重复声明链里已有的provider，因此撤回。修复后直接使用既有来源链，无须通配规则、76条重复绑定或手写能力常量。`provider_prefix_map` 仍只处理名称中真实存在的斜杠前缀，不反向改写裸名的链provider。

```yaml
shuaiapi:
  source_priority: [models.dev/anthropic, modelparams.dev/anthropic/subscription]
```

这组候选沿用原来配置过的来源token，没有为填空自动选择新来源。其中26条来源还明确声明 `max_input_tokens`，可一起审查；不靠context减去output来计算。

需要注意：78条候选中有76条与当前已有字段也存在差异，包括上下文、输入模态或推理列表。CSV逐行列出了差异。**加来源绑定不一定只增加空字段，也可能按现有优先级覆盖已有值。** 当前列出的差异是候选记录与现值比较，不是完整来源链合并预测；应用前还需做拟议配置的完整记录对照。

## NIM两条：不能只加一条provider映射

| 当前模型 | 沿用的来源 | 精确lookup_id | 来源输出声明 |
| --- | --- | --- | ---: |
| `nim/deepseek-ai/deepseek-v4-flash-0731` | `models.dev/nvidia` | `deepseek-ai/deepseek-v4-flash-0731` | 384000 |
| `nim/minimaxai/minimax-m3` | `models.dev/nvidia` | `minimaxai/minimax-m3` | 16384 |

保留现有NVIDIA来源时，需要渠道级 `deepseek-ai→nvidia`、`minimaxai→nvidia`，再显式保留上表完整查询ID。仅映射provider，自动查询仍只拿末尾短ID，无法命中NVIDIA保存的厂商路径。

不能见到别处有数就替换：同一MiniMax-M3，NVIDIA来源这里声明16384，而MiniMax厂家目录声明512000。引用哪份资料有意义，比把单元格填满更重要。

## 三条其他来源候选：需要单独决定

- `commandcode/poolside/laguna-s-2.1-free`：当前poolside来源没有对应记录；同一准确名字在OpenCode声明32000、Vercel声明32768。没有删除`-free`再匹配。
- `commandcode/zai-org/GLM-5.2-Fast`：当前zai来源没有对应记录；同名第三方记录有131072、262144、1048560等不同声明。没有删`-Fast`或拿GLM-5.2代替。
- `xl/minimax-m3`：subscription记录命中但没有所需核心字段；models.dev有19个provider候选。厂家MiniMax声明512000，Ollama Cloud声明131072等，不能默认择取。

Commandcode前两条还有配置范围问题：它们带真实provider前缀，单独修改模型的 `source_priority` 或给未成为最终token的来源写 `lookup_ids`，不能绕过渠道provider转换。若引入其他provider，需要扩展渠道映射，可能影响同前缀的其他型号，不能当成仅一行的改动。XL裸名可以使用模型级来源链和精确绑定，但来源选择仍待确认。

## 九条暂留未知

在本轮已启用的来源类型/认证范围内，固定输入中没有按原ID、既有provider路径或合法的provider完整键组合匹配到输出声明：

- `axis/gpt-5.2-2025-12-11`
- `axis/gpt-5.4-2026-03-05`
- `commandcode/deepseek/deepseek-v4-flash-fast`
- `commandcode/meituan/LongCat-2.0:free`
- `commandcode/tencent/hy3-paid`
- `congee/gpt-6`
- `ollama-cloud/deepseek-v4-pro:0813`
- `xl/gemini-3.8-flash-high`
- `xl/grok-composer-2.5-fast`

这些不是“只能手填一个数”。如果要补，先逐项核对型号对应关系或取得准确供应方资料；没有证据就继续未知。此次没有按日期、tag、变体后缀扩搜或指定替代ID。

## 60条为什么不能直接填满其他列

不是认定这些字段没必要。原60条中57条缺输入上限、60条缺 `max_tokens`、60条缺 `max_completion_tokens`；按修正后的链规则重新检查保存来源，选定命中记录仍没有声明这些缺项。没有可引用的值，不能用context减output、互抄token列或挑未批准来源制造完整。已有值是否自洽仍需单列审查。

## 历史空列统计

| 字段 | 缺失行数 | 建议 |
| --- | ---: | --- |
| `max_output_tokens` | 129 | 对启用补全的渠道优先审查准确来源声明；不从旧token列复制 |
| `output_modalities` | 129 | 有声明才补，不能把所有模型统一写成text |
| `max_input_tokens` | 186 | 仅补来源明确声明；不能用context相减推导 |
| `max_tokens(abandon)` | 143 | 独立旧字段，没声明就不强补；现有值仍保留 |
| `max_completion_tokens` | 189 | 当前全部未知，不因此复制其他输出限额 |
| 其余5列 | 各1 | 都是静态 `grok-4.7` 缺父项；先处理继承 |

37条透传/继承成员为Codex9、Kimi Coding8、SuperGrok16、Codex静态继承4。没有对它们执行外部候选搜索；“不补”是保留现有策略，不是断言全网没有资料。旧token列有值也不能证明其他token字段应当有同值。

GMICloud的403来自部署窗口的保存证据，本轮没有重试获取。其当前配置未开启bulk补全。静态Grok父项缺失同样沿用部署证据，不用另一个Grok型号冒充父项。

## 另外记录：12行已有值不一致

12行的 `default_reasoning=medium` 不在各自 `reasoning_levels` 中。CSV“已有值风险”列可筛选；完整名单在 `summary.json`。这是已有字段的组合一致性问题，不是空项，未自动改成其他默认值。

## 验证边界

- 189行原始字段取自用户消息，不从旧fixture或新HTTP响应替换；未知与0分开统计。
- 来源判断使用四份固定fixture及当前Go真实适配器，未重新抓取来源或刷新生产目录。API/flat由真实实现协调；显式查询保留准确ID与最终来源token。
- 两个临时有界检查通过：`TestTableReviewSourceProjection`（184个带渠道前缀成员）、`TestTableReviewBindingReachability`（78条内存配置建议）。后者确认原查询无来源输出声明、拟议绑定可到达准确声明。没有运行无关测试或推理请求。
- 19个Ollama Cloud成员的原生列表/show输入仍未采集；11条bulk绑定候选不代表原生输入或最终多步骤合并已验证。
- 来源精确命中不证明渠道支持对应输入、推理等级或输出预算。没有验收全部已有值、当前来源新鲜度、完整来源链或29条id形状变化。
- 未修改生产配置、重建服务、恢复旧查询规则、启用新来源、提交代码或勾选OpenSpec 3.2。
