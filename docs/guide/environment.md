# 环境变量配置指南

## 概述

本项目使用分层的环境变量配置系统，支持开发、生产等不同环境的端口和API配置。前端通过 Vite 的环境变量系统动态连接后端服务。

## 配置文件结构

```
yunqi/
├── frontend/
│   ├── .env                    # 前端默认配置
│   ├── .env.development        # 开发环境配置
│   ├── .env.production         # 生产环境配置
│   └── vite.config.ts          # Vite 构建配置
└── backend-go/
    └── .env                    # Go 后端环境配置
```

## 环境变量详解

### 前端配置变量

#### 开发环境变量

前端使用 Vite，环境变量需以 `VITE_` 前缀：

- `VITE_PROXY_TARGET` - 后端代理目标地址（默认 `http://localhost:3000`）
- `VITE_FRONTEND_PORT` - 前端开发服务器端口（默认 `5173`）
- `VITE_BACKEND_URL` - 开发环境后端 URL（用于 API 服务）
- `VITE_API_BASE_PATH` - API 基础路径（默认 `/api`）
- `VITE_PROXY_API_PATH` - 代理 API 路径（默认 `/v1`）
- `VITE_APP_ENV` - 应用环境标识

### 后端配置 (Go)

后端支持以下环境变量：

```bash
# 服务器配置
PORT=3688                              # 服务器端口（程序内部默认 3000，建议 .env 中显式设置为 3688）
# BIND_HOST=127.0.0.1                 # 可选监听主机；不设置时等价于 :PORT，监听所有网卡
ENABLE_HTTPS=false                     # 是否启用本地 HTTPS 监听（默认 false）
TLS_AUTO_CERT=true                     # 未配置证书文件时自动生成 localhost 临时自签名证书（默认 true）
TLS_CERT_FILE=/path/to/localhost.pem   # 可选 TLS 证书文件；设置时必须同时设置 TLS_KEY_FILE
TLS_KEY_FILE=/path/to/localhost-key.pem # 可选 TLS 私钥文件；设置时必须同时设置 TLS_CERT_FILE

# 运行环境
ENV=production                         # 运行环境: development | production
# NODE_ENV=production                  # 向后兼容 (已弃用，请使用 ENV)

# 访问控制
PROXY_ACCESS_KEY=your-secret-key       # 代理访问密钥（代理 API 使用，必须设置）
EXTRA_PROXY_ACCESS_KEYS=key-a,key-b    # 可选额外代理密钥（逗号分隔，仅用于代理 API）
ADMIN_ACCESS_KEY=your-admin-key        # 可选管理密钥（管理界面和 /api/* 使用；未设置时回退到 PROXY_ACCESS_KEY）
                                      # 设置 EXTRA_PROXY_ACCESS_KEYS 后必须显式设置，且不能与代理密钥相同

# Web UI
ENABLE_WEB_UI=true                     # 是否启用 Web 管理界面

# 日志配置
LOG_LEVEL=info                         # 日志级别: debug | info | warn | error
ENABLE_REQUEST_LOGS=true               # 是否记录请求日志
ENABLE_RESPONSE_LOGS=false             # 是否记录响应日志
QUIET_POLLING_LOGS=true                # 静默前端轮询端点日志（如 /api/messages/channels/dashboard）

# 性能配置
MAX_REQUEST_BODY_SIZE_MB=50            # 请求体最大大小（MB，默认 50）

# 指标持久化配置
METRICS_PERSISTENCE_ENABLED=true       # 是否启用 SQLite 持久化（默认 true）
METRICS_RETENTION_DAYS=366             # 数据保留天数（3-366，默认 366）
                                       # 支持查询最长 1 年的历史数据

# CORS 配置
ENABLE_CORS=false                      # false 不发送允许跨域的响应头；同源访问不受影响
CORS_ORIGIN=*                          # 开启后允许的单一 Origin（如 https://console.example）或 *
                                       # development 环境额外允许 http(s)://localhost[:port]；* 不允许凭据

# 调校台启动兜底（旧部署兼容）：运行时请优先使用 Web/桌面调校台
# REQUEST_TIMEOUT=300000               # 非流式上游请求总超时（毫秒，1000-300000，默认 300000）
# RESPONSE_HEADER_TIMEOUT=120          # 等待上游 HTTP 响应头超时（秒，30-300，默认 120）
# METRICS_WINDOW_SIZE=20               # 滑动窗口大小（最小 3，默认 20）
# METRICS_FAILURE_THRESHOLD=0.7        # 失败率阈值（0-1，默认 0.7 即 70%）

# 渠道权威形态（Phase 3c，调试/运维用，一般无需设置）
# 配置携带 channelsV3 时，加载始终以它为权威重建运行时六数组
# CCX_CHANNEL_AUTHORITATIVE_STRICT=false
                                      # 严格模式：对账失败拒绝启动（默认非严格，记录诊断后以 channelsV3 覆盖）
                                      # 历史变量 CCX_CHANNEL_AUTHORITATIVE_LOAD 已随运行时权威反转移除，设置无效
```

#### 监听地址

`BIND_HOST` 控制后端监听的主机地址。留空时保持默认 `:PORT` 行为，等价于监听所有网卡（常见显示为 `0.0.0.0:3688` 或 `*:3688`）；设置 `BIND_HOST=127.0.0.1` 时只允许本机访问。Docker 部署如需只暴露到宿主机本地，通常应保持容器内 `BIND_HOST` 为空，并使用 `127.0.0.1:3688:3688` 端口映射。

#### 本地 HTTPS

`ENABLE_HTTPS=true` 会让 Yunqi 在当前 `PORT` 启用 HTTPS，用于 Claude Desktop 等只接受 HTTPS Gateway Base URL 的客户端。开启后推荐本地访问地址为 `https://localhost:3688`，同时同端口仍接受 `http://localhost:3688`，以兼容已有客户端配置。

