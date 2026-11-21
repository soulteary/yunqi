import { describe, expect, it } from 'vitest'
import { clientConfig, gatewayRoot } from './client-setup'
describe('browser client setup', () => {
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
  it('normalizes trailing slashes and model whitespace, and rejects control characters', () => {
    expect(gatewayRoot('https://gateway.example/team/v1///')).toBe('https://gateway.example/team')
    expect(JSON.parse(clientConfig('claude', 'https://gateway.example', ' sonnet ').content).env.ANTHROPIC_MODEL).toBe(
      'sonnet'
    )
    expect(() => clientConfig('codex', 'https://gateway.example', 'model\u0000name')).toThrow()
  })
})
