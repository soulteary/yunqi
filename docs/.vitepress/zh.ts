import type { DefaultTheme, LocaleSpecificConfig } from 'vitepress'

export const zhConfig: LocaleSpecificConfig<DefaultTheme.Config> = {
  title: 'Yunqi 文档',
  description: 'AI API 代理与协议转换网关',
  themeConfig: {
    nav: [
      { text: '指南', link: '/guide/getting-started' },
      { text: '客户端接入', link: '/guide/clients/' },
      { text: '配置教程', link: '/providers/' },
    ],
    sidebar: {
      '/guide/clients/': [
        {
          text: '客户端接入',
          items: [
            { text: '概览', link: '/guide/clients/' },
            { text: 'Claude Code', link: '/guide/clients/claude-code' },
            { text: 'Claude Desktop', link: '/guide/clients/claude-desktop' },
            { text: 'Codex CLI / Codex App', link: '/guide/clients/codex' },
            { text: 'OpenCode', link: '/guide/clients/opencode' },
            { text: 'Web console / migration', link: '/guide/desktop/' },
          ],
        },
      ],
      '/guide/': [
        {
          text: '入门',
          items: [
            { text: '名称、沿革与许可', link: '/guide/project-origin' },
            { text: '快速开始', link: '/guide/getting-started' },
            { text: 'Web console / migration', link: '/guide/desktop/' },
            { text: 'Web 控制台排障', link: '/guide/desktop/troubleshooting' },
            { text: '部署', link: '/guide/deployment' },
            { text: '环境变量', link: '/guide/environment' },
          ],
        },
        {
          text: '深入',
          items: [
            { text: '架构说明', link: '/guide/architecture' },
            { text: 'API 与鉴权', link: '/guide/api' },
            { text: '模型目录治理', link: '/guide/model-governance' },
            { text: '社区改进记录', link: '/guide/community-improvements' },
            { text: 'Benchmark 基准图表', link: '/guide/benchmark' },
            { text: '开发指南', link: '/guide/development' },
            { text: '发布流程', link: '/guide/release' },
            { text: '贡献指南', link: '/guide/contributing' },
            { text: '隐私政策', link: '/guide/privacy' },
          ],
        },
      ],
      '/providers/': [
        {
          text: '配置教程',
          items: [
            { text: '概览', link: '/providers/' },
            { text: 'DeepSeek', link: '/providers/deepseek' },
            { text: '智谱 GLM', link: '/providers/glm' },
            { text: 'MiniMax', link: '/providers/minimax' },
            { text: 'Kimi (月之暗面)', link: '/providers/kimi' },
            { text: 'OpenAI GPT', link: '/providers/openai' },
            { text: '小米 MiMo', link: '/providers/mimo' },
            { text: 'Claude', link: '/providers/claude' },
            { text: 'Gemini', link: '/providers/gemini' },
          ],
        },
      ],
    },
    outline: { label: '页面导航' },
    lastUpdated: { text: '最后更新于' },
    docFooter: { prev: '上一页', next: '下一页' },
  },
}