- 默认 `TLS_AUTO_CERT=true`：未配置证书文件时，Yunqi 会在进程内生成仅包含 `localhost`、`127.0.0.1`、`::1` 的临时自签名证书，适合本地转发和桌面端自检。
- 如客户端严格要求系统信任证书，请使用 `mkcert`、企业证书或手动签发证书，并同时设置 `TLS_CERT_FILE` 与 `TLS_KEY_FILE`。
- `TLS_CERT_FILE` 和 `TLS_KEY_FILE` 必须成对设置；只设置其中一个会导致启动失败。
- `TLS_CERT_FILE` 和 `TLS_KEY_FILE` 请使用展开后的绝对路径。相对路径会按 Yunqi 进程工作目录解析；安装包、systemd、launchd、NSSM 或从不同目录启动时，`./backend-go/...` 这类路径很容易找不到文件。

Claude Desktop 访问本地 HTTPS 时，如果 Yunqi 日志出现 `remote error: tls: unknown certificate`，通常表示客户端不信任临时自签名证书。推荐用 `mkcert` 生成系统信任的本地证书。

安装 `mkcert`：

```bash
# macOS
brew install mkcert

# Linux（Debian/Ubuntu）
sudo apt install libnss3-tools
# 然后通过系统包管理器、Homebrew/Linuxbrew 或项目 Release 安装 mkcert

# Windows（任选其一，在管理员 PowerShell 中执行）
choco install mkcert
scoop bucket add extras
scoop install mkcert
```

初始化本机 CA：

```bash
mkcert -install
```

生成证书时，按部署方式选择证书目录，并通过部署环境配置证书路径。

| 场景 | 建议证书目录 |
| --- | --- |
| 源码开发 | `backend-go/.config/certs` |
| Linux systemd 服务 | `/etc/yunqi/certs` |
| macOS launchd 手动服务 | `~/yunqi/certs` |
| Windows NSSM 服务 | `C:\yunqi\certs` |

源码开发或 macOS/Linux 服务部署可用以下命令，把 `CERT_DIR` 替换为上表中的实际目录。示例会先展开为绝对路径，复制到 `.env` 时也应使用输出后的绝对路径：

```bash
CERT_DIR="$(pwd)/backend-go/.config/certs"
mkdir -p "$CERT_DIR"

mkcert \
  -cert-file "$CERT_DIR/localhost.pem" \
  -key-file "$CERT_DIR/localhost-key.pem" \
  "localhost" "127.0.0.1" "::1"
```

Windows PowerShell 示例：证书保存在服务部署目录下，路径应与实际环境配置一致。

```powershell
$certDir = "C:\yunqi\certs"
New-Item -ItemType Directory -Force $certDir

mkcert `
  -cert-file "$certDir\localhost.pem" `
  -key-file "$certDir\localhost-key.pem" `
  localhost 127.0.0.1 ::1
```

然后在实际生效的 `.env` 中启用 HTTPS 并写入证书路径：

```env
ENABLE_HTTPS=true
TLS_AUTO_CERT=false
TLS_CERT_FILE=/absolute/path/to/localhost.pem
TLS_KEY_FILE=/absolute/path/to/localhost-key.pem
```

`.env` 的位置取决于启动方式：

- 源码开发：通常是 `backend-go/.env`。
- Linux systemd：通常是 `EnvironmentFile` 指向的 `/opt/yunqi/.env`，也可能由服务文件改成其他路径。
- macOS launchd / Windows NSSM：若通过服务环境变量传入配置，应在对应服务配置中设置 `ENABLE_HTTPS`、`TLS_AUTO_CERT`、`TLS_CERT_FILE`、`TLS_KEY_FILE`。

安装包和系统服务场景建议在 `.env` 中使用展开后的绝对路径；不要依赖相对路径或 `%APPDATA%` 这类变量自动展开。重启 Yunqi 后，在 Claude Desktop 中使用 `https://localhost:3688` 作为 Gateway Base URL。

如果 Claude Desktop 通过局域网 IP 或自定义主机名访问 Yunqi，例如 `https://192.168.1.20:3688`，生成证书时必须把该 IP 或主机名一并加入：

```bash
mkcert \
  -cert-file "$CERT_DIR/yunqi-local.pem" \
  -key-file "$CERT_DIR/yunqi-local-key.pem" \
  "localhost" "127.0.0.1" "::1" "192.168.1.20"
```

证书私钥不要提交到仓库；示例中的 `backend-go/.config/` 默认已被忽略。

`EXTRA_PROXY_ACCESS_KEYS` 用于给多个客户端分配额外代理访问密钥，不提供用户管理、用量统计、模型权限或限速能力。只要配置了该变量，管理接口就不再回退到 `PROXY_ACCESS_KEY`：必须显式设置独立的 `ADMIN_ACCESS_KEY`，并且它不能等于 `PROXY_ACCESS_KEY` 或任何额外代理密钥。修改这些访问控制环境变量后需要重启服务。

调校台保存的运行时配置会写入 `config.json` 的 `circuitBreaker`，并在保存后立即作为全局默认值生效。对应环境变量只作为启动兜底/旧部署兼容项；一旦调校台保存了同名字段，运行时以 `config.json` 为准：

| 字段 | 默认值 | 范围 | 说明 |
| --- | --- | --- | --- |
| `requestTimeoutMs` | `REQUEST_TIMEOUT` | `1000-300000` | 非流式上游请求总超时。仅作用于非流式请求；流式请求仍由响应头等待和流式健康检测控制。 |
| `responseHeaderTimeoutMs` | `RESPONSE_HEADER_TIMEOUT * 1000` | `1000-300000` | 连接建立后等待上游 HTTP 响应头的时间。普通渠道建议保持较短，慢启动/本地推理渠道优先使用渠道级覆盖。 |

渠道配置（`config.json` 的各 `*Upstream[]` 项）支持覆盖这两个全局默认值：

| 字段 | 默认值 | 范围 | 说明 |
| --- | --- | --- | --- |
| `requestTimeoutMs` | `0` | `0` 或 `1000-300000` | 非流式上游请求总超时；`0` 或留空继承调校台/环境变量的全局值。 |
| `responseHeaderTimeoutMs` | `0` | `0` 或 `1000-300000` | 连接建立后等待上游 HTTP 响应头的时间；`0` 或留空继承调校台/环境变量的全局值。 |

### 熔断阈值与关闭保护的区别

