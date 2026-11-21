# New-API 集成设计文档

## 1. 集成入口与接口定义

### 1.1 路由注册（后端装配）
`backend-go/routes_autopilot.go` 在 autopilot 就绪后装配三组路由，全部挂在 `apiGroup`（`/api`）下：
- `autopilot.RegisterSubscriptionRoutes` — 订阅中心 CRUD + link/unlink + refresh
- `autopilot.RegisterNewApiSubscriptionRoutes` — new-api verify/provision
- `autopilot.RegisterSubscriptionAccountRoutes` — 多账号 + 主账号凭证更新

共享依赖 `NewApiSubscriptionSyncService`（装配见 `bootstrap_autopilot.go`，后台启动见 `background.go`）。启动时先调用 `newApiSyncService.SyncAllNewAPIAsync(context.Background())` 对所有 `provider=new_api` 订阅做一次全量同步（`newapi_subscription_sync_service.go`），随后 `newApiSyncService.Start(ctx)` 启动周期为 30 分钟的后台同步循环。路由注册位置与同步服务生命周期分别以 `routes_autopilot.go` 与 `background.go` 中的符号为准，避免行号再次漂移。

### 1.2 核心端点（`internal/autopilot/handlers_newapi.go`）
`RegisterNewApiSubscriptionRoutes`（行 110-114）：
- `POST /api/subscriptions/newapi/verify` → `handleNewApiVerify`（行 375）— 校验令牌 + 预览账户/分组/模型，**不落库**
- `POST /api/subscriptions/newapi/provision` → `handleNewApiProvision`（行 427）— 建 profile + 建 key + 建/并渠道 + 触发 Discovery

依赖结构 `NewApiRouteDeps`（行 40）：`Store *SubscriptionStore`、`CfgManager *config.ConfigManager`、`Runner *AutoDiscoveryRunner`、`SyncService *NewApiSubscriptionSyncService`。provision 采用**站点级锁 + 订阅级锁**串行化远端 key 查重/创建与同订阅画像写入，避免并发建同名远端 key。

### 1.3 请求/响应结构（`handlers_newapi.go`）
- `NewApiVerifyRequest`（行 127）：`baseUrl, accessToken, userId, authTokenMode, displayName, subscriptionUid`
- `NewApiVerifyResponse`（行 137）：`username, userId, quota, usedQuota, groups(map[string]float64), groupFetchError, availableModels, suggestedOriginType/Tier, accessTokenMasked`
- `NewApiProvisionRequest`（行 152）：`subscriptionUid, displayName, baseUrl, accessToken, userId, authTokenMode, channelKind(必填), channelName, provisionKeyName, provisionGroup, provisionAllEligibleGroups, provisionModels, maxGroupMultiplier(*float64), notes`
- `NewApiProvisionResponse`（行 173）：`subscription, channelUid, channelIndex, channelName, mergedChannel, provisionedKey(仅此次返回明文), provisionedTokenId, reused, provisionedKeys[], discoveryStarted`

### 1.4 上游适配器（`internal/autopilot/newapi_adapter.go`）
`NewApiAdapter`（行 98），零值可用（默认 15s client），封装 new-api family 面板接口。`doRequest`（行 121）统一注入认证并解析 `{success,data,message}` 信封（`newApiEnvelope` 行 29）。认证头 `buildAuthHeader`（行 110）：`bearer`（默认 `Authorization: Bearer <token>`）/ `raw`/`raw_auth`（裸 token）；同时下发 `New-API-User` 与 `User-id`（fork 兼容，行 142-146）。非 2xx 错误体经 `summarizeErrorBody` 生成用户可读摘要：HTML 拦截页（WAF/边缘节点，如 451 访问受限）仅提取 `<title>`，不再把整页标签/CSS 灌进错误提示；非 HTML 响应截断 512 字符返回（2026-08-30）。

调用的 new-api 接口子集与方法：
- `Verify`（行 198）→ `GET /api/user/self`（`NewApiUserSelf`：id/username/quota/used_quota）
- `VerifyWithFallback`（行 209）— userID 为空时回退 `userId="1"` 重试（`isNewApiUserHeaderError` 行 237）。注意：标准 new-api 的 access token 认证强制要求 `New-Api-User` 头（缺失时报 `New-Api-User header not provided`），空 userID 仅在 fork 不校验该头或账号恰为 ID=1 管理员时可用，因此 **userId 实际必填**；前端各入口表单（2026-08-30）均已改为必填，后端保留空值兜底仅为兼容直接 API 调用与宽松 fork
- `FetchGroups`（行 259）→ `GET /api/user/self/groups`（返回 `{group: ratio}`）
- `FetchModels`（行 272）→ `GET /api/user/models`
- `ListTokens`（行 281）/ `FindTokenByName`（行 309）→ `GET /api/token/?p=&size=`（分页遍历，maxPages=1000 防重复建 key）
- `ProvisionKey`（行 363）→ `POST /api/token/`（`NewApiCreateTokenRequest` 行 75，默认 `unlimited_quota=true, expired_time=-1`）。查重复用，明文经 `normalizeNewApiPlaintextKey`（行 352）补 `sk-` 前缀（否则上游 401）。分组不匹配时返回 `NewApiProvisionKeyConflictError`。
- `DeleteToken`（行 441）→ `DELETE /api/token/{id}`（失败补偿回收）
- `FetchBalance`（行 249）— 复用 Verify，返回 `quota` 原值 + `currency="quota"`

