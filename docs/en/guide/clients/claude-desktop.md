# Connect Claude Desktop to Yunqi

Claude Desktop (the desktop app) supports a "Third-Party Inference" mode: instead of signing in to a Claude account, it sends inference requests to a self-hosted gateway. The Yunqi **Messages** endpoint implements the Anthropic Messages protocol (including streaming and tool use) plus the `/v1/models` discovery endpoint, so Yunqi can serve as the inference gateway for Claude Desktop.

## How it works

```text
Claude Desktop  ->  HTTPS wrapper  ->  Yunqi /v1/messages  ->  Messages channel  ->  upstream Anthropic-compatible endpoint
```

## Prerequisites

1. Yunqi is running (for example `http://localhost:3000`) with `PROXY_ACCESS_KEY` set
2. At least one working channel under the Messages endpoint
3. A recent version of [Claude Desktop](https://claude.com/download) (supported when the Developer menu exists)

## 1. Configure a Yunqi channel

Same as [Claude Code](./claude-code) — Claude Desktop speaks the Messages protocol:

1. Open the Yunqi admin console and go to **Messages**
2. Click "Add Channel" and fill in the channel configuration for your upstream provider

Common examples:

| Upstream | Service type | Base URL example |
|----------|--------------|------------------|
| Anthropic Claude | `Claude` | `https://api.anthropic.com` |
| DeepSeek Anthropic-compatible | `Claude` | `https://api.deepseek.com/anthropic` |
| Kimi coding endpoint | `Claude` | `https://api.kimi.com/coding/` |
| GLM Anthropic-compatible | `Claude` | `https://open.bigmodel.cn/api/anthropic` |

::: tip
For upstream API keys, model names, and special options, see the matching [provider setup guide](/en/providers/).
:::

## 2. Put Yunqi behind HTTPS (required)

The third-party inference gateway in Claude Desktop **only accepts HTTPS URLs**. Yunqi listens over plain HTTP locally (for example `http://localhost:3000`), so you cannot put that address into the Gateway base URL directly — wrap local Yunqi with a reverse proxy first. The recommended approach is [Caddy](https://caddyserver.com/) with its locally trusted certificates (`tls internal`):

1. Install Caddy:

```bash
# macOS
brew install caddy
```

```powershell
# Windows (either one)
winget install CaddyServer.Caddy
scoop install caddy
```

2. Create a `Caddyfile` in any directory. Replace `127.0.0.1:3000` with your actual Yunqi address and port:

```text
https://127.0.0.1:8443 {
    reverse_proxy 127.0.0.1:3000
    tls internal
}
```

3. Trust the local Caddy CA on first use, then start it:

```bash
caddy trust
caddy run
```

::: warning
`caddy trust` requires sudo on macOS. Claude Desktop then reaches Yunqi through `https://127.0.0.1:8443`. You can pick a different port, but the scheme must be HTTPS.
:::

## 3. Enable Developer Mode and configure the gateway

1. Launch Claude Desktop. **No Claude sign-in is required**
2. Enable Developer Mode:
   - macOS: menu bar **Help → Troubleshooting → Enable Developer Mode**
   - Windows: **☰** application menu in the top-left of the sign-in screen → **Help → Troubleshooting → Enable Developer Mode**
3. Open **Developer → Configure Third-Party Inference…** and fill in the **Connection** section:

| Field | Value |
|-------|-------|
| Inference provider | `Gateway` (default) |
| Gateway base URL | `https://127.0.0.1:8443` (the HTTPS wrapper address from the previous step; do not append `/v1`) |
| Gateway API key | The Yunqi `PROXY_ACCESS_KEY` |
| Gateway auth scheme | `bearer` (default) or `x-api-key`; Yunqi accepts both |
| Credential kind | `Static API key` (default) |

4. Click **Test connection** to verify connectivity
5. Click **Apply Changes** to save and apply

After applying, the model picker in Claude Desktop shows the models served by the gateway, and chat requests are forwarded through Yunqi to your configured upstream channels.

## 4. Configure the model list

Claude Desktop can populate the model picker in two ways:

- **Model discovery (automatic, on by default)**: requests `{Gateway base URL}/v1/models`. Yunqi implements this endpoint. Desktop filters model IDs that do not match its Claude naming rules unless the gateway supplies a valid `anthropic_family_tier`.
- **Model list (manual)**: click **Add model** to list model IDs one by one. This overrides discovery, and the **first entry is the default model**.

Open **Service & client setup → Client model mappings**:

1. Select **Claude Desktop**.
2. Each of `Haiku`, `Sonnet`, `Opus`, `Fable`, and `Mythos` has a target-model dropdown populated from active Messages channels. Several tiers can share one model.
3. Click **Save mappings**, then repeat Desktop's connection test. **Not mapped** removes a slot. Switching clients preserves unsaved drafts.

For example, map Haiku to `deepseek-flash` and all other tiers to `deepseek-v4-pro`. The global configuration stores client slots as keys:

```json
{
  "clientModelMappings": {
    "claude-desktop": {
      "haiku": "deepseek-flash",
      "sonnet": "deepseek-v4-pro",
      "opus": "deepseek-v4-pro",
      "fable": "deepseek-v4-pro",
      "mythos": "deepseek-v4-pro"
    }
  }
}
```

Discovery adds distinct aliases such as `yunqi-claude-desktop-haiku`, carrying the corresponding `anthropic_family_tier`. Messages and token-count requests resolve aliases to actual targets before normal scheduling, entry permissions, model admission and capability checks. Aliases preserve actual context limits, prices, provider identity and protocol verification; a tier does not establish equivalence with Claude.

Targets come from declared models, enabled-key model inventories or approved imports in active default-route channels, filtered by channel model rules. Wildcard-only configurations need concrete model declarations first. Prefix-only channels retain their separate access boundary. Saved unavailable targets remain visible but cannot resolve. Normal scheduling chooses among active channels serving the same target.

The same area supports independent profiles for **Claude Code** (five tiers, Messages), **Codex** (Default, Responses) and **OpenCode** (Default / Small, Chat). Use the displayed `yunqi-...` aliases in the setup wizard or client model settings. For example, set Claude Code's `ANTHROPIC_DEFAULT_HAIKU_MODEL` to `yunqi-claude-code-haiku`; Codex can use `yunqi-codex-default`; OpenCode can use `yunqi/yunqi-opencode-default` and `yunqi/yunqi-opencode-small`.

Legacy channel `claudeDesktopModelTiers` and `modelCapabilities.anthropicFamilyTier` continue working until a global profile is saved. Exact legacy tiers can populate the new editor. After saving, global Desktop slots control discovery tiers. Empty configurations persist, so clearing a mapping survives reload. Original channel settings remain intact. Opening the editor does not run inference tests.

The HTTP response includes `anthropic_family_tier`, `is_family_default`, `supports_1m`, and `max_input_tokens`, retaining the existing camelCase fields. The configuration key `anthropicFamilyTier` and the HTTP discovery fields belong to different interfaces. Check the running instance with:

```bash
curl "$YUNQI_BASE_URL/v1/models?limit=1000" \
  -H "Authorization: Bearer $PROXY_ACCESS_KEY" \
  -H "anthropic-version: 2023-06-01"
```

References: [Anthropic gateway documentation](https://claude.com/docs/third-party/claude-desktop/gateway), [model configuration](https://claude.com/docs/third-party/claude-desktop/models). The discovery filter and field names were also checked against Desktop 6.28899.6; behavior and controls may vary by version.

## 5. Managing multiple configurations

To switch between gateways (for example local Yunqi and a remote Yunqi deployment):

1. The **Select configuration** dropdown at the top-right of the configuration window defaults to `Default`
2. Use the dropdown to duplicate, rename, or create configurations; each keeps its own base URL, API key, and model list
3. **Developer → Open Developer Config File…** opens the JSON file behind the current configuration for backup and migration
4. **Export** produces a `.mobileconfig` (macOS) or `.reg` (Windows) artifact for MDM fleet deployment

## Troubleshooting

### "Can't reach 127.0.0.1:8443"

Claude Desktop cannot reach the HTTPS wrapper:

1. Is `caddy run` still running?
2. Does the port in the Gateway base URL match the Caddyfile listener?
3. Is Yunqi itself running (can you open the admin console at `http://localhost:3000`)?

### Test connection returns 401

The Gateway API key does not match the Yunqi `PROXY_ACCESS_KEY`. Check the Yunqi startup configuration and the key entered in the configuration window.

### Connected but the model picker is empty

Check whether `/v1/models` contains the expected IDs. If DeepSeek or another non-Claude model is present but Desktop reports `Gateway returned no usable models`, check for a valid `anthropic_family_tier` in the response. HTTP 200, a display name, or `supports1m` alone does not pass Desktop's filter. Configure an explicit tier or use the manual list described in section 4.

### Can I just enter `http://localhost:3000`?

No. The third-party inference gateway must be reached over HTTPS; plain `http://` addresses fail to connect. Use the HTTPS wrapper from section 2. If Yunqi is deployed on a remote server that already has a valid TLS certificate, enter `https://your-domain` directly and skip Caddy.

### Switching back to official Claude

Open **Developer → Configure Third-Party Inference…**, switch the Inference provider from `Gateway` back to the official sign-in (or simply sign in to a Claude account) to restore the official service.
