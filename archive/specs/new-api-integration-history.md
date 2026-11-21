# New-API 集成历史分析与修复记录

本文件保留 `cd59435` 中 `docs/specs/new-api-integration.md` 第 6、8 节原文。已修复问题、旧方案、行号和待办判断反映当时记录，不作为当前实现依据。现行说明见[New-API 集成规格](../../docs/specs/new-api-integration.md)。

## 6. 可能缺失的边界处理与文档

1. **new_api 无周期性自动余额刷新**~~（旧声明，已修正）~~：`NewApiSubscriptionSyncService` 已提供 30 分钟周期的后台同步循环。启动时 `SyncAllNewAPIAsync` 做一次全量同步，随后 `Start` 启动 30 分钟 ticker 并调用 `SweepAll` 并发刷新每个 `provider=new_api` 订阅。`SubscriptionRefreshWorker.refreshAll` 仍按 `IsAutoRefreshSupported` 白名单跳过 new_api，但 new-api 的定期同步由专用 sync service 承担。

2. ~~**AccessToken 明文落库**~~ ✅ **已修复**（2026-08-09，提交 `d876784d`）：`SubscriptionProfile.AccessToken` 与 `Accounts[].AccessToken` 落库前经 AES-256-GCM 加密（`encryptProfile`/`decryptProfile`），读库透明解密并向后兼容旧明文（无加密标记前缀时原样返回）。密钥优先读环境变量 `CCX_SECRET_KEY`，未设置则退回机器派生（hostname/GOOS/GOARCH/salt）。**注意**：机器派生密钥下若迁移数据目录到另一台机器且未设 `CCX_SECRET_KEY`，旧加密 token 将无法解密——生产部署应显式配置 `CCX_SECRET_KEY`。`BillingAPIKey` 暂未加密（可复用同一机制扩展）。

3. ~~**专项 spec 文档缺失**~~ ✅ **已修正**：`docs/specs/new-api-integration.md` 已存在，`docs/specs/README.md:12` 索引链接有效，不再悬空。

4. ~~`link/unlink UI 缺失`~~ ✅ **已修复**：订阅中心已提供绑定/解绑入口，调用 `POST /api/subscriptions/:uid/link|unlink` 与前端 `api.linkSubscriptionChannel/unlinkSubscriptionChannel`。

5. **`quota` 货币换算依赖手工汇率**：new-api `quota` 非 USD，effective 成本需 `ExchangeRateQuotes` + 订阅 `PaymentAmount/CreditAmount`（`smart_router.go:1356-1378`）齐备才生效；缺任一项则回退标价 USD 成本，new-api quota 无法参与真实成本排序（静默降级，无用户提示）。注意这与渠道 key 级余额探测（§4.6）的换算是两条独立链路：探测余额数字用渠道级 `BalanceCheck.QuotaPerUnit`（默认 500000，new-api 官方默认）直接换算，不依赖汇率图。

6. **`NewApiAccountItem.usedQuota` 前端类型有、后端不填**：`api-types.ts:1446` 定义了 `usedQuota`，但后端 `handlers_subscription_accounts.go` 构造 `NewApiAccountItem` 时（行 184/244/391）从不设置该字段——per-account 已用额度不可见。

7. **合并渠道的 kind 冲突**：`findNewApiMergeTarget` 只按 baseURL+kind 匹配，若同站点用户先建了纯 key 渠道且 serviceType 与 new-api 推导不一致，合并后 `serviceType` 沿用旧渠道（不校验），可能与 new-api 分组语义错配（无显式告警）。

8. ~~**new-api 占位模型 503 误判端点不可用**~~ ✅ **已修复**（2026-08-19，提交 `cd6f93d7`）：key 验证（`verify_endpoint.go` `verifyJSONPostEndpointWithPolicy`）在 `acceptValidationError` 分支除 400/422 外新增识别 503 + `{"error":{"code":"model_not_found"}}`（或 message 含 `no available channel for model`），判定鉴权通过——new-api/one-api 对无渠道占位模型返回 503，此前被误判「端点不可用」导致有效 key 无法添加。

