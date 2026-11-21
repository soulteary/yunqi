# Quick Start


## Installation

Automated publishing is paused. Build from source or use the repository's local Docker build; check [release status](release.md) before expecting downloadable assets.

First clone the repository:

```bash
git clone https://github.com/soulteary/yunqi
cd yunqi
```

### Local Docker Build

Replace the sample `PROXY_ACCESS_KEY` in `docker-compose.yml`, then start a new deployment:

```bash
docker compose up --build -d
```

This builds `yunqi:local` from the repository. The default service address is `http://localhost:3000`.

### Go Binary Built From Source

With Go 1.26+, Bun and Make installed, run from the repository root:

```bash
(cd frontend && bun install)
make build
export PROXY_ACCESS_KEY=your-proxy-key
./dist/yunqi
```

Replace the example key before starting. The binary listens on port 3000 unless you set `PORT`; persistent files use paths relative to the working directory unless CLI options override them. See [service startup](../../service/README.md) for background operation.

### Published Artifacts

When the desired version is available in [GitHub Releases](https://github.com/soulteary/yunqi/releases), you can download its binary instead. Use a registry image only after confirming that its tag exists. Published artifacts are alternatives to the source build, not prerequisites for it.

## Core Concepts

### Channels

A channel represents a configured upstream API connection, including:

- **API Key**: Authentication credential for the upstream service
- **Base URL**: Upstream API endpoint
- **Model List**: Models available through this channel
- **Priority**: Scheduling weight

### Six Proxy Endpoints

| Endpoint | Path | Description |
|----------|------|-------------|
| Claude Messages | `/v1/messages` | Claude native protocol |
| OpenAI Chat | `/v1/chat/completions` | OpenAI Chat protocol |
| Codex Responses | `/v1/responses` | OpenAI Responses protocol |
| Gemini | `/v1beta/models/*` | Gemini native protocol |
| Images | `/v1/images/*` | OpenAI Images protocol |
| Vectors | `/v1/embeddings` | OpenAI Embeddings protocol |

## Admin Console

Visit the configured address (port 3000 by default). If `ADMIN_ACCESS_KEY` is set, use it to sign in; otherwise use `PROXY_ACCESS_KEY`, as in the examples above.

From the admin console you can:
- Add and manage channels
- View request logs and traffic stats
- Test channel connectivity
- Adjust channel priorities

### Editing model allowlists and remarks

Open the channel editor and enter rules under **Allowed models (allowlist)** in **Basic information**, then click **Save**. Rules apply to each protocol route independently. Separate rules with newlines or commas; exact names, wildcards such as `gpt*`, `*luna`, and `*deepseek*`, and exclusions such as `!*luna` are supported. Save an empty field to clear that route's restriction.

Channel lists display a nonempty remark first and show the original name on hover. Channels without a remark retain their original display name.

## Next Steps

Head to [Client Setup](/en/guide/clients/) to configure Claude Code, Codex CLI / App, or OpenCode; head to [Provider Setup](/en/providers/) to configure each LLM provider.

For existing desktop installations see the [migration guide](./desktop/).
