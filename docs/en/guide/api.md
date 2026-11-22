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
| POST | `/v1/quote` | Caller offline estimate |

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

## Conversion and streaming contracts

`X-Yunqi-Conversion` describes the final provider request after effort rewrites: `steps`, audited `coverage`, `quality`, and field-level `diagnostics`, without request values. `partial` means no loss was found within that coverage, `unknown` means inspection was unavailable, and `lossy` reports a known downgrade. Lost structured-output constraints or media structure are rejected before sending. Native requests still receive their existing compatibility transformations.

Chat `response_format`, Responses `text.format`, Claude `output_config.format`, and Gemini JSON Schema are mapped when representable. Final effort rewrites preserve schema property order. Gemini nullable/union/reference constructs use JSON Schema; unsupported constraints are not presented as equivalent support. OpenAI cache fields retain explicit false/null presence. Cross-vendor unsupported cache hints produce diagnostics. The channel JSON option `responsesClaudePrefixCache` is off by default and only adds a stable Responses→Claude history prefix when no explicit cache_control exists; explicit TTL settings are preserved.

Tool images and inline files remain structured, with parallel call/result IDs paired. Chat places media in a user message after contiguous tool replies; Gemini uses functionResponse.parts. Foreign file IDs and remote media with unknown MIME are rejected rather than downloaded or converted to binary text. Missing Gemini IDs are generated and paired FIFO by function name; an unmatched result is rejected.

Hosted search remains distinct from client function tools. When native citation positions cannot be represented, conversions preserve readable source/title/quoted text and report the fallback. Opaque search data and invented character positions are excluded. Unrepresentable hosted-search declarations require a native provider channel.

Responses stream errors identify upstream versus gateway origin; output already delivered is not replayed into another success. Responses WebSocket response.cancel/response.interrupt cancel the current turn while allowing another turn on the connection; disconnect still cancels work. TTFT starts at meaningful content, excluding metadata, empty deltas, and heartbeats.

## Team identities and caller policies

| Endpoint | Authority and purpose |
| --- | --- |
| `/api/entry-keys`, `/api/entry-keys/:id`, `/api/entry-keys/:id/rotate` | Global admin: caller credentials; plaintext returned only at creation/rotation, hashes persisted |
| `/api/admin-credentials`, `/api/admin-credentials/:id`, `/api/admin-credentials/:id/rotate` | Global admin: management credentials, roles, and channel UID scopes |
| `GET /api/access/me`, `GET /api/access/channels` | Limited-role identity and redacted channel inventory |
| `PUT /api/access/channels/:uid/state` | Scoped operator/admin state changes |
| `/access` | Limited-role portal; credentials stay in page memory |
| `POST /v1/quote` | Proxy-authenticated offline estimate, respecting model/channel policies |

viewer is read-only, operator can perform limited state operations, and scoped admin remains scoped. Global configuration, credential export, and complete diagnostics require an admin without channel restrictions. Limited roles cannot use legacy endpoints that return raw credentials. Management credentials do not implicitly authorize proxy calls. Entry policies restrict models, channel UIDs, expiry and peer CIDRs; per-principal concurrency also applies. CIDR admission uses the actual peer, not untrusted X-Forwarded-For.

Quotes distinguish known cost from incomplete input/output bounds or media prices; an incomplete quote is not zero cost. Usage records identify provider/estimated/mixed/unknown evidence and retain bounded, minimized source metering.

Trusted identity proxy authentication is disabled by default. Global admins configure issuer, trusted peer CIDRs, allowedOrigins and group-to-role/channel mappings through GET/PUT /api/identity-proxy. The server also needs IDENTITY_PROXY_SECRET of at least 32 bytes. The proxy must overwrite client assertions and supply the X-Yunqi-Identity-Secret, -Subject, -Groups, -Session and -Expires headers. Expires is the fixed session's Unix expiry; extending a session requires a new Session ID. Every SSO management call needs X-Yunqi-Management-Request: 1; writes also need an allowed Origin. Identity assertions are never forwarded upstream.

POST /api/access/session/revoke revokes the current SSO session. Global admins can revoke a sessionFingerprint through POST /api/identity-proxy/sessions/revoke. Raw subject/session values are not stored or returned. Revocations persist and survive role mapping edits. SSO does not grant data-plane access.

The external identity proxy and IdP perform OIDC login, Passkey and two-factor authentication. Yunqi verifies the trusted proxy, expiry, role/resource scope and revocation state. Copilot OAuth remains upstream authorization rather than administrator login.

`identityProxy.revocationMode` defaults to `local`, guaranteeing revocation on the current node only. `shared` checks an explicitly configured Redis backend on every SSO authentication. Replicas need the same backend/namespace and consistent identity proxy policy. Missing backends or read/write failures return 503 without falling back locally. Identity views and logout operations display their revocation scope; local logout is not global revocation.

Optional strict session routing atomically binds the first request before dispatch. An unavailable binding fails rather than migrating across channels or racing; retries of keys/URLs within that channel remain possible. See [environment settings](environment.md).

## Configuration and operational evidence

