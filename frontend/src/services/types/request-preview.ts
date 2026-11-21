export interface RequestPreviewInput {
  protocol: string
  model: string
  body: unknown
  stream?: boolean
  draft?: Record<string, unknown>
}
export interface RequestPreviewResult {
  mode: string
  sourceProtocol: string
  targetProtocol: string
  method: string
  url: string
  headers: Record<string, string>
  body?: unknown
  bodyOmitted: boolean
  effects: { field: string; status: string }[]
  boundaries: string[]
}
