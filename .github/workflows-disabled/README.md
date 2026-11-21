# 暂停的发布工作流

Yunqi 初始化期间暂停继承的发布动作。本目录不在 GitHub Actions 的工作流扫描路径内，因此推送、标签、Release 或手动事件都不会执行这些文件。

- `deploy-docs.yml`：发布 GitHub Pages 文档站点。
- `release.yml`：构建、签名和发布服务二进制。
- `docker-build.yml`：向上游配置的镜像仓库发布镜像。

桌面绑定检查和 Homebrew 工作流已随 Wails 移除；`.github/workflows/ci.yml` 仅负责 PR/主分支验证。暂停发布不影响本地 `make build`、前后端测试和文档构建。

恢复时，先将账号、仓库、镜像地址、签名身份、凭据、权限及构建范围调整为 Yunqi 的实际配置，再将需要的文件移回 `.github/workflows/`。不得直接恢复上游签名身份或 tap 推送目标。服务端发布流程已迁入归档，仍需重新配置后才能恢复。
