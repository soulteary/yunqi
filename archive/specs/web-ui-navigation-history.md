# Web 导航历史问题与修复记录

保留 `cd59435` 中页面规格的旧移动导航实测、问题分析和修复记录，文内相对链接按归档位置调整。下文的页面数量、路径、行号和问题状态只反映原记录；现行说明见[Web 页面规格](../../docs/specs/web-ui-pages.md)。

### 1.3 移动端下拉（`App.vue` L76–97，`display.width < 1000`）

`v-menu` + activator 按钮（显示当前路由 label），列表项 = `translatedApiTabOptions`（8 项）。**不含 cost-report**。

> **ego-browser 实测**（480px 宽度）：桌面平铺链接全部隐藏（`visibleDesktopLinks: []`），头部出现 activator 按钮（如 `CHANNELS`，显示当前路由 label）；点击展开下拉。**移动端无法进入 `/cost-report`**。

### 1.5 导航层问题

- **tab 清单漂移（P1）**：移动端 8 项 vs 桌面 9 项，**cost-report 仅桌面可达**；移动端无法进入 `/cost-report`。
- **硬编码中文（P2）**：桌面导航"成本报表"未走 i18n（`app.tabs.costReport` key 在 en/zh-CN 两个 locale 均**缺失**）。
- **图标重复（P3）**：conversations 与 cockpit 同用 `mdi-view-dashboard-outline`，移动端下拉辨识度低。

## 9. 已知导航 / 信息架构问题汇总

截至最近一次回填，§0–§8 中所有问题已随代码修复同步到正文，以下汇总表保留仅作历史索引。当前剩余可观测/体验类待办已在各专项文档中跟踪，不再重复。

| # | 问题 | 位置 | 状态 |
|---|---|---|---|
| P1 | Tab 清单漂移：移动端 8 项 vs 桌面 9 项，cost-report 仅桌面可达 | `useAppController.apiTabOptions` | ✅ 已修复 |
| P2 | 硬编码中文未国际化 | `App.vue`、`CostReportView.vue`、`locales/*` | ✅ 已修复 |
| P3 | 导航 icon 重复：conversations 与 cockpit 同图标 | `apiTabOptions`、`App.vue` | ✅ 已修复 |
| P4 | 全局统计卡/操作栏路径黑名单不对称 | `App.vue` L240/263/310 | ✅ 已修复 |
| P5 | 健康数据轮询不一致 | `ChannelsView` vs `HealthCenterView` | ✅ 已修复 |
| P6 | 确认体系分裂：SubscriptionsView 用原生 `window.confirm` | `SubscriptionsView.vue` | ✅ 已修复 |
| P7 | 提示体系分裂：SubscriptionsView 自带本地 `v-snackbar` | `SubscriptionsView.vue` | ✅ 已修复 |
| P8 | 路由守卫空转 | `router/index.ts` | ✅ 已修复 |
| P9 | 空态覆盖不均 | `EmptyState.vue`、`HealthCenterView.vue`、`SubscriptionsView.vue`、`AutopilotView.vue`、`locales/*` | ✅ 已修复 |
| P10 | i18n 命名重叠易混：conversations 与 cockpitOverview 曾同为"驾驶舱" | locale JSON | ✅ 已修复 |

### 已修复记录

- **P10（2026-08-09, commit c3b9d161）**：`/cockpit` 的 `app.tabs.cockpitOverview` 由"驾驶舱/Cockpit/Kokpit"改为"总览/Overview/Ikhtisar"，`/conversations` 的 `app.tabs.conversations` 保留"驾驶舱/Cockpit"，两个 tab 不再重名。
- **P1 / P2 / P4（2026-08-09, commit d876784d）**：
  - P1 移动端 cost-report 可达（`apiTabOptions` 补项 + `mdi-finance` 图标注册）。
  - P2 成本报表国际化（`app.tabs.costReport` + 43 个 `costReport.*` key ×3 locale，`CostReportView` 与导航改用 `t()`）。
  - P4 黑名单补 `/subscriptions` `/cockpit`。
- **P5 / P9（2026-08-09，Phase B.3 及后续）**：ChannelsView 与 HealthCenterView 统一接入 `useEventStream` 事件驱动刷新；新增 `EmptyState` 补齐 Health/Subscriptions/Autopilot 三页 View 级空态。
- **P6 / P7（2026-08-09）**：`SubscriptionsView` 删除改用 `dialogStore.confirm`；`success`/`error` emits 统一走 App 全局 toast，移除本地 `v-snackbar`。
- **P8（2026-08-09）**：移除对所有路由一律 `next()` 的空转 `beforeEach`，鉴权由 `App.vue` 持久认证对话框承担。
- **CapabilityTestDialog 接线（2026-08-09, commit d876784d）**：见 [web-ui-dialogs.md §16.1](../../docs/specs/web-ui-dialogs.md)。

### 关键文件路径清单

- 路由：`frontend/src/router/index.ts`
- 布局/导航/黑名单：`frontend/src/App.vue`
- 控制器/tab 源：`frontend/src/composables/useAppController.ts`
- store：`frontend/src/stores/{channel,auth,system,dialog,preferences}.ts`
- Views：`frontend/src/views/{ChannelsView,ConversationsView,HealthCenterView,SubscriptionsView,CockpitView,AutopilotView,CostReportView}.vue`
- 页面级区块组件：`frontend/src/components/{ChannelOrchestration,ConversationDashboard,ConversationCard,GlobalStatsChart,HealthCenterStats,HealthChannelTable,ProfileChangelogTimeline,AutopilotModePanel,AutopilotDiagnosePanel,AutopilotTraceStats,AutopilotTraceTable,SubscriptionPlanTable,NewApiSubscriptionForm}.vue`、`frontend/src/components/subscriptions/{SubscriptionProviderGrid,ExchangeRateManager}.vue`
- locale：`frontend/src/locales/{en,id,zh-CN}.json`