`METRICS_FAILURE_THRESHOLD=1` 表示失败率阈值为 100%，不是关闭熔断。调校台还可调整 `windowSize`（3–100）、`failureThreshold`（0.01–1）和 `consecutiveFailuresThreshold`（1–100，默认 5）；连续失败触发条件独立于失败率阈值。已保存的这些字段同样优先于环境变量。

`CIRCUIT_BREAKER_COOLDOWN` 不是当前支持的环境变量，设置它不会改变退避时间。Key 级运行态熔断默认从 30 秒开始退避，失败探针会逐步延长，最多 10 分钟。上游过载、模型级熔断、429 冷却，以及鉴权或余额原因导致的 Key 禁用各有独立规则；仅调高失败率阈值不会关闭这些限制。

单渠道遇到临时容量不足时，先查看渠道日志中的失败分类、模型和下次重试时间，再按实际情况选择调校台的“温和”预设或调整连续失败阈值。当前没有永久关闭全部熔断和禁用规则的总开关。`REQUEST_TIMEOUT` 只控制非流式上游请求总超时，也不是熔断开关。

运行时配置按渠道类型分组维护：

| 字段 | 说明 |
| --- | --- |
| `messagesUpstream` | Claude Messages 语义渠道 |
| `responsesUpstream` | Codex/OpenAI Responses 渠道 |
| `chatUpstream` | OpenAI Chat Completions 渠道 |
| `geminiUpstream` | Gemini 原生协议渠道 |
| `imagesUpstream` | OpenAI Images 渠道 |
| `vectorsUpstream` | OpenAI 兼容 Embeddings 渠道，`serviceType` 固定为 `openai` |

Vectors 渠道不内置默认模型候选。模型名以你的上游 Embeddings 服务为准：可以在配置界面填写 Base URL 和 API Key 后从 `/models` 拉取，也可以手动输入，并可通过 Vectors 渠道的模型映射把客户端请求中的 `model` 改写到实际上游模型。

Embedding 向量空间需要额外谨慎：不同模型、不同输出维度或不同归一化语义的向量默认不应混用，向量库 collection / index 也建议按模型、维度和空间隔离。旧配置没有 `embeddingCapabilities` 时，Vectors 会保持原有 fallback 行为；只要当前候选中存在 Embedding 兼容元数据，就会启用严格过滤。此时 `supportedModels` 仍匹配客户端请求的原始模型名，`embeddingCapabilities` 的 key 则匹配 `modelMapping` 后的实际上游模型名，并支持与 `modelCapabilities` 类似的精确和通配符匹配。只有 `embeddingSpaceId`（未配置时使用实际模型名）、有效维度和 `normalized` 语义一致的候选才会参与 fallback。

示例：

```json
{
  "vectorsUpstream": [
    {
      "name": "openai-small-a",
      "supportedModels": ["text-embedding"],
      "modelMapping": {
        "text-embedding": "text-embedding-3-small"
      },
      "embeddingCapabilities": {
        "text-embedding-3-small": {
          "embeddingSpaceId": "openai-text-embedding-3-small",
          "dimensions": 1536,
          "supportedDimensions": [512, 1536],
          "normalized": true
        }
      }
    }
  ]
}
```

Vectors 仍不支持 capability-test；不要通过能力测试推断 Embedding 空间兼容性。

运行时配置文件中的 `circuitBreaker` 还支持流式健康检测字段：

| 字段 | 默认值 | 范围 | 说明 |
| --- | --- | --- | --- |
| `streamFirstContentTimeoutMs` | `90000` | `5000-300000` | HTTP 200 后等待首个有效内容的时间。 |
| `streamInactivityTimeoutMs` | `90000` | `1000-180000` | 首字后等待后续有效输出的空闲时间。 |
| `streamToolCallIdleTimeoutMs` | `300000` | `30000-300000` | 工具调用 pending 阶段连续无上游 SSE 帧的 idle timeout；收到参数片段、状态事件或心跳帧都会重置计时器。 |

`streamToolCallIdleTimeoutMs` 是破坏性字段名，旧 `streamToolCallTimeoutMs` 不再使用。该字段不是工具调用总耗时上限。

#### 渠道级主动限速

每个上游渠道可配置主动限速字段，在请求发往上游前主动限流，规避免费/低额度上游（如 MiMo）的 RPM 限制导致的 429。这些字段位于渠道配置（`config.json` 的各 `*Upstream[]` 项）中，可通过 Web / 桌面端的渠道编辑表单设置：

| 字段 | 默认值 | 说明 |
| --- | --- | --- |
| `rateLimitRpm` | `0` | 每分钟请求数上限（令牌桶填充速率 = RPM/60）。`0` 或留空表示不限速。 |
| `rateLimitBurst` | `0` | 令牌桶突发容量，允许的瞬时突发请求数。`0` 时自动取 `rateLimitRpm` 的值。 |
| `rateLimitMaxConcurrent` | `0` | 同时进行的上游请求数上限（信号量）。`0` 表示不限并发。 |
| `rateLimitAutoFromHeaders` | `false` | 启用后解析上游 `Retry-After` / `anthropic-ratelimit-*-reset` / `x-ratelimit-reset*` 响应头：429/5xx 时按三族重置头取最晚有效值冷却（多维度并存取最晚，兼容 RFC3339 / duration / epoch 秒与毫秒 / 相对秒格式），所有冷却指示统一截断到 1 小时，进一步规避 429。 |

限速作用域是**渠道级**（同渠道下所有 API Key 共享同一令牌桶），符合「单账号跨 Key 共享额度」的常见上游计费模型。请求被限速拦截（cooldown / 超出 maxWait 排队上限）时会自动 failover 到其它可用渠道；调度器在选择渠道时会跳过处于 cooldown 的渠道。桌面端一键添加 MiMo 渠道时会内置保守默认 `rateLimitRpm`（官方 RPM 上限的约 80%）。

#### 调度软增强（`scheduler`）

`config.json` 顶层的 `scheduler` 段控制调度软增强，均可省略（省略即默认启用）并支持热重载：