## 2. 多账号建 key、主账号凭证管理（前后端位置）

### 2.1 后端（`internal/autopilot/handlers_subscription_accounts.go`）
`RegisterSubscriptionAccountRoutes`（行 810）：
- `PATCH /api/subscriptions/:uid/newapi-credentials` → `handleUpdateNewApiCredentials`（行 703）— 主账号凭证更新，指针字段区分“不改”与显式提交（`NewApiCredentialsUpdateRequest`，含 `proxyUrl`/`proxyPreferDirect` 订阅级代理设置），落库前 `VerifyWithFallback` 按生效代理校验，支持 `expectedVersion` 乐观锁，落库后触发 `SyncNow`
- `POST /api/subscriptions/:uid/accounts` → `handleAddSubscriptionAccount`（行 89）— **先 verify 拉账号/分组/模型，再按倍率阈值为全部合格分组自动建 key**（`namePrefix = accountUID + “-”` 避免同站同名复用冲突），失败回滚远端 key
- `GET /api/subscriptions/:uid/accounts` → `handleListSubscriptionAccounts`（行 332，脱敏；仅返回额外账号，主账号经 `GET /subscriptions/:uid` 单独拉取、前端组装进列表首行）
- `DELETE /api/subscriptions/:uid/accounts/primary` → `handleDeletePrimaryAccount`（行 373）— 账号平权：删除主账号＝清空订阅级凭证（AccessToken/UserID/Username/AuthTokenMode）与 ProvisionedKeys、剔除其自动接入 key（best-effort 回收远端），订阅本体（baseUrl/分组/计费/渠道关联）保留；无主账号时 409
- `DELETE /api/subscriptions/:uid/accounts/:accountUid` → `handleDeleteSubscriptionAccount`（行 434）— 先从渠道剔除 key，再 best-effort 回收远端 key，再移除账号
- `POST /api/subscriptions/:uid/accounts/:accountUid/refresh` → `handleRefreshSubscriptionAccount`（行 493）
- `PATCH /api/subscriptions/:uid/accounts/:accountUid/credentials` → `handleUpdateSubscriptionAccountCredentials`（行 576，62d2d46d）— 子账号独立更新凭证（accessToken 留空=保持不变），支持 userId/authTokenMode。**账号平权后（2026-08-28）编辑面板已不再调用**（换凭证统一为删除+重新添加），端点保留供外部调用

**账号平权模型（2026-08-28）**：主账号与子账号在 UI 平权——主账号行同样可删除；换主凭证的完整路径=删除主账号 → 「添加账号」输入新令牌。`handleAddSubscriptionAccount` 在订阅无主凭证时（行 235）自动把新账号提升为主账号（回填凭证/用户名/余额并从 Accounts 移除该条目，避免既主又子）；`SyncNow` 无主凭证时（`newapi_subscription_sync_service.go:291` 起）跳过站点级同步、仅同步子账号，不视为失败。

多账号建 key 复用主流程 `provisionNewApiGroupKeys`（`handlers_newapi.go:227`），核心复用逻辑集中在此。

### 2.2 前端
- 主/多账号面板：`frontend/src/components/edit-channel/NewApiAccountPanel.vue`
  - 订阅 UID 解析 `effectiveSubscriptionUid`（行 382，787db651）：`props.subscriptionUid || localSubscriptionUid`，仍为空且非 generic、`autoManagedKind==='new_api'` 时按约定兜底 `newapi-${channelUid}`——渠道 key 配置 `sourceSubscriptionUid` 丢失（编辑换 key 切断关联、`BuildChannelView` 不再暴露 `subscriptionUid`）时面板仍可直连订阅
  - `bindNewApi`（行 457）：generic 渠道绑定 new-api，`subscriptionUid: newapi-${channelUid}`；**先 verify，再按上游 `groups + availableModels` 调 `provisionAllEligibleGroups=true`**
  - `refreshPrimaryAccount`（行 523）→ `api.refreshSubscription`；`deletePrimaryAccount`（行 540）→ `api.deleteSubscriptionPrimaryAccount`（账号平权：主账号可删，换凭证=删除+重新添加，后端自动提升新主账号）
  - `handleAddAccount`（行 570）：主账号订阅未就绪时置 `addError`（`subscriptionUnavailable`，787db651）而非静默返回；就绪时同样**先 verify，再显式传 `provisionAllEligibleGroups` / `maxGroupMultiplier` / `availableModels`**；`refreshAccount` / `deleteAccount`（行 616/628）为子账号行操作
  - UI 结构（40d8b990）：主账号默认作为账号列表首行（「主账号」徽章，展开=纯详情，行操作刷新/删除），与子账号行同构；详见 `web-ui-dialogs.md` §5
  - `authTokenModeOptions`：Bearer/Raw（行 420）
