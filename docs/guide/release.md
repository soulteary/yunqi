# 发布状态

Yunqi 初始化期间，文档站点、二进制/桌面、Docker 镜像和 Homebrew 的自动发布均暂停。工作流保留在 `.github/workflows-disabled/`，不在 GitHub Actions 扫描路径内。当前保留 PR 绑定检查。

根目录 `VERSION` 继续作为版本源。本地可通过 `make build` 构建服务，通过 `docker compose up --build -d` 使用本地镜像；文档可在 `docs/` 执行 `bun run build`。这不代表 GitHub Releases、GHCR 镜像或 Pages 站点已经可用。

待项目范围和发布目标确定后，重新配置维护者自己的仓库、镜像、签名和权限，再按 `.github/workflows-disabled/README.md` 恢复所需流程。上游凭据、签名身份和 Homebrew tap 不能直接沿用。
