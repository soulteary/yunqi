# Connect OpenCode to Yunqi

If you are using **Yunqi Desktop**, you can apply OpenCode settings from [Agent configuration](/en/guide/desktop/#agent-configuration) first, then return here to confirm the Chat endpoint and Base URL behavior.

OpenCode can connect to Yunqi through OpenAI Chat-compatible configuration. Use the Yunqi **Chat** endpoint.

## How it works

```text
OpenCode  ->  Yunqi /v1/chat/completions  ->  Chat channel  ->  upstream Chat-compatible endpoint
```

## 1. Configure a Yunqi channel

1. Open the Yunqi admin console and go to **Chat**
2. Click "Add Channel"
3. Add an OpenAI Chat-compatible channel

Common examples:

| Upstream | Service type | Base URL example |
|----------|--------------|------------------|
| OpenAI | `OpenAI Chat` | `https://api.openai.com/v1` |
| DeepSeek | `OpenAI Chat` | `https://api.deepseek.com` |
| GLM | `OpenAI Chat` | `https://open.bigmodel.cn/api/paas/v4` |
| MiniMax | `OpenAI Chat` | `https://api.minimax.io/v1` |
| Kimi | `OpenAI Chat` | `https://api.moonshot.ai/v1` |

::: tip
Model names, vision support, and special options differ by upstream. Finish the matching [provider setup guide](/en/providers/) first, then configure OpenCode.
:::

## 2. Configure OpenCode

### Use Yunqi Desktop automation

In **Agent Config → OpenCode**, keep the default **Yunqi local gateway** provider. Desktop maintains:

- `~/.config/opencode/opencode.jsonc`: the OpenAI-compatible custom provider, for example `provider.yunqi.options.baseURL`
- `~/.local/share/opencode/auth.json`: the matching provider API key, for example `auth.yunqi.key`

In default Yunqi mode, the Base URL is:

```text
http://127.0.0.1:<current Yunqi port>/v1
```

Select `yunqi/<model>` in OpenCode. Requests enter Yunqi through the Chat protocol and are then routed by Yunqi Chat channels to your configured upstreams.

Agent Config also offers direct providers, but only for currently supported OpenAI Chat-compatible domestic providers plus OpenCode Zen / OpenCode Go. For any other provider, add it manually using the official OpenCode custom-provider flow.

### Manual setup

In OpenCode, choose an OpenAI-compatible / custom provider and fill in:

| Setting | Value |
|---------|-------|
| API Key | `your-yunqi-proxy-key` |
| Base URL | `http://localhost:3000/v1` |
| Model | requested model name, for example `gpt-5` or `deepseek-v4-pro` |

If your OpenCode version uses a config file, the core values are still the same:

```text
API Key: your-yunqi-proxy-key
Base URL: http://localhost:3000/v1
Model: your-model-name
```

::: warning
OpenCode settings screens and field names may change across versions. As long as you choose an OpenAI Chat-compatible provider, use the API Key, Base URL, and Model values above.
:::

## 3. Recommended model mapping

If OpenCode sends OpenAI-style model names but the upstream uses provider-specific names, configure model mapping on the Chat channel.

Example:

| Request model match | Example upstream model |
|---------------------|------------------------|
| `gpt` | upstream primary model |
| `mini` | upstream lightweight model |
| `deepseek` | DeepSeek model |

If you prefer OpenCode to request upstream model names directly, skip mapping and enter the actual model name in OpenCode.

## Troubleshooting

### Should Base URL be root or `/v1`?

For OpenAI Chat-compatible OpenCode setup, Base URL is usually:

```text
http://localhost:3000/v1
```

Do not point it to the concrete endpoint:

```text
http://localhost:3000/v1/chat/completions
```

### 401 Unauthorized

Check:

1. The OpenCode API key matches Yunqi `PROXY_ACCESS_KEY`
2. You did not enter the upstream provider API key
3. Yunqi was started with the same `PROXY_ACCESS_KEY`

### 404 or Method Not Allowed

This usually means provider type or Base URL does not match. Confirm:

1. OpenCode uses an OpenAI Chat-compatible provider
2. Base URL is `http://localhost:3000/v1`
3. Yunqi has a **Chat** channel, not only Messages or Responses channels

### model_not_found

Check the Chat channel:

1. The model allowlist includes the requested model or mapped upstream model
2. The model name in OpenCode matches your mapping rules
3. The upstream model name is valid

### Tool calls or multi-turn context behave unexpectedly

OpenCode uses Chat Completions. Different upstreams vary in support for tool calls, JSON output, and system messages. If compatibility issues appear:

1. Prefer an upstream with native OpenAI Chat tool-call support
2. Disable unsupported response formats or advanced model options
3. If only one upstream fails, adjust channel priority or model mapping so these requests use a more compatible channel

### Requests do not appear in Chat channel logs

Check whether the current OpenCode provider still points to another service. Yunqi should see this request path:

```text
/v1/chat/completions
```
