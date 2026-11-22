# 开发指南

本文档说明 Yunqi 的本地开发方式、常用命令和验证流程。

> 相关文档：
> - 架构设计：[architecture.md](architecture.md)
> - 环境变量：[environment.md](environment.md)
> - 发布流程：[release.md](release.md)

## 环境准备

### 前置依赖

| 工具 | 最低版本 | 安装方式 (macOS) | 安装方式 (Linux) | 说明 |
| --- | --- | --- | --- | --- |
| **Go** | 1.26 | `brew install go` | [go.dev/dl](https://go.dev/dl/) | 后端编译与运行 |
| **Bun** | 1.x | `brew install oven-sh/bun/bun` | `curl -fsSL https://bun.sh/install \| bash` | 前端依赖管理与构建 |
| **Git** | - | Xcode CLT 自带 | `apt install git` / `yum install git` | 版本管理 |
| **Make** | - | Xcode CLT 自带 | 通常已预装 | 构建脚本 |

macOS 用户如果还没有 Xcode Command Line Tools，先执行：

```bash
xcode-select --install
```

### 一键安装所有依赖

项目根目录 Makefile 提供了 `make install`，可自动安装前端依赖、Go 模块和开发工具（Air 热重载）：

```bash
make install
```

### 验证环境

```bash
go version     # 应 >= 1.26
bun --version  # 应 >= 1.x
make help      # 确认可用命令
```

## 推荐开发方式

| 方式 | 适用场景 | 说明 |
| --- | --- | --- |
| 根目录 Make | 日常联调 | 同时启动前端开发服务器和后端热重载 |
| backend-go Make | 后端专项开发 | 只运行 Go 后端命令 |
| frontend Bun | 前端专项开发 | 只运行 Vue/Vite 前端命令 |
| Docker | 接近生产环境验证 | 不适合作为主要热重载开发方式 |

## 方式一：根目录开发（推荐）

根目录 `Makefile` 是联调开发的事实来源。

```bash
make help
make dev
make run
make frontend-dev
make build
make clean
```

说明：
- `make dev`：启动前端开发服务器，并在 `backend-go/` 下以热重载模式运行 Go 后端
- `make run`：构建前端并运行后端
- `make build`：构建前端并编译 Go 后端
- `make frontend-dev`：仅启动前端开发服务器

## 方式二：backend-go 目录开发

`backend-go/Makefile` 是后端命令的事实来源。

```bash
cd "backend-go"
make help
make dev
make run
make build
make test
make test-cover
make fmt
make lint
make deps
```

说明：
- `make dev`：使用 Air 热重载
- `make run`：通过统一内容缓存校验/构建并嵌入前端，再运行后端
- `make build`：校验/构建并嵌入前端，再构建生产二进制到仓库根目录 `dist/`
- `make test`：运行所有 Go 测试
- `make test-cover`：生成覆盖率报告

## 方式三：frontend 目录开发

前端脚本以 `frontend/package.json` 为准，优先使用 Bun。

```bash
cd "frontend"
bun install
bun run dev
bun run build
bun run preview
bun run type-check
bun run lint
```

前端安装和升级统一使用 Bun，提交 `frontend/bun.lock`；不再维护 pnpm 兼容锁。文档站是独立的 npm 项目，使用 `cd docs && npm ci` 安装并提交自己的 `package-lock.json`，不要在仓库根目录生成 npm 锁文件。

## Windows 环境建议

如果没有 `make`，可分别使用 Go 和 Bun 命令：

```powershell
cd backend-go
air
go test ./...
go fmt ./...

cd ../frontend
bun install
bun run dev
```

推荐安装：
- Go
- Bun
- Make
- Git

## 文件变更与重载规则

- `make dev` 使用后端 `.air.toml`：监听 Go、模板、HTML、YAML、TOML 和 `.env`；默认保存后延迟 1 秒重新编译，并可能重启开发进程。
- `.air.toml` 的 `stop_on_error=true`，因此编译错误时不能保证旧进程继续运行。不要把热重载当作无中断部署。
- Air 排除后端 `tmp/`、嵌入前端目录和构建产物；前端开发服务器自行处理 Vue/样式更新。
- 渠道配置支持热重载；普通二进制运行时的环境变量变更需由部署工具安排重启，Air 模式下 `.env` 在监听范围内。
- 自动化协作不得自行杀进程或重启服务。验证代码时优先使用独立工作副本，避免触发现有服务的 watcher。

热重载不触发时先检查 `air` 是否在 PATH、实际启动目录及 `.air.toml` 的监听/排除设置，再阅读构建错误日志。端口冲突先识别占用者，由维护者决定处理方式；不要通过强杀进程或递归修改仓库权限排障。

## 代码检查与格式化

前端配置入口是 `frontend/eslint.config.js`，使用 Flat Config、Vue 与 TypeScript 推荐规则；`eslint-config-prettier` 关闭格式冲突。ESLint 不替代 `vue-tsc` 类型检查。

```bash
(cd frontend && bun run lint)        # 代码检查
(cd frontend && bun run lint:fix)    # 只在有意修改源码时自动修复
(cd frontend && bun run format)      # 有意执行格式化
(cd frontend && bun run type-check)  # 类型检查；生产构建也包含此步骤
```

特殊规则以配置文件为准。排障时避免对全仓库执行不必要的自动修复或格式化。

## 常用验证命令

在仓库根目录执行，子目录命令使用子 shell，避免后续命令落在错误目录：

```bash
make build
(cd backend-go && make test && go vet ./...)
(cd frontend && bun run lint && bun run test)
(cd docs && npm ci && npm run build)
```

`make build` 已运行前端类型检查和生产构建，并将产物嵌入 Go 服务。仅改某个模块时按影响范围选择检查；没有新修改或失败时不必重复相同构建。CI 同时运行 Node 脚本回归测试，校验所有共享生成产物（前端预设、后端嵌入 JSON、文档发布分片、索引及校验和）。统一运行 `node scripts/generate-preset-manifest.mjs` 生成，添加 `--check` 可只读校验。

## 更新模型能力基准数据

```bash
make benchmark-update        # 抓取各源并写入注册表
make benchmark-update-dry    # 预览变更（不写入）
```

数据源：deepswe、benchlm.ai、dradar (codexradar)、Artificial Analysis、litellm（价格/上下文）。

**Artificial Analysis 是唯一需要 API key 的源**。未设置时自动跳过，其余源照常更新：

```bash
export ARTIFICIAL_ANALYSIS_API_KEY=your-key   # 缺失则自动跳过 AA，不报错
make benchmark-update                          # 显式跳过用 --skip-artificial-analysis
```

新增模型时，在 `scripts/benchmark-sources/mapper.mjs`（deepswe/benchlm）及各源文件内的 `*_MODEL_MAP` 中补上映射，下次 `make benchmark-update` 即会自动捕获并创建其 benchmarkProfile。

## 本地访问入口

- Web 管理界面：`http://localhost:3688`
- 代理 API：`http://localhost:3688/v1`
- 健康检查：`http://localhost:3688/health`
- 前端开发服务器：默认 `http://localhost:5173`

### Windows / WSL / Docker 访问建议

Windows 下 cmd、PowerShell、WSL 与 Docker 可能处在不同网络环境中，`localhost` / `127.0.0.1` 不一定指向运行 Yunqi 的进程。跨环境调用时建议使用 Windows 主机的局域网 IPv4 地址，例如：

```text
http://192.168.1.23:3688/v1
```

获取地址：

```powershell
ipconfig
```

验证连通性：

```powershell
curl.exe -i http://192.168.1.23:3688/health
```

Yunqi 后端在 `BIND_HOST` 留空时默认监听 `:PORT`，等价于监听所有网卡地址；通常不需要额外修改为 `0.0.0.0`。如需仅本机访问，设置 `BIND_HOST=127.0.0.1`。如果局域网 IP 无法访问，优先检查 Windows 防火墙是否允许对应端口入站。

## 常见开发任务

### 只调试后端

```bash
cd "backend-go"
make dev
```

### 只调试前端

```bash
cd "frontend"
bun install
bun run dev
```

### 前后端联调

```bash
make dev
```

### 验证生产构建

```bash
make build
```

### 隔离测试中的兼容性学习状态

协议 handler 测试使用纯内存共享兼容性缓存；需要验证持久化时，应显式使用 `t.TempDir()` 下的路径。不要在源码包下创建或提交 `.config/channel_compat.json`：它是运行时状态，不是测试 fixture。

社区参考改动、分批提交和回归证据见[社区参考改进记录](community-improvements.md)。