| 字段 | 默认值 | 说明 |
| --- | --- | --- |
| `promptAffinityFallback` | `true` | 匿名请求的内容指纹亲和回退。请求未携带任何显式会话标识（请求头、`user`/`user_id`/`prompt_cache_key`、`metadata.user_id` 等）时，用 system 提示与首条 user 消息的规范指纹（`pp:` 前缀合成 ID）作为会话标识，使 Trace 亲和、会话跟踪对裸 SDK 客户端同样生效。同一会话各轮指纹一致；超过 4KB 的部分截断。 |
| `keyAutoWeight` | `true` | per-key 自动权重。以 5 分钟滑动窗口统计每把 Key 的整把级成功/失败（模型级失败只进熔断，不连累同 Key 其他模型），按「Laplace 平滑成功率 × 连续失败减半」计算 0.05-1.0 的软降权系数叠加到手控 weight 排序上；窗口内样本不足 10 次时不干预。硬隔离仍由熔断与持久限制负责。 |
| `streamRepetitionLoopDetection` | `true` | 流式输出无限重复检测。模型卡在同一短语/段落上无限复述（累计约 3KB 完全相同的字节级重复）时立即中止当前流（客户端收到错误/不完整收尾事件，停止烧 token），并按「一次确证即隔离」强制熔断该渠道×Key×模型组合 60 秒；隔离期满后复犯按 60s→2m→…→30min 递增退避，期间任意一次干净成功即自动恢复。误杀自愈成本低，如需一键回退可置 `false`。 |

#### 命令行运行时路径

命令行版支持用参数覆盖运行时路径，不传参数时仍保持默认行为：

```bash
yunqi --config ~/.config/yunqi/config.json --statedir ~/.local/state/yunqi --logdir ~/.local/state/yunqi/logs
```

- `--config PATH`：指定配置文件路径。
- `--statedir DIR`：指定运行时状态目录；`metrics.db`、`conversation_state.json`、`scheduled_recovery_state.json` 会写入该目录，未指定时保持默认 `.config`。
- `--logdir DIR`：指定日志目录；优先级高于 `LOG_DIR` 环境变量。使用 `none` 或 `null` 可禁用日志文件写入（仅输出到控制台），适合 systemd/journald 等环境。
- `--help`：查看完整命令行参数说明。
- 路径中的 `~` / `~/...` 会按当前用户主目录展开。

优先级：`--logdir` > `LOG_DIR` > 默认 `logs`。`--config` 不会隐式改变日志目录或状态目录。

#### 日志等级说明

项目采用标准的四级日志系统，等级从高到低：

| 等级 | 值 | 说明 | 典型场景 |
|------|----|----|---------|
| `error` | 0 | 错误日志（最高优先级） | 致命错误、异常情况 |
| `warn` | 1 | 警告日志 | 非致命问题、降级操作 |
| `info` | 2 | 信息日志（默认） | 常规操作、状态变化 |
| `debug` | 3 | 调试日志（最低优先级） | 详细调试信息、敏感数据 |

**正文约束**：`ENV=development`、debug 等级或 `ENABLE_*_LOGS=true` 都不会单独启用正文。请先通过 `GET /api/logging/body` 核对 `effective`；正文策略 API 与变更审计见 [API](api.md)。

**等级控制规则**：设置 `LOG_LEVEL=info` 时，会输出 `error`、`warn`、`info` 级别的日志，但不输出 `debug` 级别。

#### 日志控制机制

项目使用三种机制来控制日志输出：

##### 1. 显式等级控制（推荐）
```go
// 代码示例
if envCfg.ShouldLog("info") {
    log.Printf("🎯 使用上游: %s", upstream.Name)
}
```
- **适用场景**：通用信息输出
- **控制变量**：`LOG_LEVEL`

##### 2. 开关控制（分类日志）
```go
// 代码示例
if envCfg.EnableRequestLogs {
    log.Printf("📥 收到请求: %s", c.Request.URL.Path)
}
```
- **适用场景**：请求/响应类日志
- **控制变量**：`ENABLE_REQUEST_LOGS`、`ENABLE_RESPONSE_LOGS`

##### 3. 环境门控（开发专用）
```go
// 代码示例
if envCfg.ShouldLogRequestBodies() {
    log.Printf("📄 原始请求体:\n%s", formattedBody)
}
```
- **适用场景**：敏感/详细信息（请求体、请求头等）
- **控制条件**：`ENV=development` 与对应日志开关；正文还须已审计的 requestBodies/responseBodies 策略。

#### 日志输出对照表

| 日志内容 | 控制条件 | 等效等级 | 生产环境 | 开发环境 |
|---------|---------|---------|---------|---------|
| `📄 原始请求体` | `ShouldLogRequestBodies()`（含正文审计策略） | debug | ❌ 不输出 | 显式启用后输出 |
| `📋 实际请求头` | `EnableRequestLogs && IsDevelopment()` | debug | ❌ 不输出 | ✅ 输出 |
| `📦 响应体` | `ShouldLogResponseBodies()`（含正文审计策略） | debug | ❌ 不输出 | 显式启用后输出 |
| `📥 收到请求` | `EnableRequestLogs` | info | ⚙️ 可配置 | ✅ 输出 |
| `⏱️ 响应完成` | `EnableResponseLogs` | info | ⚙️ 可配置 | ✅ 输出 |
| `🎯 使用上游` | `ShouldLog("info")` | info | ⚙️ 可配置 | ✅ 输出 |
| `ℹ️ 客户端中断` | `ShouldLog("info")` | info | ⚙️ 可配置 | ✅ 输出 |
| `⚠️ API密钥失败` | 无条件 | warn | ✅ 输出 | ✅ 输出 |
| `💥 所有密钥失败` | 无条件 | error | ✅ 输出 | ✅ 输出 |

#### 配置组合效果

**开发环境 + 完整日志**：
```env
ENV=development
LOG_LEVEL=debug
ENABLE_REQUEST_LOGS=true
ENABLE_RESPONSE_LOGS=true
```
- ✅ 开启详细元数据日志；正文仍须通过审计策略独立启用，并受脱敏和字节上限约束
- ✅ 适合本地开发调试
- ⚠️ 可能包含敏感信息，不要在生产环境使用

