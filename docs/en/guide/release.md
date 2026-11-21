# Release status

Publishing remains paused for Yunqi. Pages, service binary and Docker workflows are archived under `.github/workflows-disabled/`, outside the GitHub Actions scan path. Only PR/main verification runs automatically.

`VERSION` remains the single version source. The archived service workflow targets Linux, macOS and Windows on amd64/arm64 with embedded Web assets and SHA256 checksums; it no longer builds desktop installers. Before restoring it, configure Yunqi repository targets, registries, signing identities, credentials and permissions. Existing upstream signing identities must not be reused.

Local builds remain available through `make build`, `docker compose up --build -d` and `bun run build` inside `docs/`. These commands do not imply that releases, container images or Pages have been published. Proxy APIs, configuration JSON and state database formats remain compatible. See [migration](desktop/) for older desktop data.
