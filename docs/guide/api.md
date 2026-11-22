# API 与鉴权

接口实现以仓库的 `backend-go/routes*.go` 为准，本页只维护稳定入口与调用约定。配置见[环境变量](environment.md)，模块边界见[架构](architecture.md)。

## 密钥边界

- 代理请求使用 `PROXY_ACCESS_KEY` 或已配置的额外代理密钥，常用请求头为 `x-api-key` 或 `Authorization: Bearer ...`。
- 管理请求使用 `ADMIN_ACCESS_KEY`；未配置独立管理密钥时回退到主代理密钥。配置额外代理密钥时必须提供独立管理密钥，额外密钥不获得管理权限。
- Gemini SDK 也可使用 `x-goog-api-key`；Responses WebSocket 的鉴权兼容浏览器握手子协议。
- `GET /health` 无须认证。关闭 Web UI 不关闭代理接口，管理界面和管理 API 则不可用。
- 管理 URL 即使以 `.js` 等静态后缀结尾，也必须经过管理访问控制。CORS 来源策略不能替代鉴权。

## 代理入口

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| POST | `/v1/messages`、`/v1/messages/count_tokens` | Messages 与 token 计数 |
| POST | `/v1/chat/completions` | Chat Completions |
| POST / GET | `/v1/responses` | Responses / WebSocket |
| POST | `/v1/responses/compact` | Responses compact |
| POST | `/v1/alpha/*` | Codex history/notes 透传 |
| GET | `/v1/models`、`/v1/models/:model` | 模型列表和详情 |
| POST | `/v1beta/models/*` | Gemini generateContent / streamGenerateContent |
| POST | `/v1/images/generations`、`/v1/images/edits`、`/v1/images/variations` | Images |
| POST | `/v1/embeddings` | Vectors |
| POST | `/v1/quote` | 调用方离线估价 |

这些入口还支持一个 `/:routePrefix` 前缀，例如 `/team/v1/messages`；健康检查也支持 `/team/health`。前缀是候选渠道隔离条件，不能把同一渠道的无前缀地址当作等价别名。带前缀的代理入口仍须验证代理密钥。

## 管理与密钥操作

管理接口包括 `/api/{type}/channels/*`、稳定 UID 的 `/api/channels/*`、`/api/logical-channels/*` 以及各业务模块的 `/api/*`。`type` 为 messages、chat、responses、gemini、images 或 vectors。

控制台的密钥操作使用请求体，避免把明文密钥放入 URL。下面的 `id` 是该协议渠道数组的当前索引，不是稳定 UID。

| 方法 | 路径 | 请求体 |
| --- | --- | --- |
| POST | `/api/{type}/channels/:id/keys` | `{"apiKey":"..."}`，新增密钥 |
| POST | `/api/{type}/channels/:id/keys/remove` | `{"apiKey":"..."}` |
| POST | `/api/{type}/channels/:id/keys/move-to-top` | `{"apiKey":"..."}` |
| POST | `/api/{type}/channels/:id/keys/move-to-bottom` | `{"apiKey":"..."}` |

请求使用 `Content-Type: application/json` 和管理密钥。旧 `DELETE .../keys/:apiKey`、`POST .../keys/:apiKey/top`、`POST .../keys/:apiKey/bottom` 仅为客户端兼容保留。Yunqi 会对规范化路径中的密钥脱敏，但不能控制外部反向代理日志；新客户端应使用请求体接口。

能力测试仅适用于 messages、chat、responses、gemini，不适用于 Images 或 Vectors。健康检查和余额探测的渠道级管理入口覆盖六类协议，细节见[现行规格](../specs/README.md)。

## 协议转换与流式约定

跨协议请求在实际 provider builder 和最终 effort 处理之后检查结构化输出、媒体结构、缓存提示和函数 schema。`X-Yunqi-Conversion` 给出 `steps`、`coverage`、`quality` 和字段级 `diagnostics`，不包含请求值。`partial` 只表示已声明范围内未发现损失；`unknown` 表示不能检查；`lossy` 表示已发现降级。结构化输出约束或媒体结构丢失会在发往供应商前拒绝。原生协议不因此跳过该协议已有的兼容处理。

Chat `response_format`、Responses `text.format`、Claude `output_config.format` 和 Gemini JSON Schema 在可表达范围内互转；最终 effort 改写保留 schema 原始属性顺序。Gemini 可空、联合和引用结构使用 JSON Schema 表达。供应商不能表达的约束不会伪装成等价支持。

