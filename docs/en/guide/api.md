# API and authentication

The authoritative routes are in `backend-go/routes*.go`. This page describes stable entry points and call conventions; see [environment settings](environment.md) and [architecture](architecture.md) for configuration and module boundaries.

## Credentials

- Proxy calls use `PROXY_ACCESS_KEY` or an explicitly configured extra proxy key, normally through `x-api-key` or `Authorization: Bearer ...`.
- Management calls use `ADMIN_ACCESS_KEY`, falling back to the primary proxy key when no separate admin key is configured. Extra proxy keys require a separate admin key and never grant management access.
- Gemini SDK calls may use `x-goog-api-key`; Responses WebSocket authentication also supports browser handshake subprotocols.
- `GET /health` is public. Disabling the Web UI leaves proxy calls available but disables the console and management APIs.
- A static-looking suffix such as `.js` never exempts a management URL from access control. CORS policy does not replace authentication.

## Proxy entry points

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/v1/messages`, `/v1/messages/count_tokens` | Messages and token counting |
| POST | `/v1/chat/completions` | Chat Completions |
| POST / GET | `/v1/responses` | Responses / WebSocket |
| POST | `/v1/responses/compact` | Responses compact |
| POST | `/v1/alpha/*` | Codex history/notes passthrough |
| GET | `/v1/models`, `/v1/models/:model` | Model list and details |
| POST | `/v1beta/models/*` | Gemini generateContent / streamGenerateContent |
| POST | `/v1/images/generations`, `/v1/images/edits`, `/v1/images/variations` | Images |
| POST | `/v1/embeddings` | Vectors |

Each entry also supports one `/:routePrefix` segment, such as `/team/v1/messages`; health checks support `/team/health`. Prefixes isolate channel candidates rather than providing interchangeable aliases. Prefixed proxy calls still require proxy authentication.

## Management and key mutations

Management endpoints include `/api/{type}/channels/*`, stable-UID `/api/channels/*`, `/api/logical-channels/*` and other `/api/*` modules. The six protocol types are messages, chat, responses, gemini, images and vectors.

The console sends keys in JSON bodies rather than URLs. Here `id` is the current protocol-specific channel array index, not a stable UID.

| Method | Path | Body |
| --- | --- | --- |
| POST | `/api/{type}/channels/:id/keys` | `{"apiKey":"..."}` to add a key |
| POST | `/api/{type}/channels/:id/keys/remove` | `{"apiKey":"..."}` |
| POST | `/api/{type}/channels/:id/keys/move-to-top` | `{"apiKey":"..."}` |
| POST | `/api/{type}/channels/:id/keys/move-to-bottom` | `{"apiKey":"..."}` |

Send `Content-Type: application/json` and an admin credential. Legacy `DELETE .../keys/:apiKey`, `POST .../keys/:apiKey/top` and `POST .../keys/:apiKey/bottom` remain for compatibility. Yunqi redacts keys from normalized log paths, but cannot control external reverse-proxy logs; new clients should use the JSON-body endpoints.

Capability tests apply only to messages, chat, responses and gemini, not Images or Vectors. Channel health and balance probes cover all six types; see the [current specifications](https://github.com/soulteary/yunqi/blob/main/docs/specs/README.md).
