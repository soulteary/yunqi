# Quick Start


## Installation

### Docker (Recommended)

```bash
docker run -d \
  --name yunqi \
  -p 3000:3000 \
  -v ./.config:/app/.config \
  -e PROXY_ACCESS_KEY=your-proxy-key \
  ghcr.io/soulteary/yunqi:latest
```

### Binary

Download the binary for your platform from [GitHub Releases](https://github.com/soulteary/yunqi/releases), then run:

```bash
export PROXY_ACCESS_KEY=your-proxy-key
./yunqi
```

The service listens on `http://localhost:3000` by default.

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

Visit `http://localhost:3000` and log in with your `ADMIN_ACCESS_KEY`.

From the admin console you can:
- Add and manage channels
- View request logs and traffic stats
- Test channel connectivity
- Adjust channel priorities

## Next Steps

Head to [Client Setup](/en/guide/clients/) to configure Claude Code, Codex CLI / App, or OpenCode; head to [Provider Setup](/en/providers/) to configure each LLM provider.

## Web console

Open the configured service address and port in a browser. Sign in, add and test upstream channels, then use **Service & setup** to generate client configuration. For existing desktop installations see [migration](./desktop/).