9. ~~**编辑换 key 切断渠道与订阅的关联**~~ ✅ **已修复**（2026-08-27，提交 `787db651`；2026-08-28 补自愈）：前端编辑保存时表单提交的 `apiKeyConfigs` 不含 `sourceSubscriptionUid`（且无 `keyUid` 的身份骨架条目会被 `channelPayload.ts` 的 filter 剔除），后端合并后旧 config 的 `SourceSubscriptionUID`/`SourceRemoteTokenID` 全部丢失 → `BuildChannelView` 不再暴露 `subscriptionUid` → 编辑渠道对话框主账号面板空白、添加账号静默无响应；且 `reconcileNewApiConfigs` 只按 tokenID/KeyUID 匹配，常规同步无法自愈。修复：`mergeAPIKeyConfig` 对 `MultiplierSource=new_api` 的既有 key 回填托管身份字段（止血）；前端按 `newapi-${channelUid}` 兜底推导订阅 UID（恢复）；订阅 404/未就绪给出提示而非静默。后续补齐：渠道侧误删的自动接入 key 由 `healMissingProvisionedKeys` 在每次同步时按 tokenID 从远端取回明文自愈重建；`injectProvisionedKeys` 注入的明文同时并入渠道 `APIKeys`（此前仅写 configs、新 key 实际不可调度）。

## 8. 待补充项详解

### 8.1 与 LogicalChannel 归组逻辑的交互

> **更新（2026-08-09，提交 `d876784d`）**：本节"主缺口①（逻辑卡视图不刷新）"已修复——`saveConfigLocked` 现在统一调用 `RebuildLogicalChannels`，provision/add-account 落盘后逻辑卡即时刷新，无需进程重启。下文缺口①保留作为背景记录，缺口②（AccountUID 空）仍待处理。

**当前实现**

New-api provision 建渠道时（`handlers_newapi.go:591-605`），构造的 `UpstreamConfig` **只设置** `AutoManaged=true`、`AutoManagedKind="new_api"`、`ChannelUID`、`BaseURL/BaseURLs`，**未设置 `AccountUID`，也未设置 `LogicalChannelUID`/`LogicalName`**。合并路径（`updateChannelForKind` → `UpstreamUpdate`）同样只更新 `APIKeys/APIKeyConfigs/AutoManaged*`，不碰账号/逻辑身份字段。

归组的真相来源是 `RebuildLogicalChannels`：除加载时的 `ensureLogicalBackfill` 外，现已在 `saveConfigLocked` 统一收口（见更新说明），运行期物理渠道变更后逻辑卡即时同步。`AccountUID` 回填仍是独立缺口。

`AccountUID` 与 `LogicalChannelUID` 的关系（`logical_channel.go:66-111、467-499`）：
- `logicalChannelGroupKeyFrom` 用 `(accountUID, providerID, siteIdentity)` 三元组归组。
- `shouldGroupLogical`：① 同 `accountUID` 直接合并（最高优先，`convergeLogicalByAccount` 还会强制把同账号收敛到单一 canonical 卡）；② 同 providerID + 同站点合并；③ 无 provider 无 account 的手工渠道 + 同站点合并。
- New-api 渠道 `ProviderID` 为空、`AccountUID` 为空，只能靠**规则③（同站点手工渠道合并）**归组。而 provision 的 `findNewApiMergeTarget` 恰好用相同的“同站点 + 无 providerID”语义在物理层面提前合并，二者语义一致但**各算各的**。

**缺口分析**

1. **新建渠道后逻辑卡视图不刷新（主缺口）。** provision/add-account 只改物理数组，`LogicalChannels` 聚合列表与物理渠道的 `LogicalChannelUID` 字段直到进程重启才由 `ensureLogicalBackfill` 回填。运行期 `GET /api/logical-channels` 会返回陈旧列表——新接入的 new-api 渠道要么不出现在逻辑卡里，要么 `logicalChannelUid` 为空。

2. **AccountUID 空导致多账号 new-api 无法归到同一逻辑卡。** 同一 new-api 订阅下 add-account 把多个账号的 key 并入同一 `LinkedChannelUIDs` 渠道，但**渠道 `AccountUID` 始终为空**。若同一订阅将不同 kind（messages/chat/responses）建到不同站点渠道，规则③（同站点）可能把它们拆成多张逻辑卡，而没有 `AccountUID` 作为跨站点/跨协议的强合并键——不像自动托管 provider 那样能靠 `AccountUID` 收敛。

