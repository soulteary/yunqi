# TODO

这里只维护未完成或待评估事项。上游版本、模型能力与价格描述保留原记录日期，实施前必须重新核验，不能据此判断最新产品状态。

- 新事项用 `## [ ] 标题`，说明问题、验收目标和证据；附 Issue 链接（如有）。
- 完成后把发布摘要写入 [CHANGELOG](CHANGELOG.md)，详细评估和提交依据转入[已完成任务归档](archive/completed-tasks.md)，从本文件移除。
- 当前系统设计见 [规格索引](docs/specs/README.md)；旧设计、交接记录见[历史归档](archive/README.md)。

## [ ] 管理后台使用报表导出 (#229)

来源：https://github.com/soulteary/yunqi/issues/229

需求：在管理后台为渠道使用统计增加“导出使用报表”能力，支持按月份（默认当前月，后续可考虑自定义日期范围）导出当前渠道或全部渠道的使用数据。

建议范围：优先支持浏览器下载 CSV；JSON 可作为可选格式；XLSX 和定时邮件属于 nice-to-have，暂不作为首批必做项。

导出内容应覆盖当前仪表盘已有的核心指标：渠道名、服务类型、日期/小时桶、请求数、可用率、输入/输出 token、缓存读写、RPM、TPM。

备注：该需求目前不确定是否有更多用户需要，先记录为待评估项。

---

## [ ] gpt-5.6的适配

当前状态（2026-09-16 核对）：模型注册/窗口/基准已落地（luna/sol/terra + Bedrock，context 1.05M）。仍待办如下。

2.openai新增了"OpenAI-Beta": “{client_header:OpenAI-Beta}” 这个，来传输子代理相关信息，这个也需要调整
3.工具调用传参之类的都需要优化
4.提醒下同行，oai在最新版本codex里面加入了设备验证相关信息，可能会封pro号

---

## [ ] 在codex里面使用imagegen使用上游的文生图

当前状态（2026-09-16 核对）：Images 渠道已有 `ConvertImageURLToB64JSON`（百炼 URL → OpenAI `b64_json`），管理界面可开，默认关闭。Codex 自带 imagegen 技能走 `/v1/images` 或 Responses `image_generation` 打到百炼的端到端通路仍待验证。

百炼生图是生成url, 和 openai生成base64不一样，比如我在codex介入ccx, ccx配的是百炼，然后在codex里面用 codex自带技能 imagegen 能调用百炼生图模型 成功生图
https://github.com/QuantumNous/new-api/issues/5513

---

## [ ] 英伟达渠道导入

当前状态（2026-09-16 核对）：`builtinProviderTemplates` 仍无 nvidia/NIM 模板；历史 Autopilot 设计将此项列为 P2。Claude Code v2.1.269 新增 `CLAUDE_CODE_GATEWAY_MODEL_DISCOVERY_TIMEOUT_MS`（默认 3s），客户端侧可缓解 `/v1/models` 超时，不替代 Yunqi 导入模板。

可能模型太多响应太慢导致预检时间过长，或者需要代理？

---

## [ ] Claude Code v2.1.295 上游协议/工具变更评估

发现协议/工具/用法变更（2.1.288→2.1.295）。初步判断：主要为 hooks `onFailure`、Program Status Protocol、plugin 架构完善、Claude Haiku 5.5 新模型、MCP 改进、云会话修复等，多为客户端内部能力与容错增强，协议层面无强制改动需求，列为观察评估待办。

关键变更点：
- v2.1.295：hooks `onFailure: "block"` 策略、OSC 7501 终端状态协议、`timeouts.upstream_ttfb_ms` 支持扩展到云上游、Fable 模型列表修复、`effort` 参数传递到 Agent tool
- v2.1.293：Claude Haiku 5.5 新增（`claude-haiku-5-5`，1M context，$0.10/$0.50 per Mtok）、plugin `isDeferred` 工具注册、context compaction 内存泄漏修复
- v2.1.292：`--marketplace <source>` 快捷安装、Agent tool `effort` 参数、prompt caching for mods、auto mode 修复
- v2.1.290/291：plugin 架构重构、云会话稳定性、MCP 改进、安全修复（PreToolUse bypass、网络路径读取）

