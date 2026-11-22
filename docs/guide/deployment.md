# 部署指南

## Docker Compose

在已克隆的仓库中执行部署。自动发布暂停期间使用本地镜像构建，不依赖 GHCR 产物；发布情况见[发布状态](release.md)。

```yaml
services:
  yunqi:
    image: yunqi:local
    build:
      context: .
      dockerfile: Dockerfile
      target: runtime
    ports:
      - '3000:3000'
    volumes:
      - ./.config:/app/.config
    environment:
      - ENV=production
      - PROXY_ACCESS_KEY=your-proxy-key
      # 额外代理访问密钥（可选，逗号分隔；启用后必须设置独立 ADMIN_ACCESS_KEY）
      # - EXTRA_PROXY_ACCESS_KEYS=extra-proxy-key-1,extra-proxy-key-2
      # 管理 API 独立密钥（可选，未设置时回退到 PROXY_ACCESS_KEY）
      # - ADMIN_ACCESS_KEY=your-admin-secret-key
    restart: unless-stopped
```

## 系统服务

### Linux (systemd)

```ini
[Unit]
Description=Yunqi AI API Gateway
After=network.target

[Service]
Type=simple
ExecStart=/opt/yunqi/yunqi
WorkingDirectory=/opt/yunqi
Environment=PROXY_ACCESS_KEY=your-proxy-key
# 额外代理访问密钥（可选，逗号分隔；启用后必须设置独立 ADMIN_ACCESS_KEY）
#Environment=EXTRA_PROXY_ACCESS_KEYS=extra-proxy-key-1,extra-proxy-key-2
# 管理 API 独立密钥（可选，未设置时回退到 PROXY_ACCESS_KEY）
#Environment=ADMIN_ACCESS_KEY=your-admin-secret-key
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

### macOS (launchd)

参考 `docs/service/com.yunqi.gateway.plist` 配置文件。

## 环境变量

配置统一见[环境变量指南](environment.md)，服务模板及 Linux、macOS、Windows 操作步骤见[服务部署指南](../service/README.md)。

完整状态的离线备份、校验与隔离恢复流程见[备份与恢复](recovery.md)。
