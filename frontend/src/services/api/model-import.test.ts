import { expect, it, vi } from 'vitest'
import { createChannelManagementApi } from './channel-management'
import type { ApiClient } from './client'
it('encodes channel scopes and binds model previews to revisions', async () => {
  const request = vi.fn().mockResolvedValue({})
  const api = createChannelManagementApi({ request } as unknown as ApiClient)
  await api.getModelImport('channel/a', 'chat&other')
  expect(request).toHaveBeenLastCalledWith('/channels/channel%2Fa/model-import?protocol=chat%26other')
  await api.setModelImportPolicy('a', 'chat', { enabled: false }, 'rev-1')
  expect(request).toHaveBeenLastCalledWith('/channels/a/model-import/policy?protocol=chat', {
    method: 'PUT',
    headers: { 'If-Match': 'rev-1' },
    body: '{"enabled":false}'
  })
  await api.previewModelImport('a', 'chat', ['provider/raw'], 'rev-2')
  expect(request).toHaveBeenLastCalledWith('/channels/a/model-import/preview?protocol=chat', {
    method: 'POST',
    headers: { 'If-Match': 'rev-2' },
    body: '{"models":[{"id":"provider/raw"}]}'
  })
  await api.approveModelImport('a', 'chat', 'review-1', true, false)
  expect(request).toHaveBeenLastCalledWith('/channels/a/model-import/approve?protocol=chat', {
    method: 'POST',
    body: '{"reviewId":"review-1","allowEmpty":true,"allowRemovals":false}'
  })
  await api.removeModelImport('a', 'chat', 'rev-3', false)
  expect(request).toHaveBeenLastCalledWith('/channels/a/model-import?protocol=chat', {
    method: 'DELETE',
    headers: { 'If-Match': 'rev-3' },
    body: '{"allowUnrestricted":false}'
  })
})