3. **合并目标选择口径与归组口径存在潜在分叉。** `findNewApiMergeTarget` 跳过任何带 `ProviderID` 的渠道，而 `shouldGroupLogical` 规则②允许同 provider 同站点合并。若同站点既有 new-api 渠道又有带 providerId 的模板渠道，provision 会新建独立渠道，但逻辑归组阶段规则可能有不同判断（虽然 new-api 渠道无 providerId 走规则③，不会与规则②渠道合并，当前行为一致，但两处口径独立维护，易随一方演进而漂移）。

**建议方案**

- **在物理渠道增删改后触发逻辑卡重建。** 最小改动：在 new-api provision/merge/add-account 完成、`LinkChannel` 之后，显式调用一次 `RebuildLogicalChannels`（需在 `ConfigManager` 暴露一个加锁的公开方法，如 `RebuildLogicalChannelsAndSave()`）。或在 `AddUpstream`/`UpdateXxxUpstream`/`AddChatUpstream` 等 `saveConfigLocked` 前统一插入 `RebuildLogicalChannels(&cm.config)`。
- **给 new-api 渠道分配稳定 `AccountUID`。** provision 时按 `subscriptionUID`（或 `subscriptionUID + baseURL 站点`）派生一个稳定 `AccountUID` 写入 `UpstreamConfig.AccountUID`，让同订阅多协议/多站点渠道靠 `shouldGroupLogical` 规则①和 `convergeLogicalByAccount` 强制收敛到单张逻辑卡，与自动托管 provider 语义对齐。注意 `syncManagedAccountsFromChannels` 要求 `AutoManaged && AccountUID != "" && ProviderID != ""` 才建 ManagedAccount，new-api 渠道 `ProviderID` 为空不会误入托管账号池，是安全的。
- **统一合并口径。** 将 `findNewApiMergeTarget` 的“同站点 + 无 providerId”判定抽取为与 `logicalChannelGroupKeyFrom`/`shouldGroupLogical` 共享的辅助函数，避免两处独立维护站点身份归一化逻辑（当前 `normalizeNewApiChannelURL` 仅做 `TrimRight("/")`，而归组用 `utils.BaseURLSiteIdentities`，归一化强度不同，是漂移隐患）。

### 8.2 多账号并发 provision 锁粒度

**当前实现**

`newAPIProvisionMu` 是**进程级全局互斥锁**（`sync.Mutex`，非 per-subscription）。`handleNewApiProvision` 和 `handleAddSubscriptionAccount` 都对它 `Lock/defer Unlock`，且在**整个 handler 生命周期**持锁——包括 `VerifyWithFallback`、`FetchGroups`、`FetchModels`、`provisionNewApiGroupKeys`（对远端 new-api 站点建 key）、`Store.Create/AddAccount`、`AddUpstream`、`ReconcileProvisioned`，直至 handler 返回。ctx 超时为 30s。

注释说明设计意图：串行化避免同一时刻重复创建同名远端 Key，配置管理器仍负责与其他来源的渠道名冲突检测。

对比 sync service 用的是 per-UID 锁（`lockForUID`），设计上更细粒度。

**缺口分析**

1. **全局锁把所有账号 provision 串行化，含慢速网络 I/O。** 持锁跨越多个对远端 new-api 站点的 HTTP 往返（verify/groups/models/逐分组建 key）。两个**不同订阅、不同站点**的 provision 也会互相阻塞，最坏情况下第二个请求要等第一个的完整 30s 网络周期。多账号（add-account）与主 provision 共用同一把锁，批量接入多账号时吞吐受限。

2. **锁粒度与 sync service 不一致。** 同一进程里 sync 用 per-UID 锁、provision 用全局锁，两套并发模型并存。provision 持全局锁期间，sync service 对**同一订阅**的 `SyncNow`（走 per-UID 锁）可与 provision 交错执行——provision 尚未 `Store.Create` 完成落库时 sync 读到的中间态、或 provision 已建远端 key 但未 reconcile 到渠道时 sync 的 reconcile，两者对同一渠道 `APIKeyConfigs` 的读改写不受同一把锁保护（provision 用 `newAPIProvisionMu`，sync 用 `lockForUID`，互不感知），存在竞态窗口。

