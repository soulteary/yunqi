# 完整状态备份与恢复

`yunqi-state` 是离线恢复工具，不启动、停止或重载 Yunqi。它与服务共享配置目录和状态目录的排他锁；目录正在使用时立即失败。旧版本及外部脚本不识别该锁，因此备份必须由运维先确认所有写入者均已退出，`--offline` 是这一确认，不会代替用户停止服务。需要零停机备份的部署应使用存储层的一致快照，不要在运行中复制几个数据库文件。

从仓库构建工具：

```sh
cd backend-go
go build -o yunqi-state ./cmd/yunqi-state
./yunqi-state backup --offline --config /srv/yunqi/config/config.json --statedir /srv/yunqi/state --secrets-file /srv/yunqi/service.env --output /srv/backups/yunqi-2026-11-22
./yunqi-state verify --bundle /srv/backups/yunqi-2026-11-22
./yunqi-state restore --bundle /srv/backups/yunqi-2026-11-22 --output /srv/recovery/yunqi-2026-11-22
```

输出目录必须不存在，且备份不能位于源配置或状态目录内。恢复后的启动路径为 `--config /srv/recovery/yunqi-2026-11-22/config/config.json --statedir /srv/recovery/yunqi-2026-11-22/state`。工具不自动启动服务；请先在隔离环境演练，再由运维切换部署。

## 包含的数据

- 指定的配置文件和配置同级 `entry-budget.db`，保留入口 Key、权限、预算、身份撤销及配置中的凭证。即使配置和状态目录分开，预算数据库也不会遗漏。
- 状态目录的普通文件，包括 `metrics.db`、`autopilot.db`、`thinking_cache.db`、会话/恢复状态和 `presets/` 的价格审核、完整预置 bundle。
- 显式 `--secrets-file` 指定的环境文件，恢复为根目录下 `secrets.env`；未指定时不会猜测或读取 `.env`。由部署系统、密钥管理器或进程环境注入的凭证须另行安全备份。恢复工具不会加载此文件或激活其中的密钥。

`backups/`、`logs/`、锁文件以及 SQLite WAL/SHM 副文件不直接打包。SQLite 使用 `VACUUM INTO` 生成包含已提交 WAL 的独立快照，并执行完整性检查；不把只复制主数据库当作完整备份。目录符号链接、特殊文件、超过 10,000 个文件或合计 16 GiB 的状态会失败。

## 验证与恢复演练

manifest 包含版本、UTC 时间、逐文件 SHA-256、大小和 SQLite 标记。恢复前验证完整清单、路径、校验和及数据库完整性；拒绝路径穿越、额外文件、重复项和损坏。文件权限为 0600，目录为 0700。校验和用于发现损坏，不是签名；备份含凭证，应放在有访问控制和加密的存储中。

恢复只创建新目录，绝不覆盖活动部署。失败会清理本次输出；中断留下的不完整标记会阻止服务打开该状态。演练至少检查：预算已用和预留金额、请求成本历史、价格审核与回滚版本、Key/角色范围、已撤销身份不能重新登录。重新开放入口前检查尚在预留的旧请求按保守规则恢复，不能通过删除账本来释放额度。

## 外部状态与多副本

本地 bundle **不包含 Redis、IdP、密钥管理服务或供应商侧状态**。多副本部署应先让所有写入者静止，为外部状态取得同一恢复点，再使用 `--external-snapshot SNAPSHOT_ID` 在 manifest 记录该存储层快照的引用。此参数仅作关联，不创建、验证或恢复外部快照。

恢复时必须校验各共享账本/路由/撤销命名空间的 epoch 与配置匹配，禁止通过重新初始化或清空 Redis“修复”不匹配。恢复旧撤销数据可能重新允许备份后已退出的会话；无法证明撤销记录完整时应轮换身份信任/会话域并强制重新登录，不能沿用旧会话。未配置共享账本的独立 SQLite 副本不构成统一预算。请在恢复记录中保留外部快照 ID、部署的版本及演练结果。
