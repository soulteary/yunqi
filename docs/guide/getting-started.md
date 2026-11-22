# 快速开始


## 安装

自动发布暂停期间，请从源码构建，或使用仓库中的本地 Docker 构建配置。下载产物是否可用，以[发布状态](release.md)和实际发布列表为准。

先获取源码：

```bash
git clone https://github.com/soulteary/yunqi
cd yunqi
```

### 构建本地 Docker 镜像

替换 `docker-compose.yml` 中的示例 `PROXY_ACCESS_KEY`，再启动一个新部署：

```bash
docker compose up --build -d
```

该命令从仓库构建 `yunqi:local`，默认访问地址为 `http://localhost:3000`。

### 从源码构建 Go 二进制

准备好 Go 1.27.4+、Bun 和 Make 后，在仓库根目录执行：

```bash
(cd frontend && bun install)
make build
export PROXY_ACCESS_KEY=your-proxy-key
./dist/yunqi
```

启动前替换示例密钥。未设置 `PORT` 时使用 3000 端口；未指定 CLI 路径选项时，持久化文件相对于启动目录保存。后台运行方法见[服务启动说明](../service/README.md)。

### 使用已发布产物

目标版本在 [GitHub Releases](https://github.com/soulteary/yunqi/releases) 提供二进制后，也可以直接下载。使用镜像仓库的产物前，先确认对应标签存在；源码构建不依赖这些产物。

## 基本概念

### 渠道 (Channel)

渠道是 Yunqi 的核心概念，每个渠道对应一个上游 API 的配置，包括：

- **API Key**：上游服务的认证密钥
- **Base URL**：上游 API 的地址
- **模型列表**：该渠道支持的模型
- **优先级**：调度时的优先级权重

### 六类代理入口

| 入口 | 路径 | 说明 |
|------|------|------|
| Claude Messages | `/v1/messages` | Claude 原生协议 |
| OpenAI Chat | `/v1/chat/completions` | OpenAI Chat 协议 |
| Codex Responses | `/v1/responses` | OpenAI Responses 协议 |
| Gemini | `/v1beta/models/*` | Gemini 原生协议 |
| Images | `/v1/images/*` | OpenAI Images 协议 |
| Vectors | `/v1/embeddings` | OpenAI Embeddings 协议 |

## 访问管理界面

启动后访问实际配置的地址（默认端口为 3000）。设置了 `ADMIN_ACCESS_KEY` 时使用管理密钥登录；未设置时使用 `PROXY_ACCESS_KEY`，与上面的安装示例一致。

![渠道管理界面](/images/guide/channel-list.png)

在管理界面中，你可以：
- 添加和管理渠道
- 查看请求日志和流量统计
- 测试渠道连通性
- 调整渠道优先级

### 编辑渠道模型白名单与备注

打开渠道编辑窗口，在“基础信息”的“允许的模型（白名单）”中按协议路由填写规则，然后点击窗口底部的“保存”。每行一条规则，也可用逗号分隔；支持精确模型名、`gpt*`、`*luna`、`*deepseek*` 等通配符，以及 `!*luna` 等排除规则。留空并保存表示清除该路由的模型限制。

渠道列表和卡片优先显示非空的渠道备注，悬停可查看原名称；未填写备注时显示原名称。此显示方式不会修改渠道原名称或路由规则。

## 下一步

前往 [客户端接入](/guide/clients/) 配置 Claude Code、Codex CLI / App 或 OpenCode；前往 [配置教程](/providers/) 了解如何配置各个 LLM 提供商。

旧版桌面数据迁移见[迁移指南](./desktop/)。
