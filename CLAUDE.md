# Claude Code 协作入口

先阅读并遵守 [AGENTS.md](AGENTS.md)。项目范围、事实源、构建与验证命令、安全约定和服务生命周期约束统一在那里维护，本文件不重复接口或环境变量表。

## 模块入口

- [后端专项约定](backend-go/CLAUDE.md)：Provider、调度、日志和 HTTP 传输边界。
- [前端专项约定](frontend/CLAUDE.md)：业务 API 组合、页面控制器、Vuetify 和图标注册。
- [开发与验证](docs/guide/development.md)：使用根 Makefile 和各模块脚本。
- [API 与鉴权](docs/guide/api.md)：代理/管理密钥及接口入口；实际路由以 `backend-go/routes*.go` 为准。
- [环境变量](docs/guide/environment.md)、[架构](docs/guide/architecture.md)、[发布状态](docs/guide/release.md)。
- [当前规格](docs/specs/README.md)、[未完成事项](TODO.md)、[历史归档](archive/README.md)。

前端依赖只维护 `frontend/bun.lock`，使用 Bun 安装和升级；文档站单独使用 `docs/package-lock.json` 与 npm CI。不要生成根目录或前端的 npm/pnpm 锁文件。

默认用户已运行服务；不得自行停止、杀进程或重启。代码和配置改动可能触发开发服务热重载，需要隔离验证时使用独立工作副本。
