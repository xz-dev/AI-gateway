# NIM两项元数据：本地配置修复

## 范围

最初范围是NIM两项本地配置修复。随后用户明确批准“省略冲突默认值 可部署”，再追加将zcode-api、Sub2API、官方CPA升级到最新稳定版。

当前NIM配置及Go默认值规则已本地实现，生产尚未修改。APISIX、账号映射、透传开关、其他Go来源链及资源/安全设置不在修改范围。Sub2API的新数据库迁移和请求准入语义需要单独确认后才能实施。

## 根因与变更

`sourceQueries`按既定合同拆分带厂商前缀的模型名，覆盖来源token的provider。原NIM配置只有`models.dev/nvidia`，实际得到：

- `models.dev/deepseek-ai` + `deepseek-v4-flash-0731`：miss。
- `models.dev/minimaxai` + `minimax-m3`：miss。

保存的NVIDIA来源却使用完整ID。现将NIM渠道的`deepseek-ai`、`minimaxai`映射到`nvidia`，并仅为这两项配置`models.dev/nvidia`下的完整查询ID。公开模型ID、来源URL、来源优先级及其他渠道均不改。

## 已同意的可观察例子

从缺少输出字段的模拟CPA目录出发，经真实Go HTTP处理链及本地假上游：

| 公共模型 | 来源上下文 | 来源输出上限 | 来源输入模态 |
|---|---:|---:|---|
| `nim/deepseek-ai/deepseek-v4-flash-0731` | 1,000,000 | 384,000 | text |
| `nim/minimaxai/minimax-m3` | 1,000,000 | 16,384 | text、image、video |

- 两项均得到输出模态`text`及来源名称。
- 目录仍只有这两个批准成员；来源独有模型不能进入目录；公开ID保持原值。
- 来源未声明的`max_input_tokens`、`max_tokens`、`max_completion_tokens`仍缺失，不互填。
- bulk API与flat读取各一次，不增加逐provider HTTP或启用其他来源。
- 不猜替代默认值、不写null。明确的支持列表不含合并默认值时，省略该默认字段；当前对象的显式人工默认值覆盖仍优先。
- 在引用、人工覆盖完成后才判断；原样失败回退、跳过获取且无来源、OAuth对象保持原有边界。

## 验证证据

回归：`models-enricher/nim_source_test.go`中的`TestNIMConfiguredFullIDMetadata`。加载实际`config.yaml`的NIM段，仅隔离无关渠道和静态项；复用已有`fakeCPA`、`newTestHandler`、`identityCatalog`，不引入框架。

- Red：旧配置下命令退出1，两项输出上限分别为缺失而非384000/16384，来源字段未进入目录。不是编译或环境失败。
- Green：修改NIM配置后，以下命令退出0，**11个顶层测试、49个通过事件（含子测试）**，开启race。

```sh
cd models-enricher
go test -race -count=1 -run '^Test(NIMConfiguredFullIDMetadata|ProviderPrefix)' -timeout 90s .
go vet ./...
```

默认值规则的后续验证：NIM HTTP例子先因保留冲突`medium`失败；新规则覆盖普通补全、引用、隐藏池和静态继承，保留显式人工覆盖及原样回退。当前完整Go模块命令如下，退出0，**112个顶层测试、240个通过事件（含子测试）**：

```sh
go test -race -count=1 -timeout 120s ./...
go vet ./...
```

已更新实际配置约束测试，仅将本次获准的NIM两条绑定及映射纳入允许范围，没有放宽其他渠道约束。`go vet`、格式及`git diff --check`通过。执行时禁用依赖网络下载；HTTP测试使用本地假服务器。未运行仓库其他模块或CI，未提交、推送。

最初默认值测试还暴露：现有普通metadata overlay会丢弃null字段，且直接构造的“unconfigured”输入未模拟真实保留边界。这些初次失败日志保留；本轮不扩修null合并语义，改用过滤函数的独立边界测试证明新规则本身不改null/false/0/空字符串/未知列表，并分别验证真实补全和回退路径。不能据此宣称整个旧合并器已完整保留null。

本地原配置备份、red/green JSON事件与命令回执：`/root/.cache/go-nim-metadata.RIuyjg/`。

## 数据与验收限制