**生产环境 + 最小日志**：
```env
ENV=production
LOG_LEVEL=warn
ENABLE_REQUEST_LOGS=false
ENABLE_RESPONSE_LOGS=false
```
- ✅ 只输出警告和错误
- ✅ 最小性能影响
- ✅ 不输出敏感信息
- ⚠️ 排查问题时信息较少

**生产环境 + 适度日志**（推荐）：
```env
ENV=production
LOG_LEVEL=info
ENABLE_REQUEST_LOGS=true
ENABLE_RESPONSE_LOGS=false
```
- ✅ 输出基本请求信息（如 `📥 收到请求`）
- ✅ 不输出详细内容（请求体、响应体等）
- ✅ 平衡了可观测性和性能
- ✅ 不泄露敏感信息

**调试模式**：
```env
ENV=development
LOG_LEVEL=debug
ENABLE_REQUEST_LOGS=true
ENABLE_RESPONSE_LOGS=true
```
- ✅ 最详细的日志输出
- ✅ 查看完整的请求/响应数据流
- ⚠️ 仅用于故障排查，排查完成后应恢复正常配置

#### 性能影响说明

| 配置 | CPU 影响 | 内存影响 | 磁盘 I/O |
|-----|---------|---------|----------|
| `LOG_LEVEL=error` | 极低 | 极低 | 极低 |
| `LOG_LEVEL=warn` | 极低 | 极低 | 低 |
| `LOG_LEVEL=info` | 低 | 低 | 中 |
| `LOG_LEVEL=debug` | 中 | 中 | 高 |
| `ENABLE_REQUEST_LOGS=true` | 低 | 低 | 中 |
| `ENABLE_RESPONSE_LOGS=true` | 低-中 | 中-高 | 高 |

**生产环境建议**：
- 日常运行：`LOG_LEVEL=info`，`ENABLE_RESPONSE_LOGS=false`
- 故障排查：临时开启 `ENABLE_RESPONSE_LOGS=true`
- 高负载场景：使用 `LOG_LEVEL=warn` 减少开销

### ENV 变量影响

| 配置项 | `development` | `production` |
|--------|---------------|--------------|
| `/admin/dev/info` | ✅ 开启 | ❌ 关闭 |
| CORS | 宽松（localhost自动允许）| 严格 |
| 日志 | 详细 | 最小 |

## 配置文件内容

### frontend/.env
```env
# 前端环境配置

# 后端API服务器配置
VITE_BACKEND_URL=http://localhost:3000

# 前端开发服务器配置
VITE_FRONTEND_PORT=5173

# API路径配置
VITE_API_BASE_PATH=/api
VITE_PROXY_API_PATH=/v1
```

### frontend/.env.development
```env
# 开发环境配置

# 后端API服务器配置
VITE_BACKEND_URL=http://localhost:3000

# 前端开发服务器配置
VITE_FRONTEND_PORT=5173

# API路径配置
VITE_API_BASE_PATH=/api
VITE_PROXY_API_PATH=/v1

# 开发模式标识
VITE_APP_ENV=development
```

### frontend/.env.production
```env
# 生产环境配置
VITE_API_BASE_PATH=/api
VITE_PROXY_API_PATH=/v1
VITE_APP_ENV=production
```

### backend-go/.env.example
```env
# 服务器配置
PORT=3688
# BIND_HOST=127.0.0.1

# 运行环境
ENV=production

# 访问控制 (必须修改!)
PROXY_ACCESS_KEY=your-proxy-access-key
# EXTRA_PROXY_ACCESS_KEYS=extra-proxy-key-1,extra-proxy-key-2
# ADMIN_ACCESS_KEY=your-admin-access-key-here

# Web UI
ENABLE_WEB_UI=true

# 日志配置
LOG_LEVEL=info
ENABLE_REQUEST_LOGS=false
ENABLE_RESPONSE_LOGS=false
```

## API 基础URL 生成逻辑

前端通过以下逻辑动态确定API基础URL：

```typescript
const getApiBase = () => {
  // 生产环境：直接使用当前域名
  if (import.meta.env.PROD) {
    return '/api'
  }

  // 开发环境：使用配置的后端URL
  const backendUrl = import.meta.env.VITE_BACKEND_URL
  const apiBasePath = import.meta.env.VITE_API_BASE_PATH || '/api'

  if (backendUrl) {
    return `${backendUrl}${apiBasePath}`
  }

  // 回退到默认配置
  return '/api'
}
```

## 开发服务器代理配置

Vite 开发服务器自动配置代理，将前端请求转发到后端：

```typescript
// vite.config.ts
server: {
  port: Number(env.VITE_FRONTEND_PORT) || 5173,
  proxy: {
    '/api': {
      target: backendUrl,
      changeOrigin: true,
      secure: false
    }
  }
}
```

## 环境切换

### 开发环境启动
```bash
# 方式 1: 根目录启动（推荐）
make dev

# 方式 2: 分别启动
# 启动后端 (端口 3688)
cd backend-go && make dev

# 启动前端 (端口 5173)
cd frontend && bun run dev
```

### 生产环境构建
```bash
# 完整构建
make build

# Docker 部署
docker-compose up -d
```

## 端口配置优先级

1. **环境变量** - 从 `.env.*` 文件读取
2. **默认值** - 代码中定义的回退值
3. **系统环境变量** - `PORT` （后端）

## 常见配置场景

### 场景1：更改后端端口到 8080
```env
# backend-go/.env
PORT=8080

# frontend/.env.development
VITE_BACKEND_URL=http://localhost:8080
```

### 场景2：使用远程后端服务
```env
# frontend/.env.development
VITE_BACKEND_URL=https://api.example.com
```

### 场景3：自定义前端开发端口
```env
# frontend/.env.development
VITE_FRONTEND_PORT=3000
```

### 场景4：生产环境配置

#### 4.1 高性能模式（最小日志）
```env
# backend-go/.env
ENV=production
PORT=3688
PROXY_ACCESS_KEY=$(openssl rand -base64 32)
# 可选：管理界面与 /api/* 使用独立管理密钥
ADMIN_ACCESS_KEY=$(openssl rand -base64 32)

# 最小日志输出
LOG_LEVEL=warn
ENABLE_REQUEST_LOGS=false
ENABLE_RESPONSE_LOGS=false

ENABLE_WEB_UI=true
```
- ✅ 适合：高并发场景、性能敏感应用
- ✅ 特点：最低资源消耗，只记录警告和错误
- ⚠️ 注意：排查问题时信息较少