3. **锁未保护配置写入的读-改-写序列。** provision 内先 `findNewApiMergeTarget`（读 config）→ 后 `updateChannelForKind`（改 config）。`ConfigManager` 自身的 `cm.mu` 只保护单次 Add/Update 调用，跨调用的“查合并目标 + 写”序列靠 `newAPIProvisionMu` 全局串行化来保证——一旦未来放宽这把锁的粒度，`mergeIndex` 可能失效（代码有 `mergeIndex >= len(channels)` 的防御，但那只是越界检查，不能防止 index 指向了被重排后的另一个渠道）。

**建议方案**

- **改为 per-subscription（per-UID）锁，与 sync service 统一。** provision 的 key 冲突边界本质是“同一 new-api 站点 + 同一 key 名称”。可将锁键设为 `baseURL 站点身份`（而非 subscriptionUID，因为跨订阅但同站点才会撞远端同名 key），用与 sync 同款的 `map[string]*sync.Mutex` + 保护该 map 的 `sync.Mutex`。这样不同站点的 provision 可并发。若嫌站点键复杂，退而求其次用 subscriptionUID 作键（add-account 用其所属 subscriptionUID），可解决“不同订阅互相阻塞”，但保留同站点跨订阅撞名的小概率风险（此风险由远端 `FindTokenByName` 查重 + `provisionNewApiGroupKeys` 的 `newApiProvisionConflictError` 兜底）。
- **让 provision 与 sync 共享同一把 per-UID 锁。** 把 `newAPIProvisionMu` 换成 `SyncService.lockForUID` 暴露的同一锁实例（或让 provision 也走 sync service 的锁），消除“同一订阅 provision 与 sync 并发”的竞态。
- **缩小持锁范围。** 只在“查合并目标 + 建渠道/并渠道 + LinkChannel”这段配置读-改-写序列持锁；远端 HTTP（verify/groups/models）可移出锁外（这些是幂等只读或由远端查重兜底的建 key）。注意 `provisionNewApiGroupKeys` 建 key 部分仍需在锁内以防同名并发，需权衡。

### 8.3 汇率图缺失时的降级策略

**当前实现**

SmartRouter 成本计算的三段降级（`smart_router.go:1326-1380`）：
1. 基础标价为 `listCost`，默认成本 `-1`（表示无成本证据）。
2. 若配置有 `ExchangeRateQuotes`，构建 `graph`（构建失败时通过 `logExchangeRateBuildErrorOnce` 按 `ExchangeRateSnapshot.Version` 去重记录日志）；默认 `entry.EstimatedCost = listCost * timeMultiplier * groupMultiplier`，其中 `groupMultiplier` 取 `APIKeyConfigs` 中的 `GroupMultiplier`，缺失按 `1.0`。
3. 若 `graph != nil && r.subscriptionStore != nil` 且订阅有 `PaymentAmount/CreditAmount`，调 `ResolveEffectiveCostUSD` 算 effective USD，仅当 `resolvedCost.Available` 才覆盖；否则通过 `logEffectiveCostMissingOnce` 按 `subscriptionUID + Reason` 去重记录不可用原因。

**汇率图缺失/构建失败时的降级：**
- 配置默认**永远有 quotes**：`defaultExchangeRateQuotes`（USD↔CNY、LDC↔CNY）在 `Validate` 里对未配置的情况兜底填充，所以 `len(ExchangeRateQuotes) > 0` 几乎总成立。
- `NewExchangeRateGraph` 构建失败时 error 不再静默丢弃，而是由 `logExchangeRateBuildErrorOnce` 按 snapshot version 只记录一次，并回退到 `listCost * timeMultiplier * groupMultiplier`。
- `ResolveExchangeTerms` 对 graph nil、单位不在图中、版本不匹配、单价非有限正数等都返回 `OK=false` + 具体 `Reason` 字符串，`Available=false`，调用方回退标价。
- `EstimatedCost` 最终进 `NormalizeSavingsScore`；`c < 0`（无成本证据）得中性 0.5 分，不惩罚。