| Method and path | Result and limits |
| --- | --- |
| `GET /api/metrics/prometheus` | Global admin; bounded request/latency/concurrency/failover/limiter/stream/export health metrics |
| `GET /api/reports/request-timeline/:id` | Global admin; bounded logical timeline by server request ID or correlation ID |
| `GET /api/reports/requests` | Global admin; filtered SQLite request history with cursors |
| `GET/PUT /api/logging/body` | Global admin; separate body switches, revision and actor audit |
| `GET /api/logging/body/export` | Global admin; at most 1 MiB complete lines from the current file, redacted again, with truncation/scope metadata |

X-Yunqi-Request-Id links key/channel/racing attempts. Timelines retain structured state, error origins and safe IDs, excluding prompts, models, URLs, raw/masked credentials, sessions and free-form errors. Capacity is 1024 requests, 128 attempts and 256 events per request, with explicit drop counts; restarts clear them. HTTP-200 stream failures are visible in terminalOutcome/events. phaseTiming measures exclusive request wall time without adding overlapping stages twice; unknown phases are omitted.

History requires since/until in Unix seconds and supports type/model/channelUid/correlationId plus limit (1–200). Continue with the returned cursor and unchanged filters. A first-page snapshot bound prevents later inserts from shifting subsequent pages. Retention follows metric configuration and response metadata.

Body logging is off by default even in development with the old logging switches enabled. Global admins opt requestBodies/responseBodies in separately; settings activate only after an atomic durable actor-audit write. Effective output also requires development mode and the corresponding old switch. maxBytes defaults to 4096 (256–65536); media and structured personal identifiers are omitted, and non-JSON content logs only its length. Downloads exclude active partial/oversized lines and do not accept arbitrary paths.

For partial channel compatibility updates, omission preserves a field while explicit false/0/empty arrays update it; null clears or is rejected according to the field contract. Forms retain fields not edited. Management saves supporting revisions use the returned revision/ETag with If-Match and require reloading on conflict; failed persistence is not reported as a successful save.

Replacing all upstream keys clears stale key-failure state. Partial replacement retains health for surviving keys; manual suspension remains in force. A channel automatically suspended for having no keys can recover when usable keys are added.

## Stream completion, previews and pricing

Global admins manage stable-principal budgets with `GET/PUT /api/budgets`, inspect period state with `GET /api/budgets/:principal`, and read owned attempt ledgers through `GET /api/budgets/:principal/requests/:request`. Proxy-authenticated callers use `GET /v1/budget` for their own state only. Monetary API values are decimal USD strings. `costBasis=list_price_usd` means model list prices, rather than the purchase cost of a subscription channel.

Budgets support timezone-aware daily, weekly, monthly, lifetime or rolling periods and per-request caps. Every actual retry/racing send reserves separately and settles against verifiable provider usage. Input reservation uses serialized request bytes plus framing allowance; output needs an explicit or model-capability limit. Unknown prices, media and unpriced hosted-tool fees fail before contacting the provider. Authoritative zero usage releases reservations; incomplete/cancelled metering retains a conservative charge rather than being free.

The independent durable ledger freezes the request's initial period and pricing version. Expired process leases recover conservatively. Reservation is not an absolute guarantee of a provider bill: actual usage above the reservation is recorded and blocks subsequent over-budget calls. Configuration changes never reprice old records.

All four text endpoints validate source-protocol stream completion. EOF alone is not success. Missing terminal events, provider errors and incomplete compression remain failures; delivered content is not replayed through another attempt. Ordinary content streams promptly, with only a bounded unconfirmed terminal tail.

Global admins can call `POST /api/channels/:uid/request-preview` to run the actual request builder with the current channel or allowed draft fields. This is offline: no provider request, OAuth refresh, Copilot session exchange or history lookup occurs. Results redact draft credentials and declare sample/field coverage. Inputs requiring online state, such as a previous Responses ID, are rejected. Uncovered fields are not proof of support.

Model pricing supports input-length tiers, 5-minute/1-hour cache writes, separate reasoning output prices and image prices matched by quality/size. Token prices are per million tokens; image prices are per delivered image. Explicit zero is valid; negative and non-finite prices are rejected. Limits are 128 tiers, 64 image rules and 256 metering items, without user expression execution.

Requests freeze a content-derived pricing snapshot at start. `pricingVersionId` identifies it; later configuration changes do not reprice in-flight requests or stored history. Reasoning follows each provider's subset/additional-output semantics. Missing reasoning meters, TTL or applicable prices produce a known lower bound marked incomplete, not a complete bill. Legacy CNY conversion uses a fixed coefficient rather than live exchange rates.

The cost page includes remote preset price review and rollback. Global admins use `GET /api/presets/prices`, `POST /api/presets/prices/check`, `POST /api/presets/prices/approve` (reviewId, allowAnomalies), and `POST /api/presets/prices/rollback` (historical bundleHash). Downloaded price changes remain pending. Jumps above the default 50% threshold and pricing-dimension changes require explicit anomaly acknowledgement. Approval binds the entire current bundle and source digest, preventing stale-page overwrites.

Activation, actor audit and rollback snapshots share one atomic durable file. Failure does not publish the new state. A bundle containing price changes waits as a whole; metadata-only updates that do not affect price selection retain the existing update flow. Up to 10 revisions are retained. Rollback restores the same entire preset bundle after restart, including model capabilities, without repricing settled requests. Legacy caches with different price selection and no approval become pending instead of silently replacing prices at startup. Source signature status and publication/check times are visible; see [environment settings](environment.md) for trust and freshness requirements.
