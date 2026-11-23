import type { MappingClient } from '@/services/types/client-model-mappings'

export const mappingClients: { title: string; value: MappingClient }[] = [
  { title: 'Claude Desktop', value: 'claude-desktop' },
  { title: 'Claude Code', value: 'claude-code' },
  { title: 'Codex', value: 'codex' },
  { title: 'OpenCode', value: 'opencode' }
]
export const clientSlots: Record<MappingClient, string[]> = {
  'claude-desktop': ['haiku', 'sonnet', 'opus', 'fable', 'mythos'],
  'claude-code': ['haiku', 'sonnet', 'opus', 'fable', 'mythos'],
  codex: ['default'],
  opencode: ['default', 'small']
}
export const clientProtocol: Record<MappingClient, string> = {
  'claude-desktop': 'messages', 'claude-code': 'messages', codex: 'responses', opencode: 'chat'
}
export const clientModelAlias = (client: MappingClient, slot: string) => `yunqi-${client}-${slot}`
