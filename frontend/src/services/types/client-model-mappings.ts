export type MappingClient = 'claude-desktop' | 'claude-code' | 'codex' | 'opencode'
export type ClientModelMappings = Partial<Record<MappingClient, Record<string, string>>>
export interface ClientMappingTarget { model: string; kind: string; channels: string[] }
export interface ClientMappingState { mappings: ClientModelMappings | null; targets: ClientMappingTarget[] }
