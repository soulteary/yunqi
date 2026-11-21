# Yunqi Go 后端

后端负责上游请求、协议转换、渠道调度、会话、指标与持久化，并嵌入 `frontend/` 的 Web 控制台。当前支持 Messages、Chat、Responses、Gemini、Images 和 Vectors 六类渠道。

## 开发与入口

推荐在仓库根目录使用 `make dev`、`make run` 和 `make build`。后端专项命令见 [Makefile](Makefile)；安装、Air 热重载、检查和排障统一见[开发指南](../docs/guide/development.md)。

进程入口和依赖边界：

- `main.go` / `cli.go`：CLI 分派与路径参数。
- `bootstrap*.go` / `background*.go`：装配、后台任务和恢复流程。
- `routes*.go`：真实路由和中间件；[API 指南](../docs/guide/api.md)提供鉴权、代理入口及密钥请求体接口说明。
- `server.go` / `tls_server.go`：服务监听、TLS 和信号关闭。
- `internal/`：业务模块；职责和请求流见[架构指南](../docs/guide/architecture.md)。

框架与编码约束见 [CLAUDE.md](CLAUDE.md)，全仓库约定见 [AGENTS.md](../AGENTS.md)。

## 配置与运行状态

默认相对运行目录使用 `.config/`；在本目录启动时通常是 `backend-go/.config/`。CLI 可以分别指定路径：

```bash
yunqi --config ~/.config/yunqi/config.json --statedir ~/.local/state/yunqi --logdir ~/.local/state/yunqi/logs
```

`--config` 只改变配置文件位置；`--statedir` 指定指标和恢复状态目录；`--logdir` 高于 `LOG_DIR`，可用 `none` 禁用文件日志。这些参数不搬迁 `.env`。完整配置与密钥约束见[环境变量指南](../docs/guide/environment.md)，系统服务部署见[服务指南](../docs/service/README.md)。

## 调度与日志

调度优先级、亲和性、上下文与 Embedding 兼容过滤统一见[架构中的调度说明](../docs/guide/architecture.md#调度优先级链路)。修改调度时同时检查 `internal/scheduler/` 和 `internal/metrics/`。

渠道日志模型位于 `internal/metrics/channel_log.go`。Images 的 `operation` 区分 generations、edits 和 variations；不同协议拥有独立指标空间。请求日志与鉴权日志不得包含完整密钥，旧 URL 密钥接口也必须脱敏。

## 版本与文档

版本只取自根 [VERSION](../VERSION)，构建由 Makefile 注入；发布记录只维护根 [CHANGELOG](../CHANGELOG.md)，不再另写后端模块日志。恢复发布流程前先阅读[当前发布状态](../docs/guide/release.md)。

详细设计见[规格索引](../docs/specs/README.md)；历史设计与交接见[归档](../archive/README.md)，不能用历史快照推断当前接口或待办。
