export type ClientKind = 'claude' | 'codex' | 'opencode'

export function gatewayRoot(value: string): string {
  const url = new URL(value)
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash) {
    throw new Error('Use an HTTP(S) gateway URL without credentials, query or fragment')
  }
  return url.toString().replace(/\/+$/, '').replace(/\/v1$/, '')
}

export function clientConfig(
  kind: ClientKind,
  address: string,
  model: string,
  codexContextWindow?: number
): { filename: string; content: string } {
  const root = gatewayRoot(address)
  const key = 'YOUR_YUNQI_PROXY_KEY'
  model = model.trim()
  const hasControlCharacter = Array.from(model).some(character => {
    const code = character.charCodeAt(0)
    return code < 32 || code === 127
  })
  if (!model || hasControlCharacter) throw new Error('Model is required and must not contain control characters')
  if (kind === 'claude')
    return {
      filename: 'claude-settings.json',
      content: JSON.stringify(
        { env: { ANTHROPIC_BASE_URL: root, ANTHROPIC_API_KEY: key, ANTHROPIC_MODEL: model } },
        null,
        2
      )
    }
  if (kind === 'codex') {
    if (codexContextWindow !== undefined && (!Number.isSafeInteger(codexContextWindow) || codexContextWindow <= 0))
      throw new Error('Codex context window must be a positive safe integer')
    const context = codexContextWindow === undefined ? '' : `model_context_window = ${codexContextWindow}\n`
    return {
      filename: 'codex-config.toml',
      content: `model = ${JSON.stringify(model)}\nmodel_provider = "yunqi"\n${context}\n[model_providers.yunqi]\nname = "Yunqi"\nbase_url = ${JSON.stringify(root + '/v1')}\nwire_api = "responses"\nenv_key = "YUNQI_PROXY_KEY"\n`
    }
  }
  return {
    filename: 'opencode.json',
    content: JSON.stringify(
      {
        $schema: 'https://opencode.ai/config.json',
        model: `yunqi/${model}`,
        provider: {
          yunqi: {
            npm: '@ai-sdk/openai-compatible',
            name: 'Yunqi',
            options: { baseURL: root + '/v1', apiKey: '{env:YUNQI_PROXY_KEY}' },
            models: { [model]: { name: model } }
          }
        }
      },
      null,
      2
    )
  }
}
