export interface ConsoleStatus {
  version: { version: string; buildTime: string; gitCommit: string }
  uptimeSeconds: number
  mode: string
  channels: Record<string, number>
  settings: { port: number; bindHost: string; webUI: boolean }
}