待深入评估项（按优先级）：
1. **Haiku 5.5 模型注册**（v2.1.293）：需确认 Yunqi `model-registry.json` 是否已注册该模型及其定价/能力
2. **hooks `onFailure` 行为**（v2.1.295）：虽为客户端 hook 能力，需确认 Yunqi 作为网关是否透传相关配置头
3. **`upstream_ttfb_ms` 超时配置**（v2.1.295）：Yunqi 自身已有上游超时机制，需对齐新增的 TTFB 语义
4. **plugin 架构变更**（v2.1.290-295 多版本）：`$.tool.register` / `isDeferred` / mod 热重载 / guard hooks，确认是否影响 Yunqi 作为 LLM 网关的透传行为
5. **Agent tool `effort` 参数**（v2.1.292）：确认 Yunqi Responses 渠道是否需要感知该参数

---

## [ ] Codex rust-v0.162.0 上游协议/工具变更评估

发现协议/工具/用法变更（0.160.0→0.162.0）。初步判断：主要为 worktree 管理、任务钉选、transcript 复制、MCP 改进、自定义 provider 能力扩展、Code Mode 工具搜索等，多为 TUI/app-server 侧能力，wire protocol 层面变更较少，列为观察评估待办。

关键变更点：
- v0.162.0：managed worktree 工具、任务钉选（Command Center）、`/copy` transcript 导航、自定义 provider 的 `live_web_access` / `remote_compaction` 能力配置、Code Mode 工具搜索排名、JavaScript streaming helpers
- v0.161.0：GPT-6.1 Sol 默认模型、Bedrock multi-agent V2 + Ultra reasoning、`/mcp login` TUI 命令、音频设备选择、Daybreak opt-in 机制、filesystem 提权与权限保留、Windows 沙箱改进

待深入评估项（按优先级）：
1. **GPT-6.1 Sol 模型**（v0.161.0）：需确认 Yunqi 是否需要注册该模型（若客户端通过 Yunqi 访问 OpenAI/Bedrock）
2. **自定义 provider 能力配置**（v0.162.0）：`live_web_access` / `remote_compaction` 字段，确认 Yunqi 作为自定义 Responses provider 是否需要在 `/v1/models` 或其他发现端点暴露这些能力
3. **multi-agent V2 与 Ultra reasoning**（v0.161.0）：Bedrock 侧新增，需确认 Yunqi Responses 渠道对 Bedrock 的兼容性
4. **reasoning parameters 演进**（v0.161.0 延续 v0.145）：确认 Yunqi `NormalizeReasoningObjectForUpstream` 是否需要适配新行为
5. **CRLF 保留**（v0.162.0 `apply_patch`）：与 Yunqi 无直接关系，但可作为 converter 文本处理参考

## [ ] 复核 Autopilot 历史设计中的后续方向

原设计的 Phase 1/2 勾选状态已过时，不能作为未实现清单。当前模块与接口以 [Autopilot 规格](docs/specs/autopilot.md)及真实代码为准；现有确定的跨模块待排期项见[规格索引的待补充](docs/specs/README.md#待补充)。

以下从原设计的 P2/界面演进部分保留为待评估方向，未重新认定为当前缺陷；排期前先确认是否已由现有功能覆盖：

- 人工试用意图的 Web 操作入口，以及探测预算、订阅适配器等策略配置的可达性。
- Images/Vectors 的 operation、尺寸/质量、embedding 维度和原生端点健康展示。
- advisor 隐私禁用、本地 runtime 不可用、成本置信度低、来源混杂与 RPM/TPM 未知的异常提示。
- 导入/导出是否完整保留订阅、倍率、信任等级和本地 runtime 绑定，同时避免导出明文密钥。
- 只开启 trace/dry-run 的解释模式，以及按任务类别展示节省、fallback、advisor、override 和 SLO 的报表。

旧的“双桌面/Web 前端对齐”方向随 Wails 移除退出范围。完整原始材料保存在[历史设计](archive/design/channel-autopilot.md)。