#### 4.2 标准模式（推荐）
```env
# backend-go/.env
ENV=production
PORT=3688
PROXY_ACCESS_KEY=$(openssl rand -base64 32)
ADMIN_ACCESS_KEY=$(openssl rand -base64 32)

# 适度日志输出
LOG_LEVEL=info
ENABLE_REQUEST_LOGS=true
ENABLE_RESPONSE_LOGS=false

ENABLE_WEB_UI=true
```
- ✅ 适合：大多数生产环境
- ✅ 特点：平衡可观测性和性能，不泄露敏感信息
- ✅ 优势：足够的信息用于监控和问题排查

#### 4.3 调试模式（临时排查）
```env
# backend-go/.env
ENV=production
PORT=3688
PROXY_ACCESS_KEY=$(openssl rand -base64 32)
ADMIN_ACCESS_KEY=$(openssl rand -base64 32)

# 详细日志输出（临时使用）
LOG_LEVEL=info
ENABLE_REQUEST_LOGS=true
ENABLE_RESPONSE_LOGS=true

ENABLE_WEB_UI=true
```
- ⚠️ 适合：故障排查时临时启用
- ⚠️ 注意：显式启用正文策略后输出受限、脱敏的响应内容，增加日志量
- 🔄 建议：问题解决后立即恢复标准配置

#### 4.4 开发环境配置
```env
# backend-go/.env
ENV=development
PORT=3688
PROXY_ACCESS_KEY=dev-test-key

# 完整日志输出
LOG_LEVEL=debug
ENABLE_REQUEST_LOGS=true
ENABLE_RESPONSE_LOGS=true

ENABLE_WEB_UI=true
```
- ✅ 适合：本地开发和调试
- ✅ 特点：输出所有详细信息，包括请求体、响应体
- ⚠️ 警告：包含敏感信息，仅限开发环境使用

## 调试配置

开发环境下，前端会在控制台输出当前API配置：

```javascript
console.log('🔗 API Configuration:', {
  API_BASE: '/api',
  BACKEND_URL: 'http://localhost:3000',
  IS_DEV: true,
  IS_PROD: false
})
```

## 注意事项

1. **变量前缀**：前端环境变量必须以 `VITE_` 开头才能在浏览器中访问
2. **构建时解析**：Vite 在构建时静态替换环境变量，运行时无法修改
3. **生产环境**：生产环境不需要指定后端URL，通过反向代理或一体化部署处理
4. **类型安全**：使用 `Number()` 转换端口号确保类型正确
5. **密钥安全**：切勿在版本控制中提交 `.env` 文件，使用 `.env.example` 作为模板

## 安全最佳实践

### 生成强密钥
```bash
# 生成随机密钥
PROXY_ACCESS_KEY=$(openssl rand -base64 32)
ADMIN_ACCESS_KEY=$(openssl rand -base64 32)
echo "代理密钥: $PROXY_ACCESS_KEY"
echo "管理密钥: $ADMIN_ACCESS_KEY"
```

### 生产环境配置清单
```bash
# 1. 强密钥 (必须!)
PROXY_ACCESS_KEY=<strong-random-proxy-key>
ADMIN_ACCESS_KEY=<strong-random-admin-key>  # 可选，建议与代理密钥分离

# 2. 生产模式
ENV=production

# 3. 适度日志（推荐）
LOG_LEVEL=info
ENABLE_REQUEST_LOGS=true
ENABLE_RESPONSE_LOGS=false

# 4. 启用 Web UI (可选)
ENABLE_WEB_UI=true
```

### 日志安全建议

#### 敏感信息保护
项目已自动对以下信息进行脱敏处理：
- ✅ API密钥：只显示前4位和后4位（如 `sk-a***b`）
- ✅ Authorization 请求头：完全隐藏
- ✅ x-api-key 请求头：完全隐藏

#### 推荐配置
```bash
# 生产环境：不输出详细内容
ENV=production
ENABLE_REQUEST_LOGS=true    # ✅ 基本请求信息
ENABLE_RESPONSE_LOGS=false  # ❌ 不输出响应体

# 开发环境：可以输出详细内容
ENV=development
ENABLE_REQUEST_LOGS=true
ENABLE_RESPONSE_LOGS=true
```

#### 日志存储注意事项
1. **日志轮转**：定期清理旧日志，避免磁盘空间耗尽
2. **访问控制**：限制日志文件的访问权限
   ```bash
   chmod 600 /var/log/yunqi/*.log
   ```
3. **敏感数据**：即使有脱敏，也应定期审查日志内容
4. **合规要求**：根据数据保护法规（GDPR、CCPA等）管理日志

#### 故障排查时的安全做法
```bash
# ✅ 推荐：临时开启详细日志，排查完成后恢复
ENABLE_RESPONSE_LOGS=true  # 仍须通过正文策略 API 显式启用 responseBodies

# 🔄 排查完成后立即恢复
ENABLE_RESPONSE_LOGS=false

# ❌ 不推荐：在生产环境长期开启 debug 级别
LOG_LEVEL=debug  # 可能泄露敏感信息
```

## 日志与自动轮转 FAQ

### Q1: 系统是否有自带的日志轮转机制？

是的，Yunqi 后端自带了开箱即用的日志轮转和自动归档功能。
如果未在环境变量中显式配置，系统将采用以下默认规划（定义在 `internal/logger/logger.go:43` 的 `DefaultConfig` 中）：

- **日志目录 (`LogDir`)**: `logs` （项目根目录下的 `logs/` 文件夹）
- **日志文件名 (`LogFile`)**: `app.log`
- **单文件最大大小 (`MaxSize`)**: `100 MB`（日志单文件达到 100MB 时触发轮换）
- **最大保留备份数 (`MaxBackups`)**: `10` 个（最多保留 10 个历史旧日志文件）
- **最大保留天数 (`MaxAge`)**: `30` 天（仅保留最近 30 天的日志文件）
- **是否压缩 (`Compress`)**: `true`（历史日志在轮转时会自动进行 `gzip` 压缩以极大程度地节省磁盘空间）