- 首次接入表单：`frontend/src/components/NewApiSubscriptionForm.vue`（两步 verify→provision）
- 订阅中心入口：`frontend/src/views/SubscriptionsView.vue`（`selectedProvider==='new-api'` 行 23）+ `SubscriptionProviderGrid.vue`（行 116-122）
- 快速添加入口：`frontend/src/components/QuickAddChannelForm.vue`（`NEW_API_PROVIDER_VALUE='__new_api__'` 行 264）+ `subscriptions/NewApiQuickAddDialog.vue`

## 3. 数据模型：账号/key/凭证/渠道映射

### 3.1 订阅画像 `SubscriptionProfile`（`internal/autopilot/subscription_profile.go:15`）
new-api 专用字段（行 62-94）：`Provider="new_api"`、`BaseURL`、`AccessToken`（敏感，明文序列化进 `profile_json`，API 响应脱敏）、`UserID`、`AuthTokenMode`、`ProxyURL`、`ProxyPreferDirect`（订阅级代理：绑定/同步经代理访问，直连优先开启时先直连、失败回退代理）、`ProvisionKeyName`、`ProvisionGroup`、`ProvisionGroupRatio`、`MaxGroupMultiplier`（接入时初始渠道上限的记录；运行时真源是渠道级 `UpstreamConfig.MaxGroupMultiplier`，仅作建 key 阈值回退与展示）、`ProvisionModels`、`ProvisionedTokenID`、`ProvisionedKeys []NewApiProvisionedKey`、`AvailableModels`、`GroupMultipliers map[string]float64`、`Accounts []NewApiAccount`（账号级 `ProxyURL/ProxyPreferDirect` 为空时继承订阅级）。计费条款为四字段币种/金额模型 `PaymentAmount/PaymentUnit/CreditAmount/CreditUnit`（行 37-40；旧 `RechargeMultiplier` 单字段已移除，`a96098da`，旧 JSON 键加载时自动忽略）。

- `NewApiProvisionedKey`（行 104）：`Name, Group, GroupMultiplier, TokenID, KeyUID`（**无明文 key**）
- `NewApiAccount`（行 114）：`AccountUID, AccessToken, UserID, AuthTokenMode, DisplayName, Balance, Status, ProvisionedKeys[], LastSyncError, LastCheckedAt, CreatedAt`

持久化：`SubscriptionStore`（行 159），SQLite 表 `autopilot_subscriptions`（`subscription_uid` 主键 + `profile_json` JSON 列，行 214），内存 cache + `Patch` 乐观锁（行 317，`Version` 字段）。与 `ProfileStore` 共享同一 `*sql.DB`（`manager.go:187` `NewSubscriptionStoreWithDB`）。

### 3.2 渠道侧凭证元数据 `config.APIKeyConfig`（`internal/config/config.go:217`）
key 明文只存在渠道配置，new-api 相关字段：`QuotaGroup`、`GroupMultiplier *float64`、`MaxGroupMultiplier *float64`、`MultiplierSource("new_api")`、`MultiplierUpdatedAt/ExpiresAt`、`MultiplierSyncStatus`、`MultiplierSyncError`、`SourceSubscriptionUID`、`SourceRemoteTokenID`、`KeyUID`。

### 3.3 渠道 `config.UpstreamConfig`（`config.go:19`）
new-api 渠道标记：`AutoManaged=true`、`AutoManagedKind="new_api"`（行 128，取值 `"" | "generic" | "new_api"`）、`OriginType="relay"`、`OriginTier="second"`。

`BalanceCheck *ChannelBalanceCheckConfig`（`config/balance_check.go`，2026-10-03）：渠道级上游余额探测配置；`AutoManagedKind="new_api"` 且未显式配置时默认开启 auto（探测端点与聚合语义见 §4.6）。

### 3.4 映射关系
```
SubscriptionProfile (subscriptionUid, provider=new_api, accessToken, accounts[])
  ├── GroupMultipliers {group: ratio}          ← FetchGroups 快照
  ├── ProvisionedKeys[] (主账号, tokenId→group)
  ├── Accounts[].ProvisionedKeys[] (每账号, name 加 accountUID 前缀)
  └── LinkedChannelUIDs[] ──────────────────┐
                                            ▼
UpstreamConfig (channelUid, autoManagedKind=new_api)
  ├── MaxGroupMultiplier (渠道级分组倍率上限，唯一运行时真源；接入阈值作为初始值写入)
  └── APIKeyConfigs[] (key 明文 + KeyUID
        + SourceSubscriptionUID = subscriptionUid
        + SourceRemoteTokenId  = tokenId          ← ownership 绑定
        + QuotaGroup / GroupMultiplier            ← key 级上限已废弃（渠道级统一）
        + MultiplierSource=new_api / SyncStatus / ExpiresAt)
```
`KeyUID = StableKeyUID(subscriptionUID, tokenID)`（`newapi_subscription_sync_service.go:775`，sha256 前 8 字节，前缀 `kuid_`）。tokenID 站点级唯一，主账号与多账号 key 共存不撞号。

