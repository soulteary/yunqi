export interface OutboundNetworkConfig {
  proxyMode?: 'legacy' | 'direct' | 'proxy' | 'direct_first'
  interfaceName?: string
  addressFamily?: 'auto' | 'ipv4' | 'ipv6'
  dns?: { mode?: 'system' | 'bound'; servers?: string[] }
}
export interface NetworkInterface {
  name: string
  index: number
  addresses: string[]
  up: boolean
  loopback: boolean
  bindingSupported: boolean
}
export interface NetworkDiagnosis {
  branch: string
  stage: string
  success: boolean
  error?: string
  interfaceName: string
  interfaceIndex: number
  bindingApplied: boolean
  localAddress?: string
  remoteAddress?: string
}
