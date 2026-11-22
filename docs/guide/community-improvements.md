# Yunqi 社区参考改进记录

本页记录 2026 年 11 月 22 日起，基于 `56fb5d2` 的跨节点治理、身份、恢复与可观测性改进。每批列出 Yunqi 的实际行为、功能或修复提交、回归证据，以及参考的上游 issue 和 PR。上游 PR 的设计或问题描述只作为参考，不等于其已合并、已发布，也不表示 Yunqi 关闭了上游 issue。

功能状态以本项目 PR 的合并结果为准。部署参数和能力边界分别见[环境配置](environment.md)、[API](api.md)、[完整状态恢复](recovery.md)。代码实现、回归补丁及后续集成修复分批保留在 Git 历史中。

## 功能改善

| 批次 | 原有问题或缺口 | 本轮改善 | 范围与项目 PR |
| --- | --- | --- | --- |
| 01 状态恢复 | 配置备份不包含完整账本、SQLite WAL 和价格安全状态 | 双目录排他锁、完整本地状态冷备、逐文件校验、数据库完整性验证、新目录恢复 | [#99](https://github.com/soulteary/yunqi/pull/99) 已合并；外部 Redis/IdP 仍须协调快照，不会自动重载部署 |
| 02 共享预算 | 各节点独立 SQLite 无法约束团队总预算 | Redis 原子预留/结算、精确整数金额、逐尝试价格与 usage、不变账单归档、已有本地余额切换保护 | [#102](https://github.com/soulteary/yunqi/pull/102) 已合并；并发建账的后续一致性修复正在验证；默认仍为本地账本 |
| 03 原生身份 | OIDC 登录和 MFA/Passkey 全部依赖外置代理 | Yunqi 原生 AuthCode + PKCE、服务端会话、AMR/ACR 强度校验、角色范围和共享撤销 | [#100](https://github.com/soulteary/yunqi/pull/100) 验证中；MFA/Passkey 仍由 IdP 执行，可信代理继续可用 |
| 04 共享路由 | 渠道限额与严格会话绑定各节点独立 | 共享显式渠道 RPM/并发、发送前原子会话绑定、活跃 Body 续租、状态丢失拒绝放行 | [#101](https://github.com/soulteary/yunqi/pull/101) 已合并；Key/Quota 与发现值保留本地保护，不复制 Responses 完整历史 |
| 05 路由评估 | 缺少固定样本下的质量、成本和延迟对照 | 独立质量标签与实际成本、分层门槛、P95、平均重试、持久化证据与内容摘要重放 | [#103](https://github.com/soulteary/yunqi/pull/103) 验证中；只进入人工复审，不自动改变在线策略 |
| 06 主体成本 | Key 轮换后难以统一长期成本归因 | 稳定主体与凭证身份分域、持久化归因、模型/Key/请求下钻及 CSV 对齐 | 实现与回归中；旧记录保留 unknown，不从脱敏 Key 猜测归属 |
| 07 模型治理 | 模型发现、导入纳入规则、名称映射与端点信息未形成统一审核流程 | 渠道级导入策略、规范化、差异预览与审核应用、统一 endpoint 元数据 | 实现与回归中；以实际接入的协议与探测证据为准 |
| 08 统一审计 | 不同管理模块各自审计，配置变更缺少统一 actor 与操作关联 | 显式 actor/operationId、配置与成功审计原子保存、请求意图/结果和有界查询 | 实现与回归中；历史与字段差异脱敏，不把 HTTP 接受等同于后台任务完成 |

## 问题修正

| 问题 | 影响 | 修正与回归 | 所属批次 |
| --- | --- | --- | --- |
| 冷备遗漏配置目录旁的预算库 | 配置与状态目录分离时恢复丢失已用额度 | 显式纳入 `entry-budget.db`；验证已提交 WAL、预算金额与安全状态恢复 | 01 / #99 |
| 备份或恢复输出路径指向源目录 | 符号链接可绕过路径限制，或恢复污染原 bundle | 比较真实路径，拒绝嵌套输出、符号链接根、路径穿越及额外文件 | 01 / #99 |
| 相对路径构造 SQLite URI 错误 | 默认目录备份、相对 bundle 校验失败 | 数据库路径先转绝对路径；完整相对路径 backup/verify/restore 回归 | 01 / #99 |
| 配置和状态目录别名造成重复锁 | 同一物理目录被本进程加锁两次，启动误报繁忙 | 解析真实目录后排序去重；验证第二实例拒绝及锁释放恢复 | 01 / #99 |
| 已结算历史达到主账本容量后永久拒绝 | 长期请求积累使共享预算不可继续使用 | 原子归档结算请求，保留周期金额及滚动窗口索引；10,001 条历史回归 | 02 / #102 |
| 离线初始化阻塞扫描 Redis 全库 | 对同一 Redis 的其他命名空间造成不必要阻塞 | 使用增量 SCAN，保留孤立账本拒绝和 epoch 验证 | 02 / #102 |
| WATCH 分步读取被并发写入打断 | 首次建账时把暂时不一致误判为账本损坏 | 已确定性复现首次并发建账与并发退款两种混合读取，后续修复验证中；这是 Yunqi 本轮实现问题，不据此推断上游存在同一缺陷 | 02 |
| 旧共享会话沿用较低认证强度 | 收紧 MFA/Passkey 策略后仍可能接受旧会话 | 认证策略摘要和撤销 epoch 绑定流程、会话与 realm；策略收紧回归 | 03 / #100 |
| 匿名待完成 OIDC 流程无限增加 | 共享 Redis 会话状态缺少容量边界 | 每 realm 4,096 个、消费释放、过期恢复；满载在访问 IdP 前拒绝 | 03 / #100 |
| 临时缩短会话期限导致撤销提前失效 | 退出后再次放宽策略可能复活原 Cookie | 撤销保留原存储有效期，当前策略只作局部有效期判断；local/shared 时间推进回归 | 03 / #100 |
| 管理员撤销他人会话误清自己的 Cookie | 执行管理操作后意外退出 | 仅目标指纹匹配当前会话时清 Cookie；本人/他人回归 | 03 / #100 |
| 共享准入拒绝仍消耗本地 RPM | 共享容量释放后，节点继续被本地窗口阻塞 | 显式限制由 Redis 单一管理，保留自身 Key/Quota 与发现限制；双节点反复拒绝后立即恢复回归 | 04 / #101 |
| 丢失单个严格会话绑定后重新选路 | epoch 尚在时可能跨供应商资源迁移 | 未过期索引存在但绑定缺失则拒绝；单记录删除与全状态清空回归 | 04 / #101 |
| 把成功率或缺失成本视为质量/零费用 | 路由评估错误宣布收益 | 缺少独立质量或真实价格时证据不足；每个场景分层独立检查 | 05 / #103 |
| 重试均值误命名为比例 | 每请求多次重试时指标可超过 1，含义不清 | 使用 meanRetries，覆盖平均 1.5 次与重试请求占比 0.5 的区别 | 05 / #103 |
| 新 API 未纳入路由契约清单 | 完整 CI 无法确认路由表兼容 | 显式更新三条评估路由，验证生产 Web/API 与开发模式 | 05 / #103 |

## 分批提交与验证

表中只列本轮功能与修复提交；同步 main 的 merge 提交保留在对应 PR 的完整提交列表中，不重复当作新功能。未合并批次的最终提交、CI 和状态会在本轮完成后补齐。

| 批次 | 功能与修复 commit | 本项目 PR | 回归与完整 CI |
| --- | --- | --- | --- |
| 01 | [23dc804](https://github.com/soulteary/yunqi/commit/23dc804ac8694a8494932c16f21471e417b29b49)：冷备与隔离恢复；[7a6977c](https://github.com/soulteary/yunqi/commit/7a6977c655da3588ec0fd384e3e04973651cac74)：相对路径与锁别名 | [#99 commits](https://github.com/soulteary/yunqi/pull/99/commits) | WAL、split paths、损坏、穿越、活动目录拒绝通过；[完整 Verify](https://github.com/soulteary/yunqi/actions/runs/44992303383) 通过 |
| 02 | [74899dc](https://github.com/soulteary/yunqi/commit/74899dcc528b6d94f3c1a036c4a8185cefec29a9)：共享账本；[c854f17](https://github.com/soulteary/yunqi/commit/c854f17c8f3f632dad48edaa195c152432041518)：增量初始化扫描 | [#102 commits](https://github.com/soulteary/yunqi/pull/102/commits) | 真实 Redis 双节点预算、逐尝试结算、大整数、归档与状态丢失通过；[完整 Verify](https://github.com/soulteary/yunqi/actions/runs/44993310368) 通过；后续并发修复单独记录 |
| 03 | [5885e2e](https://github.com/soulteary/yunqi/commit/5885e2e00e41b5b00275afb97af142c37bc31485)：原生 OIDC；[10adfe3](https://github.com/soulteary/yunqi/commit/10adfe30b217a75e7ebbbff16e989f48e5579ee1)：匿名流程容量；[2edd44f](https://github.com/soulteary/yunqi/commit/2edd44f14c2ff1f132901862f1ab970f786ba421)：撤销生命周期 | [#100 commits](https://github.com/soulteary/yunqi/pull/100/commits) | fake TLS IdP、跨节点会话、重放、策略收紧、退出与容量回归通过；等待最新整合 CI |
| 04 | [ef3c3be](https://github.com/soulteary/yunqi/commit/ef3c3be96c1ea680457a7d3e303ebcdfa2fae478)：共享路由；[9decc76](https://github.com/soulteary/yunqi/commit/9decc760e2667e3b06e32ae66eba0ff12454af5b)：RPM 单一所有权 | [#101 commits](https://github.com/soulteary/yunqi/pull/101/commits) | 真实 Redis 独立节点、首次原子绑定、断连/续租、状态丢失及容量恢复通过；[完整 Verify](https://github.com/soulteary/yunqi/actions/runs/44994509959) 通过 |
| 05 | [eaa01b2](https://github.com/soulteary/yunqi/commit/eaa01b2695a95831acdef9b2cb2821b325096266)：固定样本评估；[56a658a](https://github.com/soulteary/yunqi/pull/103/commits)：路由契约清单 | [#103 commits](https://github.com/soulteary/yunqi/pull/103/commits) | 分层、未知计量、重放、权限、持久化和 HTTP 保存导出通过；整合 CI 暴露的预算并发问题单独修复 |

所有功能提交的正文使用完整的 `https://github.com/<owner>/<repo>/issues/<id>` 或 `/pull/<id>` 链接并保留编号。参考外部项目时用 `Refs` 和实际语义说明；只有在相应仓库确实完成问题处理时才用 `Closes`，避免误关闭外部 issue。对既有实现只参考设计或回归场景时，应明确写出没有直接移植的部分。

## 上游社区内容与 Yunqi 的实际对应

| 上游内容 | 社区问题或改动 | Yunqi 的对应与差异 |
| --- | --- | --- |
| [new-api PR #5791](https://github.com/QuantumNous/new-api/pull/5791)、[PR #3345](https://github.com/QuantumNous/new-api/pull/3345) | API Key 周期配额、日/月限额和即时阻断 | 使用稳定 principal 预算、逐尝试预留结算与共享后端，Key 轮换不作为重置余额的方法 |
| [new-api issue #1730](https://github.com/QuantumNous/new-api/issues/1730)、[PR #6583](https://github.com/QuantumNous/new-api/pull/6583) | 渠道速率与并发准入 | 入口与渠道分别管理共享限额，保留本地 Key/Quota 自身保护；拒绝不得消耗未实际发送的本地窗口 |
| [new-api PR #8124](https://github.com/QuantumNous/new-api/pull/8124)、[issue #8123](https://github.com/QuantumNous/new-api/issues/8123) | Redis 限流 key 过期边界 | 租约可到期回收，但持久预算 epoch/索引不能作为普通缓存自动过期或静默重建 |
| [new-api PR #8350](https://github.com/QuantumNous/new-api/pull/8350)、[PR #6264](https://github.com/QuantumNous/new-api/pull/6264)、[issue #3240](https://github.com/QuantumNous/new-api/issues/3240) | 发送前保留严格会话亲和、并发路由及加密内容的资源绑定 | 以稳定渠道 UID 跨节点原子绑定；失败不迁移，不把完整 Responses 历史同步当作已完成 |
| [new-api PR #6620](https://github.com/QuantumNous/new-api/pull/6620) | 可配置 OAuth PKCE | 原生 OIDC 固定使用 PKCE S256，并独立校验 state、nonce、签名和认证强度 |
| [new-api PR #7028](https://github.com/QuantumNous/new-api/pull/7028)、[issue #6972](https://github.com/QuantumNous/new-api/issues/6972) | 会话数量达到上限后无法登录，改为淘汰最旧活跃会话 | 服务端会话淘汰与签发限额分离，另加匿名流程容量、共享撤销及策略变更隔离 |
| [new-api PR #8076](https://github.com/QuantumNous/new-api/pull/8076)、[issue #8034](https://github.com/QuantumNous/new-api/issues/8034) | 分组配置保存错误、nil 选项和持久化失败反馈 | 参考“验证后提交、失败不发布”的保存语义；完整状态 bundle 为 Yunqi 独立实现，不把上游分组保存问题表述为备份问题 |
| [new-api PR #5431](https://github.com/QuantumNous/new-api/pull/5431)、[PR #4329](https://github.com/QuantumNous/new-api/pull/4329) | 成本归因面板、模型与用户维度统计 | 增加稳定主体维度和下钻；旧的掩码分组不被伪装为精确身份 |
| [new-api PR #7953](https://github.com/QuantumNous/new-api/pull/7953)、[PR #7955](https://github.com/QuantumNous/new-api/pull/7955) | 上游模型同步白名单与名称映射规则 | 补充导入策略、规范化和审核应用；与已有运行时 supportedModels 区分 |
| [new-api PR #8296](https://github.com/QuantumNous/new-api/pull/8296)、[PR #8233](https://github.com/QuantumNous/new-api/pull/8233)、[PR #4492](https://github.com/QuantumNous/new-api/pull/4492) | endpoint 元数据及映射场景的端点推断 | 统一存储和使用协议能力提示，实际探测与接入支持决定可调用性，不仅凭模型名猜协议 |
| [new-api PR #8357](https://github.com/QuantumNous/new-api/pull/8357) | shadow 默认的学习路由、质量/成本/延迟/重试反馈 | 保留 Yunqi 路由器，增加固定样本、独立质量标签和人工复审门槛，不直接引入 EXP3/UCB 算法 |
| [new-api PR #6673](https://github.com/QuantumNous/new-api/pull/6673)、[PR #8363](https://github.com/QuantumNous/new-api/pull/8363) | 热更新读写一致性、保存 OAuth 配置后再启用 | 管理变更采用显式操作关联和持久化提交边界；后台变更单独归因为 system，防止并发串 actor |

## 后续维护

每批先补能够复现原问题的回归，记录最终行为与边界，再记录可核验的功能 commit、修复 commit、项目 PR 和 CI。新的整合问题另开修复批次，保留首次失败的原因与修正，不把反复重跑当作验证。本文只维护具体实现和对应证据；参数细节留在环境/API 指南，避免复制整套配置说明。
