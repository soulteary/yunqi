import { describe, expect, it } from 'vitest'
import { isValidUrl, parseQuickInput } from './quickInputParser'
import { normalizeQuickAddBaseUrls, recognizeQuickAddBaseUrl } from './quickAddChannel'

describe('IPv6 channel import', () => {
  it.each(['http://[::1]:11434', 'https://[2001:db8::1]:8443/v1#', 'http://[::ffff:192.0.2.1]/v1'])(
    'accepts a valid bracketed HTTP URL: %s', url => expect(isValidUrl(url)).toBe(true),
  )
  it.each(['http://[::1', 'http://[not-an-ip]', 'http://::1:11434', 'http://[::1]:65536', 'ftp://[::1]', 'http://-example.com', 'http://user:password@[::1]', 'http://[::1] /v1'])(
    'rejects malformed or unsupported URL: %s', url => expect(isValidUrl(url)).toBe(false),
  )
  it('preserves the full address, port, key and explicit version-prefix marker', () => {
    const url = 'http://[::1]:11434/proxy#'
    const result = parseQuickInput(url + '\nhttps://[2001:db8::1]:8443/v1#\nsk-test1234567890', 'openai')
    expect(result.rawBaseUrls).toEqual([url, 'https://[2001:db8::1]:8443/v1#'])
    expect(result.detectedApiKeys).toEqual(['sk-test1234567890'])
    expect(result.detectedBaseUrl).toBe(url)
    expect(normalizeQuickAddBaseUrls([url], 'chat')).toEqual([url])
    expect(recognizeQuickAddBaseUrl(url, 'chat')).toBe(url)
  })
  it('recognizes and strips endpoint paths without truncating IPv6 colons', () => {
    const result = parseQuickInput('http://[::1]:11434/v1/chat/completions sk-test1234567890')
    expect(result.detectedServiceType).toBe('openai')
    expect(result.detectedBaseUrl).toBe('http://[::1]:11434')
  })
})
