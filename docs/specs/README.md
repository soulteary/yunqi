# Yunqi 实现设计文档

本目录按模块说明配置、请求处理、数据模型与已知限制。部署和使用步骤见[开发与使用指南](../guide/getting-started.md)。

## 文档索引

| 文档 | 模块 | 核心关注点 |
|------|------|-----------|
| [autopilot.md](./autopilot.md) | Autopilot 智能路由 | 模型画像、渠道选择、硬约束、失败学习、自适应并发、TTFB 拥塞 |
| [logical-channel.md](./logical-channel.md) | 同站多协议合一 | 数据模型、归组逻辑、CRUD、Dashboard、删除原子性 |
| [new-api-integration.md](./new-api-integration.md) | New-API 多账号集成 | 账号/Key/凭证模型、verify/provision、渠道纳入调度 |
| [healthcheck.md](./healthcheck.md) | 渠道保活与余额探测 | L1/L2 探针、动态频率、稀疏模型、恢复、凭证回填、余额探测（§10） |
| [racing.md](./racing.md) | 竞速模式（影子请求） | 策略表、阈值注册表、五元组候选、提交闸门、未获胜分支的取消与记账、配置面 |
| [channel-data-model-v2.md](./channel-data-model-v2.md) | 渠道粒度重构 | Channel→Key→Endpoint→Model、能力/凭证边界、new-api 分组共享、三步迁移 |
| [public-key-routing.md](./public-key-routing.md) | 公开与临时 Key 优先消耗 | Key 级零成本、机会性消耗策略、FastDecay 回退、API/UI 与迁移 |
| [web-ui-dialogs.md](./web-ui-dialogs.md) | Web 管理界面 | 所有对话框/弹窗布局、交互、状态流转、跳转关系 |
| [web-ui-pages.md](./web-ui-pages.md) | Web 页面层 | 8 个 View 布局、导航信息架构、全局区块显隐、数据流、IA 问题 |
| [cross-module-integration.md](./cross-module-integration.md) | 跨模块集成 | 路由决策链、事件传播、状态一致性边界 |
| [model-inventory-consistency.md](./model-inventory-consistency.md) | 模型清单一致性 | 三条模型数链路口径、协议视图聚合、火山案例复盘、自动保鲜改造(用户无感) |
| [tool-call-capability.md](./tool-call-capability.md) | 工具调用能力实测 | seekai 假渠道复盘、能力测试工具探针、运行期负信号学习、硬约束收紧 |
| [guardrails.md](./guardrails.md) | Guardrails 最小集 | credential-masker 起步、优先级注册表、fail-open、统一日志脱敏入口（转发路径不改写） |
| [request-compression.md](./request-compression.md) | 请求侧工具输出压缩 | Classifier 分类、Filter 表驱动、FidelityGate 保真门、膨胀回退、Plan 开关层级、压缩统计与回退原因 |
| [omniroute-benchmark-upgrades.md](./omniroute-benchmark-upgrades.md) | 对标 OmniRoute 增强规划 | 配额真相分级调度、请求侧工具输出压缩、guardrails 最小集、路由预演升级、Tier-2/3 backlog 与不跟进决策 |
| [quota-truth-scheduling.md](./quota-truth-scheduling.md) | 配额真相分级与按余量调度 | Truth 五级+来源优先级、懒重置饱和桶、SmartRouter quotaHeadroom 因子、scheduler 沉底排序、渠道 key 余额探测接入、前端真相等级列 |
| [route-preview.md](./route-preview.md) | 路由预演升级 | 请求体直喂的零上游请求路由预演、SmartRouter + scheduler 两层对齐、前端 DiagnosePanel 升级 |

## 总览关系图

```text
[Client Request]
       │
       ▼
┌─────────────────┐
│   Entry Route   │  /v1/messages /v1/chat/completions /v1/responses ...
└────────┬────────┘
         │
         ▼
┌─────────────────┐     ┌─────────────────┐
│  LogicalChannel │◄────│  Model Registry │ (capability, effort, context, benchmark)
│  (归组/聚合层)   │     └─────────────────┘
└────────┬────────┘
         │
         ▼
┌─────────────────┐     ┌─────────────────┐
│    Autopilot    │◄────│   Healthcheck   │ (L1/L2 probe state, circuit)
│  SmartRouter    │     └─────────────────┘
│  EndpointPolicy │
└────────┬────────┘
         │
         ▼
┌─────────────────┐     ┌─────────────────┐
│    Scheduler    │◄────│   New-API Sync  │ (group multiplier, key ownership)
│ channel picker  │     └─────────────────┘
└────────┬────────┘
         │
    ┌────┴────┐
    ▼         ▼
[New-API]  [Direct Providers]
Channels   (Claude/OpenAI/Gemini/...)
    │
    ▼
[Upstream Response]
    │
    ▼
[Autopilot Learning]  ← 失败/成功反馈更新 channel/model profile
```

## 现行规格与历史记录

上方索引对应现行行为和限制，具体实现以文中函数和测试为准。已完成的修复计划、旧缺陷分析和提交记录移入[历史归档](https://github.com/soulteary/yunqi/tree/main/archive)，不再逐项标记“完成”。

- [实现缺口修复计划](https://github.com/soulteary/yunqi/blob/main/archive/specs/implementation-gap-remediation-plan.md)
- [New-API 集成修复过程](https://github.com/soulteary/yunqi/blob/main/archive/specs/new-api-integration-history.md)
- [Web 导航修复过程](https://github.com/soulteary/yunqi/blob/main/archive/specs/web-ui-navigation-history.md)

未完成事项集中在 [TODO](https://github.com/soulteary/yunqi/blob/main/TODO.md) 与专项规格的限制章节；旧记录中的问题不自动视为当前缺陷。

## 待补充

以下为跨模块的待排期方向；各模块的具体限制见对应规格。

### 待排期

- **火山 manifest 自动回填**：`manifest_drift` 已事件化并在前端健康中心告警，但仍无自动更新内置清单的下游消费者（周期性 `FetchModels` 聚合回写共享 JSON 未实现，详见 `healthcheck.md` §9.1）。
- **稀疏 L2 分时段策略**：成本语义已拆分（`CostValue`+`CostUnit`）且 AFP 余额联动已落地，但高峰/低谷时间窗口自动降/升预算仍未实现（详见 `healthcheck.md` §9.3）。
- **对标 OmniRoute 增强四方向**：配额真相分级调度、请求侧工具输出压缩、guardrails 最小集、路由预演升级，及 Tier-2 backlog（管理面 MCP 化、韧性错误分类补课、context-relay、doctor CLI）——详见 [omniroute-benchmark-upgrades.md](./omniroute-benchmark-upgrades.md)。
