# Provider-prefix 已部署，等待人工review

2026-09-08。用户授权部署整理后的Go边车及来源配置，再人工review。部署和基础边界检查已完成；这不表示全部元数据差异获验收。未提交代码、扩大补全范围或关闭OpenSpec 3.2。

## Review入口与重点

目录表路径：`/models-table`。已验证的服务器本机地址是 `http://127.0.0.1:9083/models-table`，返回200。9083是内部目录入口，不假设它可从外网直接访问。需要在自己的浏览器访问时，可使用现有入口，或自行建立SSH转发：

```sh
ssh -N -L 19083:127.0.0.1:9083 root@<tailscale-ip>
# 浏览器打开 http://127.0.0.1:19083/models-table
```

本次在线结果：

- 189个成员与切换前相同；已有 `id` 在两侧都存在时没有改名。
- 原41项及新增3项，共44项目标的 `max_output_tokens` 都存在，并与固定输入的预期数值一致。这是在线输出字段核对，不冒充全部来源层或推理能力验收。
- **全目录78项失去 `max_output_tokens` 字段，49项新增，字段存在数从89降为60。** 固定输入含已有补全值，不能拿它的缺项数量替代这次真实线上前后对照。
- **27项失去 `id` 字段，2项新增，共29项存在性变化。** 成员名称没变，不代表响应形状没有兼容风险。

失去输出字段的分布：

| 渠道 | 条数 |
| --- | ---: |
| Axis | 7 |
| Commandcode | 15 |
| Congee | 6 |
| NIM | 2 |
| Ollama Cloud | 11 |
| ShuaiAPI | 13 |
| XL | 14 |
| Zcode | 10 |

这些差异保留供人工review，没有为了补回数字而改变已批准查询语义、添加更多绑定或互填token字段。

逐模型对照保存在本地 `/root/.cache/provider-prefix-deploy.W7z7V6/`：

- `review-metadata.json`：189项的前后九项核心字段及id；缺失键与显式值分别保留。
- `live-review.json`：78项失值、49项新增、29项id存在性变化的完整名称清单，以及44项在线输出数值。
- `deployment-receipt.json`：制品、配置、容器身份、OOM观察与HTTP检查记录。

完整目录原始响应留在生产的私有证据目录中，不在文档内展开。

## 有界上线检查

仅手工重建 `models-enricher`：

```sh
docker compose up -d --no-deps --no-build --pull never --force-recreate models-enricher
```

- 镜像从已冻结的本地源码构建、导入；生产候选 `.env` 只改变Go镜像行，源配置只改变Commandcode和XL。
- 实际生效Compose的唯一服务配置变化是Go镜像；三个 `pull_policy: never` 保留。
- Go实际二进制、挂载配置与候选哈希一致；运行环境、启动参数、挂载、完整HostConfig均保持原状。
- Go限制保持256MiB内存/交换总额、0.5 CPU、64 PIDs、64MiB `/tmp` tmpfs。
- 仅Go容器身份变化；其他47个容器的身份、镜像、重启次数及运行/健康状态不变。总计48个运行容器、9个声明的健康检查通过；Go自身 `/healthz` 返回200。
- 观察窗口内Go的OOM kill计数保持0；主APISIX既有计数保持4，没有本次增加。这不是长期稳定性保证。

一次有界HTTP检查使用原先批准的用户凭据，经stdin传入，不打印或保存：

| 检查 | 结果 |
| --- | --- |
| Go健康 | 200 |
| 完整目录 `client_version=1` | 200，189项 |
| 同版本当前用户授权集与前端完整记录交集 | 一致，67项 |
| ETag条件请求 | 304，空响应体 |
| 匿名 / 无效凭据 | 均404，空响应体 |
| 目录表 | 200 |

未重跑之前已通过的代码测试、无关SSR全套或推理压力测试。只分析这次保存的响应，没有以反复请求刷新数据来制造通过。

## 尚存缺项与观察

- 本轮日志显示GMICloud自身渠道取数403，Go按规则退回CPA记录；未换身份重试。
- 静态 `grok-4.7` 的 `supergrok/grok-4.7` 父项仍缺失。
- 实时目录缺字段数：`max_output_tokens`/`output_modalities`各129项，`max_input_tokens`186项，`max_tokens`143项，`max_completion_tokens`189项；其他四项核心字段各缺1项。
- 原[本地固定输入缺项报告](../PLAN/enricher-source-config-cleanup.md)仍是那份输入的结果，不是当前生产完整性证明。新的在线变化需人工review，完整数据验收仍未完成。

## 制品与回退资料

生产：`root@<tailscale-ip>:/root/AI-gateway`。

| 项目 | 当前值 |
| --- | --- |
| 镜像 | `localhost/models-enricher:provider-prefix-20260908T134822Z` |
| Docker镜像ID | `sha256:363b70a1dd50f36117f0dc3a2ce64f1abfa8cf3a907951e10334d3d5bc55803f` |
| Go二进制SHA256 | `ee69a402a7910b99bace55c10cc1243ccb091610e35842cfbed7e75211bb14ff` |
| 挂载配置SHA256 | `88b8559a24393673e047ec19313f2ef1342ec8ed6149a4311d42715e8e76d44d` |
| 镜像压缩包SHA256 | `ba2dfd7f07b0cfd482b9732d038b5255b751d1e89da71202189ccad83c49264d` |

旧镜像保留：`localhost/models-enricher:casefold-20260908T093632Z`。

生产私有备份与证据：`/root/AI-gateway/.provider-prefix-20260908T134822Z/`。其中 `backup/` 保留切换前 `.env`、Go配置及Compose文件；本次并未修改两个Compose文件。若用户决定回退，应只恢复备份的 `.env` 和Go配置，再手工重建Go，不恢复整个工作目录或其他服务。

本任务锁已核对owner并释放；旧镜像、备份、对照保留，既有的其他任务锁没有删除。