- 测试来源字段摘自2026-09-08保存的models.dev API快照，SHA256：`3335e4ab406bcb3fb8519d81ae53c8934c750a3359f75139795049bd7ced0612`。`source-only`是测试的成员准入哨兵，不是该来源中的真实模型。
- 旧189项重放的catalog输入本身已含NIM富字段；即使查询miss，最终对象也可能看起来正确。因此新测试使用缺字段的模拟CPA输入，不以旧重放输出证明此次修复。
- DeepSeek来源声明推理等级`none/high/max`，没有声明默认值；原默认`medium`不在列表中。按后续授权，候选Go会省略默认字段而非猜值；尚未部署。
- MiniMax来源只有推理toggle声明，不据此猜effort默认值。
- 结果只证明本地例子和既定来源合同，不代表线上字段已更新，不证明NVIDIA渠道实测能力，也不把旧187项缺项统计改写为当前库存。
- GMI来源选择、其他来源候选不在本次修改范围。默认值规则已扩为获准的Go统一行为；实际全表效果仍待生产验收。

## 追加版本升级：已核官方release，待镜像/迁移预检

| 服务 | 当前运行版本 | 最新非预发布release |
|---|---|---|
| CPA | v7.2.153 | v7.2.155 |
| Sub2API | 0.2.1 | v0.2.3 |
| zcode-api（provider-sidecar） | 4.0.0 | v4.6.3 |

zcode-api运行镜像名仍为`ghcr.io/tridefender/zcode-proxy`；OCI source指向`TriDefender/zcode-api`。不能从镜像名猜成同名GitHub仓库。尚未锁定候选镜像digest或重建任何生产服务。

Sub2API官方比较`v0.2.1...v0.2.3`包含迁移235/236：把`groups.models_list_config`改名为`model_allowlist`，并把原展示过滤升级为请求准入白名单。生产只读核对：旧列存在、新列不存在，6个组中1个显式启用了旧列表，数据库约1.54 GB。**不能只换回旧镜像作为回滚；需要先明确接受准入变化、备份和隔离迁移/回退演练。** 当前未执行迁移。

## 部署完成（2026-09-09，owner go-nim-upgrade-20260909T023858Z 已释放）

- 四服务全部升级并健康：CPA v7.2.155、Sub2API v0.2.3（迁移235/236已应用，model_allowlist=true）、zcode-proxy 4.6.3、models-enricher新镜像。pg_dump备份保留于证据目录。
- NIM修复+推理默认冲突省略已上线：DeepSeek默认省略、MiniMax medium保留。
- 静态模型改为虚空池（custom_channels.static-parents，source_priority: [models.dev/openai, models.dev/xai]）：四个GPT static从models.dev/openai取全量元数据，statics层overrides把context_window/max_context_window压到372000；grok-4.6从models.dev/xai取（500000），替代了原错误的grok-4.7。池保持纯来源，不继承任何真实CPA模型，CPA漂移不再影响static。
- OAuth配对修复：identity.go第二步去掉"裸名必须同账号注册"条件（force-model-prefix下CPA只注册带前缀ID，该条件恒假导致codex/supergrok/kimi-coding全部被过滤）。改为"注册ID去前缀后命中本provider definitions或别名即qualified"。测试重写为全前缀注册集语义。
- 核验：models-table 187行；目录前门187条（supergrok 16、kimi-coding 8、codex 9、裸5）；Sub2API运营前门176条（codex为0是交集既有行为，4个组allowlist均disabled）。shuaiapi/claude-opus-4-5消失系CPA自身移除。
- 本地测试：112顶层测试、240 pass事件、-race与vet全绿。部署镜像localhost/models-enricher:oauth-pairing-20260909T045000Z（binary sha256 f7f80f0e23c88714e0391885fc998500aadca33fbdca65a1a3a73bd44eeb84e4）。
- 部署陷阱记录：bind mount单文件按inode挂载，os.replace换inode后`compose up`不感知内容变化，必须`--force-recreate`才能让Go进程重读config。

## 未决（未授权）

- GMI 403链、其他来源候选、Commandcode推理失败、OOM频率：均未展开。
- modelparams.dev可用于补max_completion_tokens/max_tokens列（models.dev不声明这些），主人暂未要求加入static-parents链。
- 整表合并的null保留（overlay丢null）已知但未修，超出本次范围。