渠道编辑保存时 `mergeAPIKeyConfig`（`config.go:785`，787db651）对 `MultiplierSource=new_api` 的既有 key 回填 `SourceSubscriptionUID`/`SourceRemoteTokenID`（客户端表单不携带这两个托管身份字段）——此前不回填，编辑换 key 会切断渠道与订阅的关联，且 `BuildChannelView` 的 `subscriptionUid` 正是从 `APIKeyConfigs[].SourceSubscriptionUID` 暴露的，关联一断前端账号面板即拿不到订阅 UID。非托管 key 的显式清空（`handlers_key_multiplier.go` 脱离同步管理场景）不受回填影响。

### 3.5 同步服务（`internal/autopilot/newapi_subscription_sync_service.go`）

`NewApiSubscriptionSyncService`（行 59）负责 new-api 订阅的周期性余额、分组倍率与可用模型同步，使用 per-uid 锁（`lockForUID`）。

- 后台循环：`Start`（行 159）启动 30 分钟周期 ticker（`newApiSyncDefaultInterval` 行 93），tick 到达时调用 `SweepAll` 并发刷新所有 new-api 订阅。`Stop` 优雅停止循环。初始启动时还会通过 `SyncAllNewAPIAsync` 先做一次性全量同步。
- `SyncNow`（行 271）：verify → FetchGroups（校验非负有限，`finiteNonNegative`）→ FetchModels → `Patch` 回写余额/分组/模型/KeyUID/ratio → `reconcileChannels`（行 620）把 desired key 元数据合并进关联渠道的 `APIKeyConfigs`（ownership 冲突→`relink_required`）→ 模型哈希变化触发 Discovery；主账号凭证为空时（行 291，账号平权）跳过站点级同步仅同步子账号
- `reconcileNewApiConfigs`（行 645）：按 `SourceRemoteTokenID`/`KeyUID` 匹配，跨订阅 ownership 冲突返回 conflict。匹配维度只有这两个字段——渠道 key 被误删后常规 reconcile 无法找回，由 `healMissingProvisionedKeys`（行 867）自愈：SyncNow/syncOneAccount 在 reconcile 后检查关联渠道是否缺失 desired key，缺失时按 tokenID 分页拉远端 token 列表（`ListTokens`），掩码 key 经揭示端点（`GetTokenKey`）换回明文并规范 `sk-` 前缀，再走 `injectProvisionedKeys` 重建 config；远端 token 也已删除的项跳过，绝不注入空 key（测试 fake 未实现 `newApiTokenHealer` 接口时自愈自动跳过）
- `buildDesiredForKeys`：计算每 key 的 syncStatus（`fresh`/`remote_group_missing`）+ TTL（`newApiSyncTTL=35m` 行 26）。`over_limit` 不在 desired 层预判，而是在 `reconcileNewApiConfigs`/`injectProvisionedKeys` 写入渠道配置时按**该渠道**的 `UpstreamConfig.MaxGroupMultiplier` 实时判定（多渠道上限不同也各自正确）；desired 构造时的展示上限经 `linkedChannelMaxGroupMultiplier` 取关联渠道值（回退 profile 接入初始值）
- `injectProvisionedKeys`（行 799）/`ReconcileProvisioned`（行 972）/`ReconcileAccountProvisioned`（行 991）：provision/加账号/自愈后把明文 key 注入渠道；注入的明文同时并入渠道 `APIKeys`（调度与 keypool 候选只遍历 `APIKeys`，仅写 configs 的 key 不参与调用）
- `RemoveAccountKeysFromChannels`（行 516）：删账号/删主账号时剔除渠道 key
- 状态常量（行 17-24）：`fresh/over_limit/sync_error/relink_required/stale/remote_group_missing`

## 4. 与 autopilot/scheduler 的集成点

### 4.1 渠道纳入调度（provision 落地）
`handleNewApiProvision`（`handlers_newapi.go:427`）第 5 步：
- 同站点合并 `findNewApiMergeTarget`（行 317，规范化 baseURL 去尾 `/`、忽略大小写、跳过带 `providerId` 的渠道、优先 active），合并时保留已有纯 key、去重追加新 key、补 `AutoManagedKind="new_api"`；渠道级 `MaxGroupMultiplier` 仅在目标渠道尚未设置时写入本次接入阈值（不覆盖用户已调过的渠道上限）
- 否则新建，`kindToDefaultServiceType`（`handlers_auto_managed.go`）推导 serviceType，`GenerateChannelUID` 生成稳定 UID，接入阈值直接作为新建渠道的 `MaxGroupMultiplier` 初始值
- 第 6 步 `Store.LinkChannel`，第 7 步 `deps.Runner.TriggerDiscovery`（`auto_discovery.go:297`）

