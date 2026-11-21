# Deployment

## Docker Compose

```yaml
services:
  yunqi:
    image: ghcr.io/soulteary/yunqi:latest
    ports:
      - '3000:3000'
    volumes:
      - ./.config:/app/.config
    environment:
      - ENV=production
      - PROXY_ACCESS_KEY=your-proxy-key
      # Extra proxy access keys (optional, comma-separated; requires independent ADMIN_ACCESS_KEY)
      # - EXTRA_PROXY_ACCESS_KEYS=extra-proxy-key-1,extra-proxy-key-2
      # Admin API key (optional, falls back to PROXY_ACCESS_KEY if not set)
      # - ADMIN_ACCESS_KEY=your-admin-secret-key
    restart: unless-stopped
```

## System Service

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
# Extra proxy access keys (optional, comma-separated; requires independent ADMIN_ACCESS_KEY)
#Environment=EXTRA_PROXY_ACCESS_KEYS=extra-proxy-key-1,extra-proxy-key-2
# Admin API key (optional, falls back to PROXY_ACCESS_KEY if not set)
#Environment=ADMIN_ACCESS_KEY=your-admin-secret-key
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

### macOS (launchd)

See `docs/service/com.yunqi.gateway.plist` for reference.

## Environment Variables

Configuration is documented in the [environment guide](environment.md). Service templates and Linux/macOS/Windows procedures are in the [service guide](../../service/README.md) (Chinese).
