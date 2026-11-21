# API 与鉴权

接口实现以仓库的 `backend-go/routes*.go` 为准，本页只维护稳定入口与调用约定。配置见[环境变量](environment.md)，模块边界见[架构](architecture.md)。

## 密钥边界

- 代理请求使用 `PROXY_ACCESS_KEY` 或已配置的额外代理密钥，常用请求头为 `x-api-key` 或 `Authorization: Bearer ...`。
- 管理请求使用 `ADMIN_ACCESS_KEY`；未配置独立管理密钥时回退到主代理密钥。配置额外代理密钥时必须提供独立管理密钥，额外密钥不获得管理权限。
- Gemini SDK 也可使用 `x-goog-api-key`；Responses WebSocket 的鉴权兼容浏览器握手子协议。
- `GET /health` 无须认证。关闭 Web UI 不关闭代理接口，管理界面和管理 API 则不可用。
- 管理 URL 即使以 `.js` 等静态后缀结尾，也必须经过管理访问控制。CORS 来源策略不能替代鉴权。

## 代理入口

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| POST | `/v1/messages`、`/v1/messages/count_tokens` | Messages 与 token 计数 |
| POST | `/v1/chat/completions` | Chat Completions |
| POST / GET | `/v1/responses` | Responses / WebSocket |
| POST | `/v1/responses/compact` | Responses compact |
| POST | `/v1/alpha/*` | Codex history/notes 透传 |
| GET | `/v1/models`、`/v1/models/:model` | 模型列表和详情 |
| POST | `/v1beta/models/*` | Gemini generateContent / streamGenerateContent |
| POST | `/v1/images/generations`、`/v1/images/edits`、`/v1/images/variations` | Images |
| POST | `/v1/embeddings` | Vectors |

这些入口还支持一个 `/:routePrefix` 前缀，例如 `/team/v1/messages`；健康检查也支持 `/team/health`。前缀是候选渠道隔离条件，不能把同一渠道的无前缀地址当作等价别名。带前缀的代理入口仍须验证代理密钥。

## 管理与密钥操作

管理接口包括 `/api/{type}/channels/*`、稳定 UID 的 `/api/channels/*`、`/api/logical-channels/*` 以及各业务模块的 `/api/*`。`type` 为 messages、chat、responses、gemini、images 或 vectors。

控制台的密钥操作使用请求体，避免把明文密钥放入 URL。下面的 `id` 是该协议渠道数组的当前索引，不是稳定 UID。

| 方法 | 路径 | 请求体 |
| --- | --- | --- |
| POST | `/api/{type}/channels/:id/keys` | `{"apiKey":"..."}`，新增密钥 |
| POST | `/api/{type}/channels/:id/keys/remove` | `{"apiKey":"..."}` |
| POST | `/api/{type}/channels/:id/keys/move-to-top` | `{"apiKey":"..."}` |
| POST | `/api/{type}/channels/:id/keys/move-to-bottom` | `{"apiKey":"..."}` |

请求使用 `Content-Type: application/json` 和管理密钥。旧 `DELETE .../keys/:apiKey`、`POST .../keys/:apiKey/top`、`POST .../keys/:apiKey/bottom` 仅为客户端兼容保留。Yunqi 会对规范化路径中的密钥脱敏，但不能控制外部反向代理日志；新客户端应使用请求体接口。

能力测试仅适用于 messages、chat、responses、gemini，不适用于 Images 或 Vectors。健康检查和余额探测的渠道级管理入口覆盖六类协议，细节见[现行规格](../specs/README.md)。