### 4.2 分组安全闸门 `resolveNewApiProvisionGroups`（`newapi_group_guard.go:24`）
`DefaultNewApiMaxGroupMultiplier=1.0`（行 12）。只为 `ratio <= maxMultiplier` 的合格分组建 key，一个 key 固定绑一个上游分组；`defaultNewApiProvisionKeyNameForGroup`（行 96）给 key 名加分组后缀。

**当前统一语义**：
- 所有入口都先 verify，获取上游 `groups` 与 `availableModels`。
- 空分组请求不再猜 `default`；统一按“全部合格分组”路径处理。
- 只有调用方**显式**传 `provisionGroup` 时才进入单分组模式。
- `groupFetchError`、无合格分组、倍率阈值非法时均阻断，不创建任何部分 key。

### 4.3 调度期成本闸门 `IsAPIKeyGroupMultiplierAllowed`（`internal/config/key_group_multiplier.go`）
`GetNextAPIKey`（`config.go`）与 keypool 选 key 时调用 `EvaluateAPIKeyMultiplierEligibility(cfg, channelMax, now)`：第二参数是渠道级 `UpstreamConfig.MaxGroupMultiplier`（nil=不启用闸门），`GroupMultiplier > channelMax` 判 `over_group_limit`；`new_api` 源还需 `SourceSubscriptionUID`+`SourceRemoteTokenID` 有效、status=`fresh` 且未过期，否则 `stale/over_limit/relink_required` 一律不参与调度。这是把 new-api 分组倍率纳入调度的核心闸门。

### 4.4 SmartRouter 候选过滤/成本评分
`bootstrap_scheduler.go` `SetCandidateFilterProvider` 注入 `SmartRouter.CandidateFilterForWithActual`。`buildChannelEntry` 在有汇率图 + 到账规则时，用带 `SourceSubscriptionUID` 且 `GroupMultiplier` 的 key 配置调用 `ResolveEffectiveCostUSD`（`key_endpoint_profile.go:360`）算真实 effective USD 成本，替代标价。`EffectiveMultiplier = GroupMultiplier × TimeMultiplier × PaymentAmount×PaymentUSDPrice / (CreditAmount×CreditUSDPrice)`（行 416）。到账规则有两条入口：**订阅级** billing-terms（`PaymentAmount/CreditAmount` 四字段）与**渠道级**计费四字段（`UpstreamConfig.ChannelPayment*/ChannelCredit*`，`49f28b3e`），后者使无订阅 billing-terms 的渠道也能算 effective cost。

### 4.5 Discovery/画像链路
`TriggerDiscovery`→`runDiscovery`（`auto_discovery.go:360`）→`discoverEndpoints`（行 527）/`probeEndpoint`（行 899）拉 `/v1/models`（统一走 `utils.FetchUpstreamModels`：Anthropic 风格端点首发即带 Claude Code 探针头；其余裸发、命中客户端指纹风控特征时带探针头重试一次并回写 `LearnedClientFingerprint`，见 `cross-module-integration.md` §3.6），`parseModelsDeclaredEndpointTypes`（行 1119）解析 new-api 的 `supported_endpoint_types`（`protocolForEndpointType` 行 1154：openai→chat / anthropic→messages / gemini / openai-response→responses；仅作探测排序提示不做过滤），`discoverEndpointProtocols`（`protocol_discovery.go`）逐模型多协议实测，`writeProfileForEndpoint`（`auto_discovery.go:1237`）写 `KeyEndpointProfile`。`buildEndpointInventory`（`endpoint_inventory.go`）建 endpoint 清单供画像与限速。

### 4.6 余额刷新路径
`handleRefreshSubscription`（`handlers_subscription.go:376`）对 `provider=="new_api"` 走 `syncService.SyncNow`，其他 provider 走 `SubscriptionRefreshWorker`（`subscription_refresh_worker.go`）。注意 `IsAutoRefreshSupported`（`subscription_balance_fetcher.go`）白名单只含 openai/anthropic/google，**不含 new_api**（见第 6 节）。

**渠道 key 级余额探测（2026-10-03 起，与订阅级互补）**：`internal/balanceprobe`（探测器 `upstreamprobe/balance.go`）用渠道持有的推理 key 直接调上游余额端点，不走 accessToken/`/api/user/self`。new-api 系首选 `newapi_token` 端点 `GET /api/usage/token/`（`TokenAuthReadOnly` 鉴权：Bearer key，宽松只验 key 存在；恒 key 级，返回原始 quota 点 `total_granted/total_used/total_available/unlimited_quota`）；`newapi_billing`（`/v1/dashboard/billing/subscription`+`/usage` 对）作兜底。newapi 订阅托管渠道（`AutoManagedKind="new_api"`）默认开启 auto 探测（间隔默认 6h、硬下限 30min，配置见 `config/balance_check.go`），点数按 `QuotaPerUnit`（默认 500000）换算 USD；聚合 Σ 写配额真相 provider_api 级（明细落 key_health check_kind=balance）。管理端点 `GET/POST /api/{type}/channels/:id/balance(/check)`；渠道编辑对话框「探测上游余额」卡片可手动指定类型/换算比率并查看 per-key 结果。详见 `healthcheck.md` §10 与 `quota-truth-scheduling.md` §7.1。

