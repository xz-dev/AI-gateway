# Provider 前缀查询

仅影响 Go 边车的 bulk 元数据查找，不修改公开名称、推理路由，也不在运行时写回来源配置。以下是配置片段，不是可直接上线的数据迁移。

```yaml
provider_prefix_map:
  vendor: [first, second]

channels:
  example:
    source_priority:
      - models.dev/original
      - modelparams.dev/original/subscription
    provider_prefix_map:
      VENDOR: third
      alias: fourth
    models:
      Vendor/Team/Model:
        lookup_ids:
          models.dev/third: Literal/Source-ID
```

- 全局与渠道字典按声明顺序合并。同键整组替换、位置不变；新键追加。因此上例有效映射是 `vendor: [third]`、`alias: [fourth]`。字符串等价于单元素数组，重复目标保留首次；同层大小写重复键后声明覆盖。
- 缺省或 `{}` 表示没有别名映射，不关闭默认拆分。`null`、空目标、非字符串、斜杠或空白 provider 组件均报错。没有模型级映射或默认来源链。
- 去掉一次 CPA 渠道前缀后，`Vendor/Team/Model` 默认拆为 provider=`vendor`、模型ID=`team/model`；只转换局部查询，剩余路径、日期、版本和tag不猜测。
- 无斜杠时沿用每个来源 token 明确指定的 provider，不对链 provider 反向应用名称映射。匹配限定在该 family/provider/authType：精确ID优先，否则只接受唯一大小写等价记录。其他provider不造成歧义、不补救miss；缺provider的modelparams记录不能满足明确来源。
- 来源链是外层顺序，映射目标是内层顺序；真正相同的查询只保留最早位置，裸名的不同provider步骤分别参与。前项覆盖，后项补缺；三个输出token字段仍独立。`ollama_cloud` 原生请求不变。
- `lookup_ids` 按**最终token**匹配，显式ID不再拆分。上例第三方ID仅用于 `models.dev/third`；旧 `models.dev/original` 的绑定不会自动迁移或成为后备查询。
- modelparams 使用独立 provider AND model 条件；models.dev 协调 API 裸/完整ID与flat完整键，避免重复前缀。对应记录保持API优先、flat补缺。匹配到来源不证明型号、版本或渠道推理能力。

## 裸名直接使用渠道链

```yaml
channels:
  shuaiapi:
    source_priority: [models.dev/anthropic, modelparams.dev/anthropic/subscription]
```

同名 `claude-opus-4-7` 等裸模型直接查询这两个明确作用域，无需重复写 `lookup_ids`。真正ID不同才保留显式绑定；模型级来源链仍整体替换渠道链。

## 迁移与回退

默认行为是BREAKING变化。先比较真实查询与来源层，再考虑移除显式绑定；目录已有字段和成员数不变不足以证明等价。代码变更时的原始差异见[固定来源报告](../PLAN/enricher-provider-prefix-replay.md)。

历史上Commandcode的40条模型配置已改为4条前缀映射，新增3项Qwen补全另获批准；旧44项检查见[历史配置整理报告](../PLAN/enricher-source-config-cleanup.md)。后来用户纠正裸名清空provider的错误，已先用原配置部署公共入口修复，再经同输入等价验证删除XL Muse的同名 `lookup_ids`；其模型级Meta链保留。新证据及尚存缺口见[两阶段修复与review](enricher-bare-chain-deployment-2026-09-08.md)。

恢复旧查询需要回退代码与配套配置；**仅清空字典不够**。后续已按用户授权完成Go-only部署，未提交代码；[上线与人工review记录](enricher-provider-prefix-deployment-2026-09-08.md)包含在线字段变化、检查结果及回退资料。部署不等于完整数据验收，原配置与修改前源码证据均保留。
