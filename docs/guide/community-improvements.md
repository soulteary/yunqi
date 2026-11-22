# Yunqi 社区参考改进记录

本页记录 2026 年 11 月 22 日起，基于 `56fb5d2` 的跨节点治理、身份、恢复与可观测性改进。每批列出 Yunqi 的实际行为、功能或修复提交、回归证据，以及参考的上游 issue 和 PR。上游 PR 的设计或问题描述只作为参考，不等于其已合并、已发布，也不表示 Yunqi 关闭了上游 issue。

功能状态以本项目 PR 的合并结果为准。部署参数和能力边界分别见[环境配置](environment.md)、[API](api.md)、[完整状态恢复](recovery.md)、[模型目录治理](model-governance.md)。代码实现、回归补丁及后续集成修复分批保留在 Git 历史中。

## 功能改善

| 批次 | 原有问题或缺口 | 本轮改善 | 范围与项目 PR |
| --- | --- | --- | --- |
| 01 状态恢复 | 配置备份不包含完整账本、SQLite WAL 和价格安全状态 | 双目录排他锁、完整本地状态冷备、逐文件校验、数据库完整性验证、新目录恢复 | [#99](https://github.com/soulteary/yunqi/pull/99) 已合并；外部 Redis/IdP 仍须协调快照，不会自动重载部署 |
| 02 共享预算 | 各节点独立 SQLite 无法约束团队总预算 | Redis 原子预留/结算、精确整数金额、逐尝试价格与 usage、不变账单归档、已有本地余额切换保护 | [#102](https://github.com/soulteary/yunqi/pull/102) 已合并；后续一致性修复 [#104](https://github.com/soulteary/yunqi/pull/104) 也已合并；默认仍为本地账本 |
| 03 原生身份 | 缺少原生 OIDC 登录及服务端会话，认证强度依赖可信代理断言 | Yunqi 原生 AuthCode + PKCE、服务端会话、AMR/ACR 强度校验、角色范围和共享撤销 | [#100](https://github.com/soulteary/yunqi/pull/100) 已合并；原生登录使用 `/access` 安全门户，Vue 管理页仍使用管理凭证；MFA/Passkey 由 IdP 执行，可信代理继续可用 |
| 04 共享路由 | 渠道限额与严格会话绑定各节点独立 | 共享显式渠道 RPM/并发、发送前原子会话绑定、活跃 Body 续租、状态丢失拒绝放行 | [#101](https://github.com/soulteary/yunqi/pull/101) 已合并；Key/Quota 与发现值保留本地保护，不复制 Responses 完整历史 |
| 05 路由评估 | 缺少固定样本下的质量、成本和延迟对照 | 独立质量标签与实际成本、分层门槛、P95、平均重试、持久化证据与内容摘要重放 | [#103](https://github.com/soulteary/yunqi/pull/103) 已合并；只进入人工复审，不自动改变在线策略 |
| 06 主体成本 | Key 轮换后难以统一长期成本归因 | 稳定主体与凭证身份分域、持久化归因、模型/Key/请求下钻及 CSV 对齐 | [#106](https://github.com/soulteary/yunqi/pull/106) 已合并；旧记录保留 unknown，不从脱敏 Key 猜测归属 |
| 07 模型治理 | 模型发现、导入纳入规则、名称映射与端点信息未形成统一审核流程 | 渠道级导入策略、规范化、差异预览与审核应用、统一 endpoint 元数据 | [#107](https://github.com/soulteary/yunqi/pull/107) 已合并；以实际接入的协议与探测证据为准 |
| 08 统一审计 | 不同管理模块各自审计，配置变更缺少统一 actor 与操作关联 | 显式 actor/operationId、配置与成功审计原子保存、请求意图/结果和有界查询 | [#105](https://github.com/soulteary/yunqi/pull/105) 已合并；历史与字段差异脱敏，不把 HTTP 接受等同于后台任务完成 |

## 问题修正

以下同时记录原有缺口和本轮实现、回归及复审中暴露的边界，按所属批次定位；新实现的问题不倒推为旧版或上游同样存在的缺陷。

| 问题 | 影响 | 修正与回归 | 所属批次 |
| --- | --- | --- | --- |
| 冷备遗漏配置目录旁的预算库 | 配置与状态目录分离时恢复丢失已用额度 | 显式纳入 `entry-budget.db`；验证已提交 WAL、预算金额与安全状态恢复 | 01 / #99 |
| 备份或恢复输出路径指向源目录 | 符号链接可绕过路径限制，或恢复污染原 bundle | 比较真实路径，拒绝嵌套输出、符号链接根、路径穿越及额外文件 | 01 / #99 |
| 相对路径构造 SQLite URI 错误 | 默认目录备份、相对 bundle 校验失败 | 数据库路径先转绝对路径；完整相对路径 backup/verify/restore 回归 | 01 / #99 |
| 配置和状态目录别名造成重复锁 | 同一物理目录被本进程加锁两次，启动误报繁忙 | 解析真实目录后排序去重；验证第二实例拒绝及锁释放恢复 | 01 / #99 |
| 已结算历史达到主账本容量后永久拒绝 | 长期请求积累使共享预算不可继续使用 | 原子归档结算请求，保留周期金额及滚动窗口索引；10,001 条历史回归 | 02 / #102 |
| 离线初始化阻塞扫描 Redis 全库 | 对同一 Redis 的其他命名空间造成不必要阻塞 | 使用增量 SCAN，保留孤立账本拒绝和 epoch 验证 | 02 / #102 |
| WATCH 分步读取被并发写入打断 | 首次建账时把暂时不一致误判为账本损坏 | 已确定性复现首次并发建账与并发退款两种混合读取，修复已提交 [#104](https://github.com/soulteary/yunqi/pull/104)，[完整 Verify](https://github.com/soulteary/yunqi/actions/runs/44996353737) 已通过并合并；这是 Yunqi 本轮实现问题，不据此推断上游存在同一缺陷 | 02 |
| 旧共享会话沿用较低认证强度 | 收紧 MFA/Passkey 策略后仍可能接受旧会话 | 认证策略摘要和撤销 epoch 绑定流程、会话与 realm；策略收紧回归 | 03 / #100 |
| 匿名待完成 OIDC 流程无限增加 | 共享 Redis 会话状态缺少容量边界 | 每 realm 4,096 个、消费释放、过期恢复；满载在访问 IdP 前拒绝 | 03 / #100 |
| 临时缩短会话期限导致撤销提前失效 | 退出后再次放宽策略可能复活原 Cookie | 撤销保留原存储有效期，当前策略只作局部有效期判断；local/shared 时间推进回归 | 03 / #100 |
| 管理员撤销他人会话误清自己的 Cookie | 执行管理操作后意外退出 | 仅目标指纹匹配当前会话时清 Cookie；本人/他人回归 | 03 / #100 |
| 共享准入拒绝仍消耗本地 RPM | 共享容量释放后，节点继续被本地窗口阻塞 | 显式限制由 Redis 单一管理，保留自身 Key/Quota 与发现限制；双节点反复拒绝后立即恢复回归 | 04 / #101 |
| 丢失单个严格会话绑定后重新选路 | epoch 尚在时可能跨供应商资源迁移 | 未过期索引存在但绑定缺失则拒绝；单记录删除与全状态清空回归 | 04 / #101 |
| 把成功率或缺失成本视为质量/零费用 | 路由评估错误宣布收益 | 缺少独立质量或真实价格时证据不足；每个场景分层独立检查 | 05 / #103 |
| 重试均值误命名为比例 | 每请求多次重试时指标可超过 1，含义不清 | 使用 meanRetries，覆盖平均 1.5 次与重试请求占比 0.5 的区别 | 05 / #103 |
| 新 API 未纳入路由契约清单 | 完整 CI 无法确认路由表兼容 | 显式更新三条评估路由，验证生产 Web/API 与开发模式 | 05 / #103 |
| 成本记录缺少模型或价格为 NULL | 未知计量可能被误当作已知零成本，汇总完整性失真 | 保留 unknown 归因和 partial 状态；覆盖缺失模型、NULL 价格及不完整计量，避免把未知金额计作完整成本 | 06 / #106 |
| 成本汇总声明的截止时间与 SQL 不一致 | 未来时间记录进入汇总，却不在返回的历史范围内 | 每次报告请求开始时冻结 `until`，汇总与定价明细同时限制上界；覆盖未来记录，不承诺跨查询数据库快照 | 06 / #106 |
| 模型发现使自己的草稿审核过期 | 未启用草稿仍自动写白名单，或 staging 后学习指纹，改变同轮审核的源版本 | 保存治理策略后发现只生成待审目录，暂停同轮自动白名单、指纹与新协议写入；首次草稿发现可批准，外部并发修改仍返回 409；显式移除策略恢复旧自动行为 | 07 / #107 |
| 模型别名、报价与实际执行身份不一致 | 精确审核别名可能被家族映射替换，或借用其他模型的价格与窗口 | 精确别名优先，虚拟/联邦模型最终 raw ID 必须在审核目录；真实 HTTP、报价原始价格、图片 multipart、向量与 Gemini URI 回归 | 07 / #107 |
| 别名覆盖显式上下文溢出重定向 | 调度已选择大窗口，执行时却改回原小窗口模型 | 区分普通别名与显式溢出重定向，保留且校验已批准的最终执行模型 | 07 / #107 |
| 未保存治理策略就从审核面板启动发现 | 默认空表单未标为修改，发现可能沿用自动写配置的旧流程 | 读取状态返回 `configured`，未持久化策略时要求先保存草稿；界面按钮回归 | 07 / #107 |
| 旧发现路径漏用治理保护 | staging 错误被忽略，仍自动启用协议并报告成功 | 旧路径与正常路径使用相同错误处理和治理保护，覆盖发现期间外部配置变化 | 07 / #107 |
| 探测期间启用治理后旧任务仍自动写配置 | 启动快照无策略，结束时会按旧行为覆盖刚保存的草稿边界 | 自动发现写入在配置锁内验证源版本、当前治理状态与目标身份；过期结果拒绝发布，不盲目刷新版本 | 07 / #107 |
| 隔离 Redis 测试只等 socket 文件出现 | Redis 尚不能处理命令就发布端口，预算初始化回归偶发 EOF | 私有 socket 必须返回真实 PONG，检测总超时和子进程退出，失败输出私有日志；确定性旧行为失败，真实双节点预算与就绪用例连续 20 轮通过；不重试生产账本写事务 | 验证补充 / #108 |
| 统一审计回滚重复执行撤销 | 中心回滚与调用方撤销叠加，可能破坏恢复后的配置 | 移除依赖修改后列表的增量 undo，使用完整快照恢复；覆盖首个及已有逻辑渠道创建、更新、删除失败，失败事务不发布配置或成功审计 | 08 / #105 |
| 审计保留窗口未识别旧游标 | 早于保留下限的 cursor 可能被当作连续历史查询，掩盖已裁剪记录 | 在游标中保留首屏 retention floor，覆盖记录部分淘汰、全部淘汰及旧版本缺少该字段；过期 cursor 返回 409，要求重新获取历史 | 08 / #105 |
| 仅审计写盘吞掉待重载的外部 ACL 变化 | 未发布的业务编辑被误标为本次自身写盘，运行时权限仍旧 | 比较磁盘原始业务文档与已发布摘要，有待重载改动时拒绝 audit-only 写盘；未知字段及大整数差异也参与 guard，覆盖外部 ACL 待重载和正常连续自身写入 | 08 / #105 |
| 账号事务 helper 提取丢失 HTTP actor | 本轮模型与审计合流后，HTTP 管理变更可能被误记为后台 system | HTTP 入口显式转交审计上下文到锁内 helper，发现入口使用独立 system 包装；验证账号写入、单渠道发现写入及操作身份不串用；这是本轮合流问题 | 08 补充 / #105 |

## 分批提交与验证

表中只列本轮功能与修复提交；同步 main 的 merge 提交保留在对应 PR 的完整提交列表中，不重复当作新功能。本轮各功能批次均已通过完整 CI 并合并；验证所用 head 与 main 合并提交同时保留，方便追溯合流结果。

| 批次 | 功能与修复 commit | 本项目 PR | 回归与完整 CI |
| --- | --- | --- | --- |
| 01 | [23dc804](https://github.com/soulteary/yunqi/commit/23dc804ac8694a8494932c16f21471e417b29b49)：冷备与隔离恢复；[7a6977c](https://github.com/soulteary/yunqi/commit/7a6977c655da3588ec0fd384e3e04973651cac74)：相对路径与锁别名 | [#99 commits](https://github.com/soulteary/yunqi/pull/99/commits) | WAL、split paths、损坏、穿越、活动目录拒绝通过；[完整 Verify](https://github.com/soulteary/yunqi/actions/runs/44992303383) 通过 |
| 02 | [74899dc](https://github.com/soulteary/yunqi/commit/74899dcc528b6d94f3c1a036c4a8185cefec29a9)：共享账本；[c854f17](https://github.com/soulteary/yunqi/commit/c854f17c8f3f632dad48edaa195c152432041518)：增量初始化扫描 | [#102 commits](https://github.com/soulteary/yunqi/pull/102/commits) | 真实 Redis 双节点预算、逐尝试结算、大整数、归档与状态丢失通过；[完整 Verify](https://github.com/soulteary/yunqi/actions/runs/44993310368) 通过；后续并发修复单独记录 |
| 02 补充 | [697d4b9](https://github.com/soulteary/yunqi/commit/697d4b934d023e068f4066fc005aa71bffeb4c59)：WATCH 读取一致性、提交前取消及有界抖动 | [#104 commits](https://github.com/soulteary/yunqi/pull/104/commits) | 首次/已初始化主体各 20 并发、各 20 轮；两种确定性竞态、父取消/短 deadline、独立结算通过；[完整 Verify](https://github.com/soulteary/yunqi/actions/runs/44996353737) 通过 |
| 验证补充 | [e5ce139](https://github.com/soulteary/yunqi/commit/e5ce1396752c511de993d7358c255d10b0b3b4ac)：隔离 Redis 协议就绪检测 | [#108 commits](https://github.com/soulteary/yunqi/pull/108/commits) | 旧 Stat 条件确定性失败；两项就绪用例与真实双 HTTP 节点预算连续 20 轮通过；[完整 Verify](https://github.com/soulteary/yunqi/actions/runs/45002955274) 通过；[d595cd3](https://github.com/soulteary/yunqi/commit/d595cd3972af9913c6a3993a2a9903f097ad04aa) 已合并 |
| 03 | [5885e2e](https://github.com/soulteary/yunqi/commit/5885e2e00e41b5b00275afb97af142c37bc31485)：原生 OIDC；[10adfe3](https://github.com/soulteary/yunqi/commit/10adfe30b217a75e7ebbbff16e989f48e5579ee1)：匿名流程容量；[2edd44f](https://github.com/soulteary/yunqi/commit/2edd44f14c2ff1f132901862f1ab970f786ba421)：撤销生命周期 | [#100 commits](https://github.com/soulteary/yunqi/pull/100/commits) | fake TLS IdP、跨节点会话、重放、策略收紧、退出与容量回归通过；[完整 Verify](https://github.com/soulteary/yunqi/actions/runs/44996929484) 通过 |
| 04 | [ef3c3be](https://github.com/soulteary/yunqi/commit/ef3c3be96c1ea680457a7d3e303ebcdfa2fae478)：共享路由；[9decc76](https://github.com/soulteary/yunqi/commit/9decc760e2667e3b06e32ae66eba0ff12454af5b)：RPM 单一所有权 | [#101 commits](https://github.com/soulteary/yunqi/pull/101/commits) | 真实 Redis 独立节点、首次原子绑定、断连/续租、状态丢失及容量恢复通过；[完整 Verify](https://github.com/soulteary/yunqi/actions/runs/44994509959) 通过 |
| 05 | [eaa01b2](https://github.com/soulteary/yunqi/commit/eaa01b2695a95831acdef9b2cb2821b325096266)：固定样本评估；[56a658a](https://github.com/soulteary/yunqi/commit/56a658a0ca1d21c6246f6c17e24db0c713a0f693)：路由契约清单 | [#103 commits](https://github.com/soulteary/yunqi/pull/103/commits) | 分层、未知计量、重放、权限、持久化和 HTTP 保存导出通过；预算并发问题单列 #104 修复；[完整 Verify](https://github.com/soulteary/yunqi/actions/runs/44997667250) 通过 |
| 06 | [ac53904](https://github.com/soulteary/yunqi/commit/ac53904088e510386884fd82bf68b67c08dac0fa)：稳定主体成本归因、下钻与未知计量边界 | [#106 commits](https://github.com/soulteary/yunqi/pull/106/commits) | 轮换归因、模型/Key/请求下钻、NULL 价格与 partial、`until` 和 CSV 对齐回归；[完整 Verify](https://github.com/soulteary/yunqi/actions/runs/44999683781) 通过并合并 |
| 07 | [db29751](https://github.com/soulteary/yunqi/commit/db29751d424eea2c9bc3c1a42d51865a29478f9d)：目录审核与原始执行身份；[2d9ebb8](https://github.com/soulteary/yunqi/commit/2d9ebb83cf0324a5ecb6ec474bff7ee3c400eced)：安全退出治理；[5c5d4f9](https://github.com/soulteary/yunqi/commit/5c5d4f945498ee66eb199a2879991dd8f4be1fef)：独立管理面板 | [#107 commits](https://github.com/soulteary/yunqi/pull/107/commits) | 目录版本、真实 HTTP/raw ID、模型缓存、报价、媒体构造、权限与 UI 竞态回归通过；最终 head [7cfc825](https://github.com/soulteary/yunqi/commit/7cfc825436da1520e43abd6288468c974583fbb2) 的[完整 Verify](https://github.com/soulteary/yunqi/actions/runs/45002978965) 通过；[837e158](https://github.com/soulteary/yunqi/commit/837e158d93d5c3e45d4d96a511156f91eb62d80d) 已合并 |
| 07 补充 | [e95a52d](https://github.com/soulteary/yunqi/commit/e95a52d3bec7a97df2d72ec7b9e0050b2604d8e0)：溢出模型、持久化草稿、旧发现路径及锁内版本保护 | [#107 commits](https://github.com/soulteary/yunqi/pull/107/commits) | 四类问题均先复现失败；config、autopilot、真实执行回归及 22 项前端测试通过，旧发现测试使用真实加载后的完整快照；随最终 head 的[完整 Verify](https://github.com/soulteary/yunqi/actions/runs/45002978965) 通过并合并 |
| 08 | [1066644](https://github.com/soulteary/yunqi/commit/10666445f127a049fb0db85f269a54d1688f5884)：原子配置历史；[644f1ea](https://github.com/soulteary/yunqi/commit/644f1ea46c6e0c399dcca77336981cf203b8b001)：HTTP 主体、查询及领域关联；[28956f3](https://github.com/soulteary/yunqi/commit/28956f3c80ff6f7db2ead86b82315ec20048339b)：审计契约；[d640277](https://github.com/soulteary/yunqi/commit/d640277e687232d98277aab8d7823bfca8c6eee2)：回滚与冻结保留下界；[f508d12](https://github.com/soulteary/yunqi/commit/f508d1219caf3b664e6c6544e981a1d88fbff5ea)：模型四事务审计 | [#105 commits](https://github.com/soulteary/yunqi/pull/105/commits) | 原子回滚、保留窗口部分/全部淘汰及旧游标、外部 ACL guard、模型四项事务（策略、预览、批准、移除）的 actor/system 隔离和真实 HTTP 三阶段回归通过；最终 head [85ed887](https://github.com/soulteary/yunqi/commit/85ed887bc85bf4de1a85317ba37da1b9b5b9778d) 的[完整 Verify](https://github.com/soulteary/yunqi/actions/runs/45003020851) 通过；[8c64425](https://github.com/soulteary/yunqi/commit/8c64425d6580caa8876e6a08af37e251d7ba388f) 已合并 |
| 08 补充 | [914b19c](https://github.com/soulteary/yunqi/commit/914b19cb82485f1b20088fcf2ab3528b477d7a18)：账户 HTTP actor 与发现 system helper 分离 | [#105 commits](https://github.com/soulteary/yunqi/pull/105/commits) | 修复本轮 helper 提取的身份合流问题，账号事务保留显式管理主体，账号及单渠道自动发现维持 system；回归通过，随[完整 Verify](https://github.com/soulteary/yunqi/actions/runs/45003020851) 通过并合并 |

所有功能提交的正文使用完整的 `https://github.com/<owner>/<repo>/issues/<id>` 或 `/pull/<id>` 链接并保留编号。参考外部项目时用 `Refs` 和实际语义说明；只有在相应仓库确实完成问题处理时才用 `Closes`，避免误关闭外部 issue。对既有实现只参考设计或回归场景时，应明确写出没有直接移植的部分。

## 上游社区内容与 Yunqi 的实际对应

| 上游内容 | 社区问题或改动 | Yunqi 的对应与差异 |
| --- | --- | --- |
| [new-api PR #5791](https://github.com/QuantumNous/new-api/pull/5791)、[PR #3345](https://github.com/QuantumNous/new-api/pull/3345) | API Key 周期配额、日/月限额和即时阻断 | 使用稳定 principal 预算、逐尝试预留结算与共享后端，Key 轮换不作为重置余额的方法 |
| [new-api issue #1730](https://github.com/QuantumNous/new-api/issues/1730)、[PR #6583](https://github.com/QuantumNous/new-api/pull/6583) | 渠道速率与并发准入 | 入口与渠道分别管理共享限额，保留本地 Key/Quota 自身保护；拒绝不得消耗未实际发送的本地窗口 |
| [new-api PR #8124](https://github.com/QuantumNous/new-api/pull/8124)、[issue #8123](https://github.com/QuantumNous/new-api/issues/8123) | Redis 限流 key 过期边界 | 租约可到期回收，但持久预算 epoch/索引不能作为普通缓存自动过期或静默重建 |
| [new-api issue #2841](https://github.com/QuantumNous/new-api/issues/2841) | 多步 Redis 读取与更新的高并发原子性 | WATCH 的读取并非快照；Yunqi 在拒绝前校验监测键一致性，冲突重试，不盲目重放已尝试的金融写事务 |
| [new-api PR #8350](https://github.com/QuantumNous/new-api/pull/8350)、[PR #6264](https://github.com/QuantumNous/new-api/pull/6264)、[issue #3240](https://github.com/QuantumNous/new-api/issues/3240) | 发送前保留严格会话亲和、并发路由及加密内容的资源绑定 | 以稳定渠道 UID 跨节点原子绑定；失败不迁移，不把完整 Responses 历史同步当作已完成 |
| [new-api PR #6620](https://github.com/QuantumNous/new-api/pull/6620) | 可配置 OAuth PKCE | 原生 OIDC 通过 `/access` 安全门户使用 PKCE S256，校验 state、nonce、签名与 AMR/ACR；IdP 执行 MFA/Passkey，Vue 管理页继续使用管理凭证 |
| [new-api PR #7028](https://github.com/QuantumNous/new-api/pull/7028)、[issue #6972](https://github.com/QuantumNous/new-api/issues/6972) | 会话数量达到上限后无法登录，改为淘汰最旧活跃会话 | 服务端会话淘汰与签发限额分离，另加匿名流程容量、共享撤销及策略变更隔离 |
| [new-api PR #8076](https://github.com/QuantumNous/new-api/pull/8076)、[issue #8034](https://github.com/QuantumNous/new-api/issues/8034) | 分组配置保存错误、nil 选项和持久化失败反馈 | 参考“验证后提交、失败不发布”的保存语义；完整状态 bundle 为 Yunqi 独立实现，不把上游分组保存问题表述为备份问题 |
| [new-api PR #5431](https://github.com/QuantumNous/new-api/pull/5431)、[PR #4329](https://github.com/QuantumNous/new-api/pull/4329)、[issue #4328](https://github.com/QuantumNous/new-api/issues/4328) | 成本归因面板、模型与用户维度统计 | 增加稳定主体维度和下钻；旧的掩码分组不被伪装为精确身份 |
| [new-api PR #7953](https://github.com/QuantumNous/new-api/pull/7953)、[PR #7955](https://github.com/QuantumNous/new-api/pull/7955)、[issue #7951](https://github.com/QuantumNous/new-api/issues/7951)、[issue #7952](https://github.com/QuantumNous/new-api/issues/7952) | 上游模型同步白名单与名称映射规则 | 补充导入策略、规范化和审核应用；与已有运行时 supportedModels 区分 |
| [new-api PR #8296](https://github.com/QuantumNous/new-api/pull/8296)、[PR #8233](https://github.com/QuantumNous/new-api/pull/8233)、[PR #4492](https://github.com/QuantumNous/new-api/pull/4492)、[issue #8295](https://github.com/QuantumNous/new-api/issues/8295)、[issue #8232](https://github.com/QuantumNous/new-api/issues/8232) | endpoint 元数据及映射场景的端点推断 | 统一存储和使用协议能力提示，实际探测与接入支持决定可调用性，不仅凭模型名猜协议 |
| [new-api PR #8357](https://github.com/QuantumNous/new-api/pull/8357) | shadow 默认的学习路由、质量/成本/延迟/重试反馈 | 保留 Yunqi 路由器，增加固定样本、独立质量标签和人工复审门槛，不直接引入 EXP3/UCB 算法 |
| [new-api PR #6673](https://github.com/QuantumNous/new-api/pull/6673)、[PR #8363](https://github.com/QuantumNous/new-api/pull/8363) | 热更新读写一致性、保存 OAuth 配置后再启用 | 管理变更采用显式操作关联和持久化提交边界；后台变更单独归因为 system，防止并发串 actor |

## 后续维护

每批先补能够复现原问题的回归，记录最终行为与边界，再记录可核验的功能 commit、修复 commit、项目 PR 和 CI。新的整合问题另开修复批次，保留首次失败的原因与修正，不把反复重跑当作验证。本文只维护具体实现和对应证据；参数细节留在环境/API 指南，避免复制整套配置说明。