工具结果中的图片和内联文件保留为结构化媒体，工具调用 ID 与并行结果配对。Chat 的工具媒体放在连续 tool 结果之后的 user 多模态消息；Gemini 使用 `functionResponse.parts`。不能跨供应商使用的文件 ID 或无法确定 MIME 的远程媒体明确拒绝，不会下载文件或把二进制塞进普通工具文本。Gemini 缺 ID 时按同名未匹配调用顺序生成和配对，找不到调用则拒绝。

托管搜索与客户端函数工具保持区别。跨协议无法表达原生引用定位时保留可读来源、标题和引用文本，并附转换诊断；不暴露供应商 opaque 搜索数据，也不捏造字符位置。不能等价转换的原生托管搜索声明要求使用原生渠道。

Responses 错误终帧区分上游错误和网关合成错误，已交付内容后不重放为另一次成功。Responses WebSocket 的 `response.cancel`/`response.interrupt` 只取消当前轮，连接可以继续下一轮；客户端断开仍触发取消。TTFT 计量等待首个有效内容的耗时，纯元数据、空 delta 和心跳不结束首内容等待。

`responsesClaudePrefixCache` 是渠道 JSON 选项，默认关闭，仅用于 Responses→Claude 的稳定历史前缀。存在显式 `cache_control` 时保留原设置，不推断供应商 TTL。OpenAI 原生缓存字段保留显式 false/null 的存在性；跨供应商不支持的提示会报告丢弃。

## 团队身份与调用方策略

四类文本入口都会验证源协议的流式终态；连接 EOF 本身不代表成功。缺少终态、供应商错误或不完整压缩流会保留失败语义，已写出的内容不会被另一请求重放。正常内容及时转发，只有待确认终态尾部使用有界缓冲。

| 入口 | 权限与用途 |
| --- | --- |
| `/api/entry-keys`、`/api/entry-keys/:id`、`/api/entry-keys/:id/rotate` | 全局管理员管理调用方 Key；明文只在创建或旋转时返回，持久化保存 hash |
| `/api/admin-credentials`、`/api/admin-credentials/:id`、`/api/admin-credentials/:id/rotate` | 全局管理员管理控制面凭证、角色和渠道 UID 范围 |
| `GET /api/access/me`、`GET /api/access/channels` | 有限角色的身份和脱敏渠道视图 |
| `PUT /api/access/channels/:uid/state` | operator/admin 在授权渠道范围内调整状态 |
| `/access` | 有限角色门户；凭证只保留在当前页面内存中 |
| `POST /v1/quote` | 代理认证，离线估价；遵守模型与渠道范围，不调用供应商 |

viewer 只读，operator 可执行有限状态操作，admin 仍受其渠道范围限制；全局管理、凭证导出和全量诊断要求无渠道范围限制的 admin。有限角色不复用会返回原始 Key 的旧管理接口。管理凭证不会自动获得代理调用权限。入口 Key 可限制模型、渠道 UID、有效期、来源 CIDR 和并发；CIDR 使用真实对端地址，伪造 `X-Forwarded-For` 不改变准入。

报价区分已知成本与缺失输入/输出上界或媒体价格的情况，不能将不完整报价当作零费用。实际 usage 会标注 provider/estimated/mixed/unknown 来源并保留有界、最小化的原始计量证据。

可信身份代理默认关闭。全局管理员通过 `GET/PUT /api/identity-proxy` 配置 issuer、真实代理 CIDR、allowedOrigins 与组→角色/渠道范围映射，服务端环境还须配置至少 32 字节的 `IDENTITY_PROXY_SECRET`。代理必须覆盖客户端同名身份头，发送 `X-Yunqi-Identity-Secret/Subject/Groups/Session/Expires`；Expires 是固定会话的 Unix 到期秒，延长会话需换 Session。所有 SSO 管理请求还须 `X-Yunqi-Management-Request: 1`，写请求须带匹配的 Origin。身份头不转发到供应商。

`POST /api/access/session/revoke` 撤销当前 SSO 会话；全局管理员也可使用 `POST /api/identity-proxy/sessions/revoke` 按 sessionFingerprint 撤销。原始 subject/session 不保存或返回，撤销持久化，修改角色映射不会清除撤销记录。SSO 不自动授予数据面权限。