## 5. 前端编辑弹窗字段/校验/API 调用链路

### 5.1 弹窗接入位置
`frontend/src/components/EditChannelModal.vue:118-136` 在 `isNewApiChannel || isGenericAutoManagedChannel`（行 443-451，判断逻辑：`autoManagedKind==='new_api'` 或 `originType==='relay' && autoManaged && !providerId`；generic 为 `autoManaged && !providerId && autoManagedKind!=='new_api'`）时渲染 `NewApiAccountPanel`，传 `subscription-uid / channel-name / base-url / channel-uid / channel-kind / is-generic / auto-managed-kind / channel-proxy-url / channel-proxy-prefer-direct`（后两项让面板的绑定/校验/同步复用渠道「代理通道」，面板自身不配置代理）。

### 5.2 字段与校验
- 首次接入 `NewApiSubscriptionForm.vue`：
  - 表单字段：`baseUrl, accessToken(password), userId(必填，标准 new-api 强制 New-Api-User 头), authTokenMode, proxyUrl, proxyPreferDirect, displayName`；provision 步 `subscriptionUid, channelKind, channelName, maxGroupMultiplier(number,min=0), notes`（代理设置在 verify 步录入，provision 预填继承并写入所建渠道）
  - 校验：`canVerify`（baseUrl+accessToken+userId 非空，行 287；userId 自 2026-08-30 起必填）、`canProvision`（subscriptionUid+channelKind + `maxGroupMultiplierValid` + `eligibleGroupItems.length>0`，行 288）
  - 合格分组过滤：`eligibleNewApiGroups`（`utils/newApiGroups.ts:12`）与 `isValidNewApiGroupMultiplier`（行 8，非负有限），前端与后端 `resolveNewApiProvisionGroups` 语义一致
  - provision 固定发 `provisionAllEligibleGroups=true`（行 318）
- 已有渠道绑定 / 追加账号（`NewApiAccountPanel.vue`）：
  - userId 同样必填（2026-08-30）：`canBindNewApi` 与添加账号按钮均要求 userId 非空
  - **同样先 verify，再使用 verify 返回的 `groups` + `availableModels` 计算全部合格分组**
  - 绑定主账号与追加账号都显式传 `provisionAllEligibleGroups=true`、`maxGroupMultiplier`（当前默认 1.0）与 `availableModels`
  - `groupFetchError`、无合格组、verify 失败时均阻断提交，不允许 fallback 到 `default`
- key 倍率编辑 `edit-channel/ApiKeyManagementSection.vue`（行内展开、变更即保存）：策略选择/倍率定稿 → `patchKeyMultiplier`；`new_api` 源 key 的 `groupMultiplier` 后端拒绝手改（`handlers_key_multiplier.go:158`，409 Conflict「new_api key 的 groupMultiplier 由远端同步，不能手动修改」），Key 级上限字段已随渠道级统一而移除，行内面板仅允许改 `consumptionPolicy`（渠道上限在渠道编辑计费区调整）；状态色 `multiplierStatusColor`/`multiplierStatusLabel`（`utils/subscriptionBilling.ts`）。定位符为 `KeyUID`，非 new-api key（托管账号手工 key 等）无 `KeyUID` 时后端兜底匹配 `CredentialUID`、前端行数据以 `keyUid ?? credentialUid` 回填，倍率编辑入口对全部可编辑 key 可见（`bb736eef` 及后续兜底修复）。

### 5.3 API 服务层（`frontend/src/services/api.ts`）
`verifyNewApiSubscription`(1473) / `provisionNewApiSubscription`(1481) / `updateNewApiCredentials`(1490) / `getSubscriptionAccounts`(1497) / `addSubscriptionAccount`(1502) / `deleteSubscriptionAccount`(1510) / `deleteSubscriptionPrimaryAccount`(1517，账号平权) / `refreshSubscriptionAccount`(1524) / `updateSubscriptionAccountCredentials`(1531，62d2d46d，面板已不再调用) / `refreshSubscription`(1436) / `patchKeyMultiplier`(1460) / `linkSubscriptionChannel`(1422) / `unlinkSubscriptionChannel`(1429)。类型定义在 `api-types.ts`：`NewApiVerifyRequest/Response`、`NewApiProvisionRequest/Response`、`NewApiProvisionedKey(Info)`、`NewApiAccountItem`、`NewApiCredentialsUpdateRequest`、`NewApiKeyStatus`、`NewApiSyncResult`、`APIKeyConfig`。

### 5.4 调用链路（统一组计划）
1. `verify`：输入 `baseUrl/accessToken/userId(必填)/authTokenMode`，后端拉 `GET /api/user/self`、`GET /api/user/self/groups`、`GET /api/user/models`，返回 `groups + availableModels`。
2. `plan`：前端按统一倍率阈值（默认 1.0）用 `eligibleNewApiGroups` 计算全部合格分组；若 `groupFetchError`、无合格组或阈值非法，则直接阻断。
3. `provision / add-account`：
   - 新增订阅：`NewApiSubscriptionForm.handleProvision` → `api.provisionNewApiSubscription`
   - generic 渠道绑定：`NewApiAccountPanel.bindNewApi` → 先 verify，再 `api.provisionNewApiSubscription`
   - 追加账号：`NewApiAccountPanel.handleAddAccount` / Desktop 对应入口 → 先 verify，再 `api.addSubscriptionAccount`
