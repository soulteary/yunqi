import { describe, expect, it } from 'vitest'
import { spawnSync } from 'node:child_process'
import { clientConfig, gatewayRoot } from './client-setup'
describe('browser client setup', () => {
  it('exports an explicit Codex context window as a root TOML integer', () => {
    const config = clientConfig('codex', 'https://gateway.example/team', 'gpt-5.2', 1_000_000)
    const parsed = spawnSync(
      'bun',
      ['--eval', 'process.stdout.write(JSON.stringify(Bun.TOML.parse(await Bun.stdin.text())))'],
      { input: config.content, encoding: 'utf8' }
    )
    expect(parsed.status, parsed.stderr).toBe(0)
    const toml = JSON.parse(parsed.stdout)
    expect(toml.model_context_window).toBe(1_000_000)
    expect(toml.model).toBe('gpt-5.2')
    expect(toml.model_providers.ccx.model_context_window).toBeUndefined()
    expect(toml.model_auto_compact_token_limit).toBeUndefined()
  })
  it.each([0, -1, 1.5, NaN, Infinity, Number.MAX_SAFE_INTEGER + 1])(
    'rejects an invalid explicit Codex context window: %s',
    value =>
      expect(() => clientConfig('codex', 'https://gateway.example', 'model', value)).toThrow('positive safe integer')
  )
  it('does not carry a Codex context override into other clients', () => {
    for (const kind of ['claude', 'opencode'] as const) {
      expect(clientConfig(kind, 'https://gateway.example', 'model', 1_000_000)).toEqual(
        clientConfig(kind, 'https://gateway.example', 'model')
      )
    }
  })
  it('exports parseable Codex TOML with a Responses provider using the client API key environment', () => {
    const config = clientConfig('codex', 'https://gateway.example/team/v1/', 'model"with\\escapes')
    expect(config.filename).toBe('codex-config.toml')
    // Use Bun's TOML parser, available with the frontend's documented Bun toolchain.
    // Parsing the exact exported text catches literal \\n separators that substring checks miss.
    const parsed = spawnSync(
      'bun',
      ['--eval', 'process.stdout.write(JSON.stringify(Bun.TOML.parse(await Bun.stdin.text())))'],
      { input: config.content, encoding: 'utf8' }
    )
    expect(parsed.error).toBeUndefined()
    expect(parsed.status, parsed.stderr).toBe(0)
    expect(JSON.parse(parsed.stdout)).toEqual({
      model: 'model"with\\escapes',
      model_provider: 'ccx',
      model_providers: {
        ccx: {
          name: 'CCX',
          base_url: 'https://gateway.example/team/v1',
          wire_api: 'responses',
          env_key: 'CCX_PROXY_KEY'
        }
      }
    })
  })
  it('preserves a route prefix and normalizes the API suffix', () => {
    expect(gatewayRoot('https://gateway.example/team/v1/')).toBe('https://gateway.example/team')
    expect(clientConfig('claude', 'https://gateway.example/team', 'sonnet').content).toContain(
      'https://gateway.example/team'
    )
    expect(clientConfig('codex', 'https://gateway.example/team', 'model').content).toContain(
      'https://gateway.example/team/v1'
    )
  })
  it('rejects unsafe addresses', () => {
    for (const url of [
      'file:///tmp/key',
      'https://key@example.com',
      'https://example.com?key=secret',
      'https://example.com/#secret'
    ]) {
      expect(() => gatewayRoot(url)).toThrow()
    }
  })
  it('escapes user input and never substitutes an administrator credential', () => {
    const model = 'a"b'
    expect(JSON.parse(clientConfig('opencode', 'http://localhost:3000', model).content).model).toBe('ccx/' + model)
    expect(clientConfig('codex', 'http://localhost:3000', model).content).toContain('env_key = "CCX_PROXY_KEY"')
    expect(() => clientConfig('codex', 'http://localhost', 'a\nb')).toThrow()
  })
  it.each(['claude', 'codex', 'opencode'] as const)(
    'rejects every ASCII control character inside a %s model name',
    kind => {
      for (const code of [...Array.from({ length: 32 }, (_, value) => value), 127]) {
        expect(() => clientConfig(kind, 'https://gateway.example', `model${String.fromCharCode(code)}name`)).toThrow(
          'Model is required and must not contain control characters'
        )
      }
    }
  )
  it.each([' ', '~', '\u0080', '模型', '😀'])(
    'preserves allowed characters at and beyond the ASCII control boundaries: %s',
    character => {
      const model = `model${character}name`
      const content = clientConfig('claude', 'https://gateway.example', model).content
      expect(JSON.parse(content).env.ANTHROPIC_MODEL).toBe(model)
    }
  )
  it('normalizes trailing slashes and model whitespace, and rejects control characters', () => {
    expect(gatewayRoot('https://gateway.example/team/v1///')).toBe('https://gateway.example/team')
    expect(JSON.parse(clientConfig('claude', 'https://gateway.example', ' sonnet ').content).env.ANTHROPIC_MODEL).toBe(
      'sonnet'
    )
    expect(() => clientConfig('codex', 'https://gateway.example', 'model\u0000name')).toThrow()
  })
})
