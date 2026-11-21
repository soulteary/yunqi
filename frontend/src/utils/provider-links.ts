// 服务商官方控制台链接映射。
// 订阅中心展示卡片的外链入口。

/** 官方控制台链接（查看用量 / 余额 / 密钥） */
export const providerConsoleLinks: Record<string, string> = {
  'github-copilot': 'https://github.com/settings/copilot',
  deepseek: 'https://platform.deepseek.com/usage',
  mimo: 'https://platform.xiaomimimo.com/console/balance',
  compshare: 'https://console.compshare.cn/light-gpu/model-subscription',
  runapi: 'https://runapi.co/console',
  tokenrhythm: 'https://tokenrhythm.studio/',
  'tencent-lkeap': 'https://console.cloud.tencent.com/lkeap/token-plan',
  kimi: 'https://platform.moonshot.cn/console/account',
  volcengine: 'https://console.volcengine.com/ark/region:cn-beijing/subscription/coding-plan?projectName=default',
  qianfan: 'https://console.bce.baidu.com/qianfan/resource/subscribe',
  originrouter: 'https://easytransnote.com/ai/console/#key',
  glm: 'https://open.bigmodel.cn/coding-plan/personal/overview',
  sensenova: 'https://platform.sensenova.cn/console',
  minimax: 'https://platform.minimaxi.com/user-center/payment/balance',
  dashscope: 'https://bailian.console.aliyun.com/cn-beijing?tab=model#/api-key',
  'opencode-zen': 'https://opencode.ai/',
  openrouter: 'https://openrouter.ai/keys',
  modelscope: 'https://modelscope.cn/my/myaccesstoken',
  xfyun: 'https://console.xfyun.cn/',
  stepfun: 'https://platform.stepfun.com/interface-key',
  bitdeer: 'https://www.bitdeer.ai/en/model/apikeys',
  atomgit: 'https://ai.atomgit.com/dashboard/coding-plan'
}

export function openExternal(url: string) {
  if (typeof window !== 'undefined') window.open(url, '_blank', 'noopener,noreferrer')
}

export function openProviderConsole(providerId: string) {
  const url = providerConsoleLinks[providerId]
  if (url) openExternal(url)
}