4. 所有入口都显式发送：`provisionAllEligibleGroups=true`、`maxGroupMultiplier`、`availableModels`；空请求不再由后端猜 `default`。
5. 后端统一执行：`verify→FetchGroups→resolveGroups→provisionNewApiGroupKeys→建 profile/账号→建/并渠道→LinkChannel→TriggerDiscovery→SyncService.ReconcileProvisioned`，并返回实际接入的 key/group 列表。

### 5.5 i18n
locale 键在 `frontend/src/locales/{zh-CN,en,id}.json`，前缀 `subscription.newApi.*`（zh-CN 行 1201-1275，含 787db651 新增的 `primaryBadge`/`primaryAccountUnavailable`/`subscriptionNotFound`/`subscriptionUnavailable` 与账号平权新增的 `deletePrimaryAccount`/`primaryAccountRemoved`）与 `subscription.keyMultiplier.*`（行 1494-1569），`autopilot.quickAdd.provider.newApi`（行 1466）。

## 6. 运行时行为与安全边界

- **定期同步**：`NewApiSubscriptionSyncService` 启动时异步同步已有订阅，随后按 30 分钟周期刷新。通用 `SubscriptionRefreshWorker` 不负责 new-api 的这条同步路径。
- **令牌存储**：`SubscriptionStore.encryptProfile` 使用 AES-256-GCM 加密订阅及账号的 `AccessToken`，读取时解密并兼容旧明文。生产部署应设置 `CCX_SECRET_KEY`；未设置时使用机器派生密钥，迁移数据目录到其他机器可能无法解密。`BillingAPIKey` 不在这段加密逻辑中，不能据此认为所有字段都已加密。
- **渠道关联**：订阅中心提供 link/unlink 操作。Key 合并会保留 `SourceSubscriptionUID` 等身份字段；同步还会恢复误删的接入 Key。前端缺少订阅 UID 时，按 `newapi-${channelUid}` 查找的逻辑仍作为兼容路径保留。
- **配额与成本**：余额探测的 `BalanceCheck.QuotaPerUnit` 换算，与用于成本排序的汇率及充值条款是两条独立路径。余额可显示不代表实际 USD 成本已能计算，详见 §8.3。
- **账号用量**：`NewApiAccountItem` 返回余额、状态、已创建 Key 和检查时间，不提供独立的 `usedQuota` 字段。调用方不能从该接口推导每个账号的累计用量。

