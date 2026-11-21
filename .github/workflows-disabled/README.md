# 暂停的发布工作流

Yunqi 初始化期间暂停继承的发布动作。本目录不在 GitHub Actions 的工作流扫描路径内，因此推送、标签、Release 或手动事件都不会执行这些文件。

- `deploy-docs.yml`：发布 GitHub Pages 文档站点。
- `release.yml`：构建、签名和发布二进制及桌面产物。
- `docker-build.yml`：向上游配置的镜像仓库发布镜像。
- `update-homebrew.yml`：向上游 Homebrew tap 发送更新。

保留 `.github/workflows/check-bindings.yml` 的 PR 绑定检查。暂停发布不影响本地 `make build`、前后端测试和文档构建。

恢复时，先将账号、仓库、镜像地址、签名身份、凭据、权限及构建范围调整为 Yunqi 的实际配置，再将需要的文件移回 `.github/workflows/`。不得直接恢复上游签名身份或 tap 推送目标。Wails 移除后应重新设计服务端发布流程，而非恢复桌面发布。