### Q2: 既然内置了自动轮转，我还需要配置系统的 `logrotate` 或手动清理吗？

不需要。只要启动了服务，内置的日志框架就会对 `logs/app.log` 状态进行自维护。只有在有特定的多进程共享、操作系统集中审计、或需要将日志发送到第三方分析系统时，才需要考虑外部 `logrotate` 等外部工具介入。通常情况下，内置的 100MB 轮换和自动 gzip 压缩已经能彻底避免磁盘耗尽的隐患。

### Q3: 如何在 `.env` 中定制这些日志和轮转相关的参数？

你可以在后端 `.env` 配置文件中声明以下自定义变量来覆盖默认的配置：

```env
# 基础日志开关与级别
LOG_LEVEL=info                         # 日志级别: debug | info | warn | error
ENABLE_REQUEST_LOGS=true               # 是否记录请求日志
ENABLE_RESPONSE_LOGS=false             # 是否记录响应日志
QUIET_POLLING_LOGS=true                # 静默轮询日志

# 轮转与存储定制
LOG_DIR=logs                           # 自定义日志存储目录 (默认 logs，可被 --logdir 覆盖)
# LOG_DIR=none                           # 禁用日志文件写入，仅输出到控制台 (none/null 均可，不区分大小写)
LOG_FILE=app.log                       # 自定义日志文件名 (默认 app.log)
LOG_MAX_SIZE=100                       # 单个日志文件最大大小 (MB) (默认 100)
LOG_MAX_BACKUPS=10                     # 保留的旧日志文件最大数量 (默认 10)
LOG_MAX_AGE=30                         # 保留的旧日志文件最大天数 (默认 30)
LOG_COMPRESS=true                      # 是否压缩旧日志文件 (默认 true)
LOG_TO_CONSOLE=true                    # 是否同时输出到控制台 (默认 true)
```

## 故障排除

### 问题：前端无法连接后端
1. 检查后端是否在正确端口启动
   ```bash
   curl http://localhost:3000/health
   ```
2. 确认 `VITE_BACKEND_URL` 配置正确
3. 查看浏览器控制台的API配置输出

### 问题：构建后API请求失败
1. 确认生产环境配置了正确的反向代理或使用一体化部署
2. 检查 `VITE_API_BASE_PATH` 设置
3. 验证后端API路径匹配

### 问题：环境变量不生效
1. 确认变量名以 `VITE_` 开头 (前端) 或在后端代码中正确读取
2. 重启开发服务器
3. 检查 `.env` 文件语法正确 (无多余空格、引号等)

### 问题：认证失败
```bash
# 检查代理密钥设置
echo $PROXY_ACCESS_KEY
echo $ADMIN_ACCESS_KEY

# 测试代理 API 认证（示例）
curl -H "x-api-key: $PROXY_ACCESS_KEY" http://localhost:3000/v1/models

# 测试管理 API 认证（若配置了 ADMIN_ACCESS_KEY）
curl -H "x-api-key: ${ADMIN_ACCESS_KEY:-$PROXY_ACCESS_KEY}" http://localhost:3000/api/messages/channels
```

### 问题：日志输出过多或过少

#### 日志过多（影响性能）
**症状**：日志文件快速增长，磁盘空间不足，或系统性能下降

**解决方案**：
1. 降低日志等级
   ```env
   LOG_LEVEL=warn  # 从 info 或 debug 降级
   ```

2. 关闭详细日志
   ```env
   ENABLE_REQUEST_LOGS=false
   ENABLE_RESPONSE_LOGS=false
   ```

3. 使用日志轮转（推荐）
   ```bash
   # 使用 systemd 日志轮转
   journalctl --vacuum-time=7d

   # 或使用 logrotate
   # /etc/logrotate.d/yunqi
   /var/log/yunqi/*.log {
       daily
       rotate 7
       compress
       delaycompress
       missingok
       notifempty
   }
   ```

#### 日志过少（排查困难）
**症状**：出现问题时没有足够的日志信息

**解决方案**：
1. 提高日志等级
   ```env
   LOG_LEVEL=info  # 从 warn 提升
   ```

2. 临时开启详细日志
   ```env
   ENABLE_REQUEST_LOGS=true
   ENABLE_RESPONSE_LOGS=true
   ```

3. 使用开发模式（仅限测试环境）
   ```env
   ENV=development
   LOG_LEVEL=debug
   ```

#### 看不到请求体/响应体
**症状**：日志中没有详细的请求/响应内容

**原因**：正文默认关闭；需要 development、对应日志开关，以及全局管理员通过 `PUT /api/logging/body` 显式启用的已审计正文策略。

**解决方案**：
```env
# 方案1：临时切换到开发模式（不推荐生产环境）
ENV=development
ENABLE_REQUEST_LOGS=true
ENABLE_RESPONSE_LOGS=true

# 方案2：查看是否开启了日志开关
ENABLE_REQUEST_LOGS=true   # 必须为 true
ENABLE_RESPONSE_LOGS=true  # 必须为 true

# 方案3：检查当前环境
echo $ENV  # 必须是 development
```

**安全提醒**：
- ⚠️ 请求体和响应体可能包含敏感信息（API密钥、用户数据等）
- ⚠️ 生产环境建议关闭 `ENABLE_RESPONSE_LOGS`
- ⚠️ 排查完成后立即恢复安全配置

### 问题：日志格式混乱
**症状**：日志输出格式不统一或难以阅读

**检查项**：
1. 确认是否混用了多个日志系统
2. 检查是否有第三方库输出了额外日志
3. 验证环境变量是否正确加载
   ```bash
   # 打印当前日志配置
   curl -H "x-api-key: $PROXY_ACCESS_KEY" http://localhost:3000/health
   ```

## 文档资源

- **项目架构**: 参见 [架构说明](./architecture.md)
- **快速开始**: 参见 [快速开始](./getting-started.md)
- **贡献指南**: 参见 [贡献指南](./contributing.md)

## 可选治理与可观测性策略

这些变量在服务启动时读取，修改后由部署方安排生效；配置 JSON 的在线保存不等同于重新读取环境变量。

