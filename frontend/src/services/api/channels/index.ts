import type { ApiClient } from '../client'
import { createChannelsMessagesApi } from './messages'
import { createChannelsResponsesApi } from './responses'
import { createChannelsChatApi } from './chat'
import { createChannelsImagesApi } from './images'
import { createChannelsVectorsApi } from './vectors'
import { createChannelsGeminiApi } from './gemini'

export function createChannelsApi(client: ApiClient) {
  return {
    ...createChannelsMessagesApi(client),
    ...createChannelsResponsesApi(client),
    ...createChannelsChatApi(client),
    ...createChannelsImagesApi(client),
    ...createChannelsVectorsApi(client),
    ...createChannelsGeminiApi(client)
  }
}
export type ChannelsApi = ReturnType<typeof createChannelsApi>
