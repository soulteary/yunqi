export interface CopilotDeviceCodeResponse {
  deviceCode: string
  userCode: string
  verificationUri: string
  expiresIn: number
  interval: number
}

export interface CopilotTokenResponse {
  accessToken?: string
  tokenType?: string
  scope?: string
  error?: string
  errorDescription?: string
}

export interface CopilotUserResponse {
  login: string
  id: number
  avatarUrl?: string
  htmlUrl?: string
}

// 健康检查响应类型
export interface HealthResponse {
  version?: {
    version: string
    buildTime: string
    gitCommit: string
  }
  timestamp: string
  uptime: number
  mode: string
}

export interface ConsoleStatus {
  version: { version: string; buildTime: string; gitCommit: string }
  uptimeSeconds: number
  mode: string
  channels: Record<string, number>
  settings: { port: number; bindHost: string; webUI: boolean }
}