旧缺陷、修复提交和分析过程见[历史记录](https://github.com/soulteary/yunqi/blob/main/archive/specs/new-api-integration-history.md)。

## 7. 布局示意图

### 7.1 整体数据流

```text
[用户输入 BaseURL + AccessToken]
           │
           ▼
   POST /api/subscriptions/newapi/verify
           │
           ▼
   ┌─────────────────┐
   │  NewApiAdapter  │ ──→ GET /api/user/self
   │   (Verify)      │ ──→ GET /api/user/self/groups
   └─────────────────┘ ──→ GET /api/user/models
           │
           ▼
   返回账号/分组/模型预览（不落库）
           │
           ▼
   POST /api/subscriptions/newapi/provision
           │
           ▼
   ┌─────────────────┐
   │ resolveNewApi   │  过滤 ratio <= maxGroupMultiplier 的合格分组
   │ ProvisionGroups │
   └─────────────────┘
           │
           ▼
   ┌─────────────────┐
   │ provisionNewApi │  为每个合格分组建 key（或复用同名 key）
   │    GroupKeys    │  namePrefix = accountUID + "-"
   └─────────────────┘
           │
           ▼
   ┌─────────────────┐     ┌─────────────────┐
   │ 建 Subscription │────→│ 建/并 Upstream  │
   │    Profile      │     │     Config      │
   └─────────────────┘     └─────────────────┘
           │                       │
           ▼                       ▼
   ┌─────────────────┐     ┌─────────────────┐
   │  LinkChannel    │────→│ TriggerDiscovery │
   └─────────────────┘     └─────────────────┘
           │                       │
           ▼                       ▼
   ┌─────────────────┐     ┌─────────────────┐
   │ Reconcile       │────→│  AutoDiscovery  │
   │ ProvisionedKeys │     │     Runner      │
   └─────────────────┘     └─────────────────┘
```

### 7.2 账号-Key-渠道映射

```text
SubscriptionProfile (subscriptionUid)
  ├── AccessToken (主账号；账号平权：DELETE accounts/primary 清空，
  │                无主凭证时添加账号自动提升为新主账号)
  ├── GroupMultipliers {group: ratio}
  ├── ProvisionedKeys[]
  │     ├── Name, Group, TokenID, KeyUID
  │     └── ...
  └── Accounts[]  (子账号；提升为主账号的条目会从列表移除)
        ├── AccountUID
        ├── AccessToken
        ├── ProvisionedKeys[]
        │     ├── Name (accountUID + "-group")
        │     └── TokenID, KeyUID
        └── ...

UpstreamConfig (channelUid, autoManagedKind=new_api)
  ├── APIKeys[]  (调度池；注入的明文 key 一并并入)
  ├── MaxGroupMultiplier（渠道级上限，接入阈值作为初始值；调整走渠道编辑）
  └── APIKeyConfigs[]
        ├── Key (明文)
        ├── KeyUID = StableKeyUID(subscriptionUID, tokenID)
        ├── SourceSubscriptionUID = subscriptionUid
        ├── SourceRemoteTokenID = tokenID
        ├── QuotaGroup / GroupMultiplier
        └── MultiplierSource = new_api / SyncStatus / ExpiresAt
```

### 7.3 同步服务状态机

```text
[SyncNow]
    │
    ├─ 主账号凭证为空（账号平权，已删除）→ 仅 syncAccounts，不视为失败
    │
    ├─ Verify ──→ 余额/账号信息
    ├─ FetchGroups ──→ GroupMultipliers
    ├─ FetchModels ──→ AvailableModels
    │
    ▼
[reconcileChannels]
    │
    ├─ 按 SourceRemoteTokenID / KeyUID 匹配
    ├─ ownership 冲突 → relink_required
    └─ 合并 desired key 元数据进 APIKeyConfigs
    │
    ▼
[healMissingProvisionedKeys]
    ├─ 渠道缺失 desired key → 按 tokenID 拉远端列表
    │   └─ 掩码经揭示端点换明文 → injectProvisionedKeys 重建
    └─ 远端 token 也已删除 → 跳过，不注入空 key
    │
    ▼
[状态计算]
    ├─ fresh: 正常
    ├─ over_limit: ratio > 渠道级 UpstreamConfig.MaxGroupMultiplier（写入渠道配置时按各渠道判定）
    ├─ stale: 超过 TTL 未同步
    ├─ relink_required: ownership 冲突
    └─ remote_group_missing: 远端分组已删除
```

## 8. 身份、并发与成本计算

### 8.1 渠道身份与逻辑卡刷新

新建 new-api 渠道时，`StableAccountUID(subscriptionUID)` 从订阅 UID 派生稳定账号身份。同一订阅的多协议渠道可据此归到同一逻辑卡；配置层的 `deriveNewApiAccountUID` 负责兼容已有数据，两处派生算法需保持一致。

`ConfigManager.saveConfigLocked` 统一重建 `LogicalChannels` 并回写逻辑身份。因此 provision/add-account 保存后会刷新逻辑卡，不需要重启服务。物理渠道合并仍由 `findNewApiMergeTarget` 选择目标，逻辑归组由 `RebuildLogicalChannels` 处理；修改任一规则时，应同时检查站点归一化、渠道类型和账号身份是否一致。

### 8.2 订阅级与站点级互斥

`withNewAPIProvisionUIDLock` 优先使用同步服务的订阅级锁；未装配同步服务时，使用进程内 per-UID 锁。添加账号路径同样复用 `SyncService.LockForUID`，避免与同一订阅的 provision/同步交错写入。

远端 Key 查重和创建另受站点级锁保护，锁键由归一化站点地址与用户 ID 组成；缺少用户 ID 时使用 access token 区分账号。不同订阅不再共用一把全局 provision 锁，但同一订阅的慢速远端操作仍可能阻塞该订阅的其他修改。

### 8.3 汇率与充值条款缺失时的处理

路由期先计算模型标价，并应用时间倍率与分组倍率。订阅具备 `PaymentAmount/CreditAmount` 等计费条款、汇率图也可解析时，才以实际充值成本覆盖估算。汇率图构建失败、单位不在图中或条款缺失时，保留标价估算；`logExchangeRateBuildErrorOnce` 和 `logEffectiveCostMissingOnce` 分别按版本或订阅/原因去重记录日志。

`ExchangeRateQuotesConfigured` 区分旧配置未填汇率与用户显式清空汇率：前者可补默认值，后者不建图并回退标价。汇率单价和版本应来自同一 `ExchangeRateSnapshot`。

请求期 `buildRequestCostContext` 的计算顺序为：

1. 记录标价 USD 成本及汇率快照版本。
2. 渠道的充值币种/金额、到账币种/金额四字段齐备且金额大于零时，调用 `ResolveEffectiveCostUSD`，按 `(Payment × 充值币价) / (Credit × 渠道币价)` 计算有效倍率。
3. 四字段不足但配置了 `CostMultiplier` 时，使用该倍率。
4. 缺乏有效证据时保留不可用原因，不把未知成本当成免费。

new-api 接入时不会自动推断用户的充值金额；需要通过 billing-terms 或渠道计费设置提供。非默认计价单位还需要匹配的汇率。订阅级条款的路由期估算和渠道级条款的请求期快照不能混为同一份账单证据。
