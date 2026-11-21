# Web 控制台与桌面版迁移

Yunqi 已移除 Wails 桌面外壳，统一使用 Go 服务内置的 Web 控制台。下载安装服务二进制或使用 Docker 后，在浏览器访问实际配置的端口。

## 开始使用

1. 在部署环境设置 `PROXY_ACCESS_KEY`；需要区分管理权限时另设 `ADMIN_ACCESS_KEY`。
2. 启动 Yunqi，在浏览器使用管理密钥登录。
3. 添加并测试上游渠道，再进入「服务与接入」生成客户端配置。
4. 在客户端发起请求，在「会话」查看请求与调度记录。

客户端配置由用户复制或下载后合并到本机文件；控制台不会自动修改浏览器所在电脑的配置。

## 从旧桌面版迁移

迁移前保留旧安装和全部数据。原配置 JSON 和数据库格式不变，无需转换。

旧桌面版通常使用以下目录；自定义部署及 Windows Store 虚拟化路径可能不同，应以原版本显示的数据目录为准：

- macOS：`~/Library/Application Support/ccx-desktop`
- Windows：用户配置目录下的 `ccx-desktop`（通常为 `%APPDATA%\ccx-desktop`）
- Linux：`${XDG_STATE_HOME:-$HOME/.local/state}/ccx`

先备份环境文件、配置、状态数据库及其关联文件。由管理员停止旧版本写入后，再将数据复制到新的空目录；运行中的 SQLite 数据库应使用一致性备份，不能只复制主数据库而忽略 WAL。

服务启动参数可继续使用：

```bash
ccx --config /path/to/config.json --statedir /path/to/state --logdir /path/to/logs --backupdir /path/to/backups
```

`.env` 的加载位置不会因这些参数而自动改变。按[环境变量说明](../environment)配置原有代理密钥、管理密钥及端口，或通过进程管理器显式传入环境变量。

首次启动后核对六类渠道数量、模型映射、密钥、订阅和历史统计。不要覆盖已有目标目录，也不要立即删除旧数据；回退前停止新实例写入，避免两个实例同时操作同一数据库。

## 运维与排障

使用 [systemd / launchd / Windows 服务](https://github.com/soulteary/yunqi/tree/main/docs/service) 或 Docker Compose 管理服务生命周期。控制台提供运行状态、配置生效说明、健康检查入口和无密钥诊断导出；服务日志通过部署端日志工具查看。

- 渠道配置支持热重载。
- 环境变量变更后，由管理员通过部署工具重启服务。
- 无法连接时核对实际端口、监听地址和防火墙；远程客户端不能使用服务机器的 `localhost`。
- 服务二进制保留 macOS codesign 和 Windows SignPath 签名；桌面安装包与应用内更新已移除。