**请求期路径（`upstream_failover.go:291` `buildRequestCostContext`）**（`49f28b3e` 后已支持渠道级 effective cost）：
1. 列表成本按渠道计价币种折 USD（`ResolveUpstreamCapability` + `CalculateTokenCostUSDWithPricing`），只固化 `ListCostUSD` + `ExchangeSnapshotVersion`。
2. 构建全局汇率图（quotes 来自 `CostOptimization.ExchangeRateQuotes`）。
3. 渠道级「充值币种/金额 + 渠道币种/到账金额」四字段齐备且金额>0 时，调 `autopilot.ResolveEffectiveCostUSD` 计算 `EffectiveCostMultiplier = (Payment×充值币价)/(Credit×渠道币价)`，置 `EffectiveCostAvailable=true`、`EffectiveCostReason="channel payment/credit conversion"`。
4. 四字段缺配但渠道配了 `CostMultiplier` 时走简化路径直取该倍率（reason `"channel cost multiplier"`）。
5. 订阅级 billing-terms 仍未在请求期解析（reason 以 `"subscription payment/credit snapshot unavailable"` 起步）——订阅级补齐仍是路由期（§4.4）专属。

**缺口分析**

1. **new-api 无 billing-terms 且渠道未配四字段时 effective USD 不可用。** effective USD 计算要求订阅同时有 `PaymentAmount` 和 `CreditAmount`，或渠道配齐计费四字段。new-api provision 建 profile 时设 `Currency:"quota"`、`Balance`，**从不设置 `PaymentAmount/CreditAmount`**（只能通过 `PATCH /subscriptions/:uid/billing-terms` 手工填，四字段 `paymentAmount/paymentUnit/creditAmount/creditUnit`，充值倍率单字段模型已移除 `a96098da`）。未补齐的订阅/渠道走 `listCost * timeMultiplier * groupMultiplier`；分组倍率已能影响 SavingsScore，但真实到账 USD 成本仍无法计算。

2. **`quote` 单位与 key 计价单位可能对不上。** new-api key 的 `GroupMultiplier` 是相对倍率，effective 计算用 `PaymentUnit/CreditUnit`（如 CNY/LDC）查图。若用户 billing-terms 填的单位不在默认图（USD/CNY/LDC）中，`ResolveExchangeTerms` 返回 `"exchange rate unit not in graph"`，静默回退——用户没有任何反馈知道要去补一条 quote。

**建议方案**

- **引导用户补 billing-terms。** 在订阅详情/编辑处明确提示：只有填写 `PaymentAmount/CreditAmount/PaymentUnit/CreditUnit` 并保证单位在汇率图内，effective USD 成本路由才会生效。
- **暴露成本降级的可观测指标。** 请求期 `buildRequestCostContext` 已有 `EffectiveCostReason` 字段并持久化到 SQLite，`cost_report_handler.go` 也统计 `EffectiveUnavailableCount`。建议把 SmartRouter 路由期解析出的 `Reason` 也接入同一诊断链路，让 `/api/reports/cost` 能展示“多少 new-api 请求因汇率/billing-terms 缺失而无法算 effective cost”。
- **provision 时预填默认 billing-terms 或返回 warning。** 若 new-api quota 与某法币有确定换算（如平台按 CNY 充值得 quota），provision 可预填 `PaymentAmount/CreditAmount/PaymentUnit/CreditUnit`，或在 provision 响应里返回一个 warning 提示用户补 billing-terms + 对应汇率 quote，才能启用 effective cost 路由。

### 8.4 补充发现

- **`ExchangeRateQuotesConfigured` 语义**：区分“用户显式清空 quotes”与“旧配置没配 quotes”的机制。若用户显式传空数组清空 quotes，则 `len(ExchangeRateQuotes) == 0`，不建图，所有 key 回退标价——这是**唯一能真正让图缺失的路径**，且同样无日志。
- **图版本一致性检查**依赖 `ResolveUSDPrice` 返回的 version 全等。构图时 version 取自 `ExchangeRateSnapshot.Version`（缺省 1），若 snapshot 与 quotes 不同步（如 quotes 更新但 snapshot 未重算），version 仍能自洽（同一次 `NewExchangeRateGraph` 内所有单位同 version），不会误判——此处设计正确，无缺口。