Yunqi 可通过 `/auth/oidc/login` 原生发起 OIDC Authorization Code + PKCE S256 登录；也保留外置可信身份代理模式。Passkey 与双因素认证由 IdP 完成，原生模式验证签名 ID Token 的 `amr`/`acr` 是否满足所配认证强度，不能仅凭界面声称已开启 MFA。Copilot OAuth 仍是上游授权，不是管理员登录。

原生 OIDC 默认关闭。浏览器仅保存 `__Host-yunqi-session` 随机会话引用（Secure、HttpOnly、SameSite=Lax），不保存 ID/Access/Refresh Token。`/access` 提供登录、当前身份与撤销退出入口；退出复用 `POST /api/access/session/revoke`。每次请求依据当前 `identityProxy.groupMappings` 重新计算角色与渠道范围，撤销检查沿用同一 local/shared 机制。Cookie 写请求须携带所配控制台 Origin 和 `X-Yunqi-Management-Request: 1`；登录不授予代理调用权限。

本地会话仅驻留当前进程，重启后须重新登录；共享模式把授权流程和会话存入显式 Redis 后端，支持登录回调落到另一节点。后端或撤销 realm 不可用时拒绝认证，不退回本地模式。每主体保留最多 20 个会话，新登录淘汰最旧会话，每小时最多签发 100 个会话；达到签发限制时不淘汰现有会话。会话最多 8 小时且不超过 ID Token 到期时间。Redis 不包含在本地文件备份内，不能单独回滚旧 Redis 快照来恢复会话；共享撤销存储的持久化/禁止驱逐要求仍适用。

`identityProxy.revocationMode` 默认为 `local`，只保证当前节点撤销；`shared` 使用显式配置的 Redis 撤销后端，每次 SSO 鉴权检查共享状态。多节点应使用同一撤销后端和 namespace，并保持身份代理策略一致；后端缺失或读写失败时返回 503，不退回本地放行。身份视图与退出操作会显示撤销范围，不能把本地退出当作全节点撤销。

共享模式把 `revocationEpoch` 持久绑定到撤销命名空间。仅首次显式启用共享模式可初始化空命名空间；普通鉴权、启动、旧客户端省略字段的保存都不会重建。标记丢失或变化时拒绝认证，需恢复原标记与撤销数据；不能只重建标记继续放行。Redis 后端必须保证已确认撤销写入不丢失并禁止驱逐，单独丢失撤销记录无法仅靠命名空间标记识别。

严格会话为可选进程策略，详见[环境变量](environment.md)。首次请求在响应之前原子绑定渠道 UID；绑定不可用时失败，不跨渠道或竞速迁移，同渠道内 Key/URL 重试仍可发生。

## 配置与运行诊断

全局管理员可通过 `POST /api/channels/:uid/request-preview` 对已有 messages/chat/responses/gemini 渠道及其白名单草稿字段运行真实请求 builder。预览不请求供应商、不刷新 OAuth、不查询历史会话；Copilot 不支持离线预览。样例及支持字段的检查范围会显式返回；未覆盖字段不能据此判定为有效。Responses 的历史响应 ID 等依赖在线状态的输入会拒绝，草稿凭证不会出现在预览结果中。

| 方法与路径 | 结果与边界 |
| --- | --- |
| `GET /api/metrics/prometheus` | 全局 admin；低基数请求、延迟、并发、failover、限流、流终态与导出健康指标 |
| `GET /api/reports/request-timeline/:id` | 全局 admin；按服务端 request ID 或 correlation ID 查有界逻辑请求时间线 |
| `GET /api/reports/requests` | 全局 admin；SQLite 请求记录的筛选与游标分页 |
| `GET/PUT /api/logging/body` | 全局 admin；独立正文开关、保存版本与 actor 变更审计 |
| `GET /api/logging/body/export` | 全局 admin；当前文件最多 1 MiB 完整行，二次脱敏，返回截断/范围说明 |

`X-Yunqi-Request-Id` 关联所有 Key、渠道和竞速尝试。时间线只留结构化状态/错误来源/安全 ID，不留 prompt、模型、URL、原始 Key、会话或自由错误。最近 1024 个逻辑请求，每请求最多 128 尝试与 256 事件，超限有丢弃计数，重启清空。HTTP 200 的流内失败另看 terminalOutcome/events。`phaseTiming` 使用请求独占耗时口径，重叠网络/排队/写出阶段不重复相加；未知阶段省略。

请求历史必填 `since`、`until`（Unix 秒），支持 type/model/channelUid/correlationId 和 limit（1–200）。继续页同时使用返回 cursor 与原筛选，游标不允许换筛选；结果使用首屏快照上界，新增记录不挤入后续页。保留期以响应及指标配置为准。

