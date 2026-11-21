# 非 Docker 自启动

本目录提供 Yunqi 直接运行可执行文件时的自启动示例。Docker 部署推荐使用根目录的 `docker-compose.yml` 和 `docker-compose.watchtower.yml`。

## Linux systemd

适合 Linux 服务器长期运行。

### 1. 准备文件

从 Release 下载 Linux 可执行文件，并放到 `/opt/yunqi`：

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin yunqi
sudo mkdir -p /opt/yunqi
sudo cp yunqi-linux-amd64 /opt/yunqi/
sudo chmod +x /opt/yunqi/yunqi-linux-amd64
```

在 `/opt/yunqi/.env` 写入运行配置：

```bash
PROXY_ACCESS_KEY=your-proxy-access-key
PORT=3688
ENABLE_WEB_UI=true
APP_UI_LANGUAGE=zh-CN
ENV=production
LOG_LEVEL=warn
```

如果希望按 XDG 习惯拆分配置与日志，可以在 service 的 `ExecStart` 中追加命令行参数，例如：

```ini
ExecStart=/opt/yunqi/yunqi-linux-amd64 --config /etc/yunqi/config.json --statedir /var/lib/yunqi --logdir /var/log/yunqi
# 或使用 --logdir none 禁用日志文件写入，完全交由 journald 管理日志
# ExecStart=/opt/yunqi/yunqi-linux-amd64 --config /etc/yunqi/config.json --statedir /var/lib/yunqi --logdir none
```

普通用户服务也可以使用用户目录，例如 `--config ~/.config/yunqi/config.json --statedir ~/.local/state/yunqi --logdir ~/.local/state/yunqi/logs`。`--config` 只改变配置文件位置；`--statedir` 统一指定 `metrics.db`、`conversation_state.json`、`scheduled_recovery_state.json` 的目录，未指定时保持默认 `.config`；`--logdir` 只影响应用日志目录。

对于 `yunqi@.service` 这类用户级模板服务，可以把实例名用于区分配置目录：

```ini
ExecStart=/usr/local/bin/yunqi --config %h/.config/yunqi/%I/config.json --statedir %h/.local/state/yunqi/%I --logdir %h/.local/state/yunqi/%I/logs
WorkingDirectory=%h/.local/state/yunqi/%I/
```

设置目录权限：

```bash
sudo chown -R yunqi:yunqi /opt/yunqi
```

### 2. 安装服务

```bash
sudo cp docs/service/yunqi.service /etc/systemd/system/yunqi.service
sudo systemctl daemon-reload
sudo systemctl enable --now yunqi
```

### 3. 查看状态和日志

```bash
sudo systemctl status yunqi
journalctl -u yunqi -f
```

### 4. 更新二进制

```bash
sudo systemctl stop yunqi
sudo cp yunqi-linux-amd64 /opt/yunqi/yunqi-linux-amd64
sudo chmod +x /opt/yunqi/yunqi-linux-amd64
sudo chown yunqi:yunqi /opt/yunqi/yunqi-linux-amd64
sudo systemctl start yunqi
```

## macOS launchd

适合 macOS 本机后台运行。原 `yunqi-desktop` Homebrew Cask 已退出维护，不再自动更新桌面 Tap。发布资源可用后，从 GitHub Releases 获取对应架构的服务二进制并校验 SHA256，按组织的软件分发策略安装；当前发布状态见[发布说明](../guide/release.md)。旧版数据迁移见[迁移指南](../guide/desktop/index.md)。

### 1. 准备文件

从 Release 下载 macOS 可执行文件，并放到用户目录，例如：

```bash
mkdir -p ~/yunqi/logs
cp yunqi-darwin-arm64 ~/yunqi/
chmod +x ~/yunqi/yunqi-darwin-arm64
```

编辑 `docs/service/com.yunqi.gateway.plist`，将所有 `/Users/your-user/yunqi` 替换为实际路径。

### 2. 安装 LaunchAgent

```bash
cp docs/service/com.yunqi.gateway.plist ~/Library/LaunchAgents/com.yunqi.gateway.plist
launchctl unload ~/Library/LaunchAgents/com.yunqi.gateway.plist 2>/dev/null || true
launchctl load ~/Library/LaunchAgents/com.yunqi.gateway.plist
launchctl start com.yunqi.gateway
```

### 3. 查看和停止

```bash
launchctl list | grep com.yunqi.gateway
launchctl stop com.yunqi.gateway
launchctl unload ~/Library/LaunchAgents/com.yunqi.gateway.plist
```

日志默认写入：

```text
~/yunqi/logs/stdout.log
~/yunqi/logs/stderr.log
```

## Windows NSSM

适合 Windows 服务器或桌面环境后台运行。详见 `docs/service/windows-nssm.md`。

基本流程：

```powershell
nssm install yunqi C:\yunqi\yunqi-windows-amd64.exe
nssm set yunqi AppDirectory C:\yunqi
nssm set yunqi AppEnvironmentExtra PROXY_ACCESS_KEY=your-proxy-access-key PORT=3688 ENABLE_WEB_UI=true APP_UI_LANGUAGE=zh-CN
nssm set yunqi Start SERVICE_AUTO_START
nssm start yunqi
```

## 自动更新建议

非 Docker 部署暂不建议程序内自更新。更稳妥的方式是手动替换二进制并重启服务：

- Linux: `systemctl stop yunqi` 后替换 `/opt/yunqi/yunqi-linux-amd64`，再 `systemctl start yunqi`
- macOS: `launchctl stop com.yunqi.gateway` 后替换可执行文件，再 `launchctl start com.yunqi.gateway`
- Windows: `nssm stop yunqi` 后替换 exe，再 `nssm start yunqi`

如需要自动更新，优先使用 Docker + Watchtower。
