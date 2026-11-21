import { describe, expect, it, vi } from 'vitest'
import { createChannelManagementApi } from './channel-management'
import type { ApiClient } from './client'

describe('channel save receipt', () => {
  it.each([undefined, {}, { persisted: false, revision: 'rev' }, { persisted: true }])('rejects missing persistence acknowledgement %o', async result => {
    const request = vi.fn().mockResolvedValue(result)
    const api = createChannelManagementApi({ request } as unknown as ApiClient)
    await expect(api.updateChannelV2('route', { fastMode: false })).rejects.toThrow('持久化')
  })
  it('returns the committed revision and sends conditional revision', async () => {
    const receipt = { persisted: true, changed: false, revision: 'new-revision' }
    const request = vi.fn().mockResolvedValue(receipt)
    const api = createChannelManagementApi({ request } as unknown as ApiClient)
    expect(await api.updateChannelV2('route', { fastMode: false }, 'old-revision')).toEqual(receipt)
    expect(request).toHaveBeenCalledWith('/channels/route', expect.objectContaining({ headers: { 'If-Match': 'old-revision' }, body: '{"fastMode":false}' }))
  })
  it('propagates validation and persistence failures', async () => {
    const request = vi.fn().mockRejectedValue(new Error('disk full'))
    const api = createChannelManagementApi({ request } as unknown as ApiClient)
    await expect(api.updateChannelV2('route', {})).rejects.toThrow('disk full')
  })
})