正文日志默认关闭，即使 development 与旧日志开关开启也不会自动输出正文。全局管理员通过正文 API 分别设置 requestBodies/responseBodies，只有策略与 actor 审计原子落盘成功才激活。有效正文输出还需要 development 和对应旧日志开关；maxBytes 默认 4096，可设 256–65536，媒体及结构化个人信息字段省略，非 JSON 只记录长度。下载不包含活动尾行或超长行，不支持任意文件路径。

渠道兼容字段的部分更新中，省略字段表示保留，显式 false/0/空数组会参与更新；null 按具体字段定义清空或拒绝。Web 表单保留未编辑字段。支持版本检查的管理保存使用响应 revision/ETag 与 `If-Match`，冲突时重新读取；不把未成功持久化的状态显示为保存成功。

完整替换上游 Key 会清除旧 Key 故障状态；部分替换保留仍有效 Key 的健康记录。手动暂停不会被换 Key 自动撤销；因没有 Key 自动暂停的渠道可在补充可用 Key 后恢复。

## 价格维度与账单版本

全局管理员通过 `GET/PUT /api/budgets` 管理稳定 principal 的预算，通过 `GET /api/budgets/:principal` 查看周期状态，`GET /api/budgets/:principal/requests/:request` 查询该调用方的逐尝试账本。调用方通过代理认证的 `GET /v1/budget` 仅查看自身状态，不会看到其他身份。金额 API 使用 USD 十进制字符串，`costBasis=list_price_usd` 明确表示模型标价口径，不等于订阅渠道的实际购入成本。

预算可按时区设置日、周、月、终身或滚动窗口，并设置每请求上限。每次实际发送前先预留，重试与竞速各自记账，最后按供应商可验证计量结算。输入预留使用最终序列化字节数加余量，输出需要显式上限或模型能力上限；未知价格、媒体或尚不能定价的托管工具费用会在调用上游前拒绝。完整的供应商零计量可释放预留，断流或不完整计量保守保留预估，不能按零费用处理。

预算账本默认独立 SQLite 持久化，也可显式配置共享 Redis 账本（见环境变量指南）；共享模式按 Redis 服务端时间确定周期并保留逐尝试价格版本。预算账本固定请求开始时的周期与价格版本；过期进程租约按保守规则回收。预留采用保守上界，但不承诺供应商费用的绝对封顶；上游返回超预留用量时如实记录，并阻止后续超预算调用。修改配置不会追溯改写旧账单。

模型价格支持输入长度阶梯、缓存写入 5 分钟/1 小时价、独立推理输出价，以及按 quality/size 匹配的图片单价。token 价格使用每百万 token，图片价格使用每张；显式零价有效，负数和非有限数拒绝。规则最多 128 个阶梯、64 条图片价格，每次计量最多 256 个项目，不执行用户表达式。

请求开始时冻结价格快照，`pricingVersionId` 由内容生成；请求进行中修改配置不会改变其结算，旧历史金额不会按新价格重算。推理 token 按供应商口径区分输出子集与额外产出；启用相应差异定价后缺少所需推理/TTL 计量，或缺少适用价格时，只返回已知下界并标记价格维度不完整，不能把它当完整账单。普通输入/输出价格保留原有计费兼容性。旧 CNY 价格换算沿用固定系数，并非实时汇率。

成本页提供远程预置价格的待审、差异和回滚面板。全局管理员使用 `GET /api/presets/prices` 查看，`POST /api/presets/prices/check` 检查来源，`POST /api/presets/prices/approve` 提交 reviewId 与 allowAnomalies，`POST /api/presets/prices/rollback` 提交历史 bundleHash。下载到的价格变化不会自动生效；默认超过 50% 的跳变或计价维度变化需要显式异常确认。审核绑定当前完整 bundle 和来源摘要，过期页面不能覆盖新版本。

审核激活、actor 审计及回滚快照在单一原子文件中保存，失败不发布新状态。包含价格变化的整个预置 bundle 会等待审核；不影响价格选择的纯元数据更新沿用既有更新流程。最多保留 10 个版本，回滚在重启后仍恢复同一完整 bundle，包含模型能力，不只是价格；不重算已结算请求。旧缓存若没有审核依据且价格选择不同，只进入待审，不在启动时静默替换。来源签名、检查时间和发布时间可查看；签名与时间要求见[环境变量](environment.md)。

## 管理变更审计