| 变量 | 默认值与含义 |
| --- | --- |
| `PROXY_MAX_CONCURRENCY` | 0，不限；按认证入口主体限制完整 HTTP/SSE/WS 轮次的并发 |
| `PROXY_PRINCIPAL_IDS` | 可选旧入口 Key→稳定主体映射，用于旋转后的持续身份；托管入口 Key 自带稳定 PrincipalID |
| `IDENTITY_PROXY_SECRET` | 未配置；可信身份代理共享密钥，至少 32 字节，还须配置受信 CIDR/issuer/组映射/allowedOrigins |
| `YUNQI_STRICT_SESSION_AFFINITY` | false；显式稳定会话按入口主体、协议及 main/subagent 原子绑定渠道 UID |
| `YUNQI_OUTBOUND_ALLOWED_HOSTS` | 空，保留已有主机兼容性；可设逗号分隔主机/通配主机白名单 |
| `YUNQI_OUTBOUND_ALLOWED_PORTS` | 空，不额外收窄；可设逗号分隔端口白名单 |
| `YUNQI_OUTBOUND_BLOCK_PRIVATE` | false；启用时阻止私网/回环等目标 |
| `YUNQI_OUTBOUND_PRIVATE_CIDRS` | 空；阻止私网时允许的明确 CIDR 例外 |
| `YUNQI_OTLP_ENABLED` | false；启用 OTLP/HTTP protobuf trace 导出 |
| `YUNQI_OTLP_ENDPOINT` | 启用后的默认值为 `http://127.0.0.1:4318/v1/traces`，接受完整 traces URL |
| `YUNQI_OTLP_SAMPLE_RATIO` | 0.1，范围 0–1；0 不导出 span |
| `YUNQI_OTLP_PROPAGATE_UPSTREAM` | true（仅启用 OTLP 后生效）；是否向供应商传播网关 traceparent |
| `OTEL_SERVICE_NAME` | yunqi；导出的服务名 |
| `USAGE_DRAIN_TIMEOUT_MS` | 0（关闭）；客户端断开后限时读取 usage，最大 5000 毫秒 |

出站策略覆盖实际拨号、DNS 结果、重定向和代理连接；云元数据地址始终阻止。私网阻止默认关闭，因此现有本地模型/企业私网渠道不会因升级自动失效。OTLP 也走同一出站策略，若开启私网限制，本地 collector 需要相应例外。

严格会话需要显式会话 ID，例如 `Session_id`、`Conversation_id`、`X-Claude-Code-Session-Id` 或既有显式 body 身份；不使用匿名 prompt 指纹。缺主体/会话返回 400，容量满返回 503。每进程最多 4096 条，30 分钟无活动过期，在途 lease 不过期；失败、渠道禁用或删除不触发迁移。该状态在重启后清空，多节点需入口会话粘性，不是分布式会话绑定。默认普通亲和行为保持。

OTLP 也接受 `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`，或 `OTEL_EXPORTER_OTLP_ENDPOINT` 加 `/v1/traces`；YUNQI_OTLP_ENDPOINT 优先。标准 OTEL_EXPORTER_OTLP_TRACES_HEADERS/OTEL_EXPORTER_OTLP_HEADERS 可提供 collector 认证。客户端使用系统 TLS 信任与网关出站策略，不使用 SDK 自建证书客户端；导出无重定向/重试，超时 2 秒，队列 512、批次 64，关闭最多 5 秒。只导出固定协议/状态/阶段及受限关联 ID，不导出 prompt、模型、URL、正文、凭证、baggage 或传入 tracestate。导出异常不阻塞请求，健康状态可从 Prometheus 查看。

正文日志策略不使用新增环境开关：仍由 `ENV`、`ENABLE_REQUEST_LOGS`、`ENABLE_RESPONSE_LOGS` 提供原有门控，并要求管理员经 `/api/logging/body` 独立显式启用正文；默认关闭。策略、实际 effective 状态、字节上限、轮转保留期与 actor 审计以该 API 为准，不能仅凭日志等级判断是否会记录正文。

断客 usage drain 仅适用于显式输出上限不超过 1024 token 的普通 HTTP 流式请求，最多读取 1 MiB；不用于 WebSocket 控制取消或竞速分支。客户端断开后不再写回客户端，只限时等待供应商计量。原请求 deadline、服务退出、预算或租约取消仍优先终止上游。缺少真实协议终态的 usage 标为不完整，不能因读到 EOF 就当作完整计费；诊断摘要仍保持取消结果，不记请求成功。

预置更新器的 `PRESET_TRUSTED_ED25519_KEYS` 接受 key ID 到 Base64 Ed25519 公钥的 JSON 对象。未配置时可检查未签名来源，但会明确显示未验证；配置可信公钥后拒绝未签名、未知 key 和错误签名，审核时按当前公钥重新验证，不能绕过已撤销 key。来源 digest 绑定下载的索引与分片校验链。

跨节点入口限额由 `PROXY_REDIS_URL` 显式启用，支持 redis/rediss；未设置仍使用本地准入。`PROXY_REDIS_NAMESPACE` 默认 yunqi，同一限额域的节点必须使用相同 namespace、稳定主体 ID 和限额策略；独立网关应使用不同 namespace。`PROXY_REDIS_MAX_RPM` 默认为 0（不额外限制），`PROXY_REDIS_LEASE_SECONDS` 默认 30，允许 5–300 秒。入口并发仍取主体策略或 `PROXY_MAX_CONCURRENCY`，共享租约限制各节点的总和，不按 Key 重试重复消耗逻辑请求 RPM。

Redis 不可用或续约失效时拒绝准入并取消受影响上游，不退回本地放行；活跃 Body（含后台计量读取）结束前保留租约。异常进程退出后等待 Redis 租约过期回收，不立即假定请求结束。

`PRESET_SOURCE_MAX_AGE`、`PRESET_SOURCE_FUTURE_SKEW` 使用 Go duration（如 `168h`、`5m`），默认 0 保持时间门槛未启用。启用时间校验后需要有效发布时间，拒绝过旧或超过允许时钟偏差的来源；审核等待期间也会重新检查。即使未设时间门槛，待审来源仍显示发布时间和检查时间，不能将检查成功等同于来源新鲜。