全局管理员可访问 `GET /api/audit`，按 `actorId`、`action`、`outcome`、`operationId` 筛选，`limit` 为 1–200（默认 100）。后续页携带返回的 `nextCursor` 并保持筛选和 limit 不变；游标冻结首屏序号上界及保留下界，并绑定快照事件摘要，不会串到另一副本的不同历史；新增事件不会挤入旧页，首屏历史区间发生部分或全部淘汰时返回 409。有限角色和限定渠道范围的 admin 不可读取全局审计。响应还按同一 operationId 关联正文策略与价格审核的原子审计。

已认证管理写请求先持久化 `intent`，再执行授权和处理器；`X-Yunqi-Audit-Operation` 返回服务端生成的关联 ID。真实配置成功提交的 `committed` 事件与配置在同一个原子文件替换中保存，包含 actor、操作类别、资源路径哈希、前后状态摘要及字段级差异摘要。值只保存 SHA256 和编码长度，动态 map 键也使用哈希，不保存明文配置值、正文或自由错误。后台与外部配置重载使用 system actor；请求 actor 通过显式上下文传递，不使用全局可变 actor。

`completed` 是 HTTP 处理结束，`accepted` 仅表示任务已接收；二者不替代实际 `committed` 或异步任务的完成状态。授权拒绝、校验失败和处理错误保留各自结果。审计意图无法持久化时拒绝执行管理变更；响应已交付后若结果记录失败，保留已有记录而不捏造完成事件。未认证请求沿用认证失败日志，不触发配置审计写入，避免匿名请求造成整份配置的磁盘写放大。

审计最多保留 1000 条且不超过 2 MiB，存于配置文档内，随一致状态备份恢复；多副本各自保留本节点历史。普通配置快照和导入候选不携带可覆盖权威历史的权限，审计查询独立读取历史。发现磁盘存在尚未发布的外部配置编辑时，管理审计/写入暂时拒绝，待正常热重载完成；不会把外部 ACL 编辑误标为自身写入而跳过加载。

路由预演、请求预览等 POST 只读管理接口也会记录审计元数据，其业务配置、调度、预算和 trace 的只读契约保持不变；审计元数据不会改变渠道 revision/If-Match 或价格审核 ID。

失败结果也不表示所有外部副作用都已撤销；应结合 intent、committed 与领域状态核实实际变更。

## 稳定调用方成本归因

全局管理员使用 `GET /api/reports/cost?groupBy=principal&type=chat&duration=7d` 按调用方主体聚合。`principalId` 可进一步限定主体，并与 `groupBy=model/user/key` 组合下钻。`user` 参数保留原代理 Key 掩码分组语义，`key` 为上游 Key 掩码；API 默认仍是 `user`，Web 成本页默认选择主体。

记录只从服务端已认证的身份生成域隔离 SHA-256 归因 ID：`p_` 前缀为显式稳定主体，`k_` 为凭证绑定身份。两者均是不透明 ID，响应中的短指纹只作脱敏显示，筛选和 CSV 使用完整 ID。请求头不能指定归因主体，原始 Key、subject、email 和配置中的主体明文不会写入新增归因列。只有为入口 Key 配置相同稳定 principal 的调用才会在轮换后归并；不能凭显示掩码猜测多个静态 Key 属于同一团队。

旧记录和未捕获认证身份的记录统一归到 `unknown`，不会按当前 Key 配置回填或重分配历史。SQLite v12 升级增加默认空值的归因列和主体/时间索引，保留原有费用快照及旧记录。

Web 成本页可从主体进入模型明细，切换其他维度仍保留主体筛选；CSV 同时导出完整归因 ID 与脱敏标签，刷新/切换主体时不会用过期响应覆盖新范围。请求历史支持相同 `principalId` 筛选，游标与该筛选绑定，继续页不能改成其他主体或全量。历史界面沿用报告返回的时间范围与指标保留期，返回结构不含请求正文或认证凭据。

这些报表聚合已持久化的请求指标，沿用既有费用和定价完整性口径，不代替独立预算账本，也不保证多次实时查询构成同一数据库快照。主体、成本和请求历史接口继续要求管理权限；数据面代理 Key 不获得其他主体的报表权限。

模型导入策略、预览、批准和显式移除在同一配置事务中提交管理审计。HTTP 调用保留已认证操作者与 operationId，自动发现使用新的 system 操作。策略与批准的领域审计保留同一 operationId；持久化失败不会发布目录状态或 committed 事件。
