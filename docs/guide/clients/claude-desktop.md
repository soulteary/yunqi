# Claude Desktop 接入 Yunqi

Claude Desktop（桌面客户端）支持「第三方推理（Third-Party Inference）」模式：不登录 Claude 官方账号，把推理请求发给自建网关。Yunqi 的 Messages 入口实现了 Anthropic Messages 协议（含流式与工具调用）以及 `/v1/models` 模型发现端点，可直接作为 Claude Desktop 的推理网关。

## 工作方式

```text
Claude Desktop  ->  HTTPS 包装层  ->  Yunqi /v1/messages  ->  Messages 渠道  ->  上游 Anthropic 兼容端点
```

## 前提条件

1. Yunqi 已启动（例如 `http://localhost:3000`），并已设置 `PROXY_ACCESS_KEY`
2. Messages 入口下至少有一个可用渠道
3. 安装较新版本的 [Claude Desktop](https://claude.com/download)（菜单中存在 Developer 菜单即支持）

## 一、配置 Yunqi 渠道

与 [Claude Code](./claude-code) 相同，Claude Desktop 走 Messages 协议：

1. 打开 Yunqi 管理界面，进入 **Messages** 入口
2. 点击「添加渠道」，按上游提供商填写渠道配置

常见配置：

| 上游 | 服务类型 | Base URL 示例 |
|------|----------|---------------|
| Claude 官方 | `Claude` | `https://api.anthropic.com` |
| DeepSeek Anthropic 兼容 | `Claude` | `https://api.deepseek.com/anthropic` |
| Kimi 编码端点 | `Claude` | `https://api.kimi.com/coding/` |
| GLM Anthropic 兼容 | `Claude` | `https://open.bigmodel.cn/api/anthropic` |

::: tip
具体上游的 API Key、模型名和特殊开关，请参考对应的[提供商配置教程](/providers/)。
:::

## 二、为 Yunqi 提供 HTTPS 入口（必须）

Claude Desktop 的第三方推理网关**只接受 HTTPS 地址**。Yunqi 本地默认以 HTTP 监听（如 `http://localhost:3000`），不能直接填入 Gateway base URL，需要先用反向代理把本地 Yunqi 包装成 HTTPS。推荐使用 [Caddy](https://caddyserver.com/) 的本地自签证书（`tls internal`）：

1. 安装 Caddy：

```bash
# macOS
brew install caddy
```

```powershell
# Windows（任选其一）
winget install CaddyServer.Caddy
scoop install caddy
```

2. 在任意目录创建 `Caddyfile`，把 `127.0.0.1:3000` 换成你的 Yunqi 实际地址和端口：

```text
https://127.0.0.1:8443 {
    reverse_proxy 127.0.0.1:3000
    tls internal
}
```

3. 首次运行先让 Caddy 把本地 CA 安装进系统信任库，再启动：

```bash
caddy trust
caddy run
```

::: warning
`caddy trust` 在 macOS 上需要 sudo 权限。之后 Claude Desktop 通过 `https://127.0.0.1:8443` 访问 Yunqi，端口可自行调整，但协议必须是 HTTPS。
:::

## 三、启用开发者模式并配置网关

1. 启动 Claude Desktop，**无需登录** Claude 账号
2. 启用开发者模式：
   - macOS：顶部菜单栏 **Help → Troubleshooting → Enable Developer Mode**
   - Windows：登录界面左上角 **☰** 应用菜单 → **Help → Troubleshooting → Enable Developer Mode**
3. 打开 **Developer → Configure Third-Party Inference…**，在 **Connection** 区域填写：

| 字段 | 填写值 |
|------|--------|
| Inference provider | `Gateway`（默认） |
| Gateway base URL | `https://127.0.0.1:8443`（上一步的 HTTPS 包装地址，不要带 `/v1`） |
| Gateway API key | Yunqi 的 `PROXY_ACCESS_KEY` |
| Gateway auth scheme | `bearer`（默认）或 `x-api-key`，Yunqi 两者都接受 |
| Credential kind | `Static API key`（默认） |

4. 点击 **Test connection** 验证连通性
5. 点击 **Apply Changes** 保存并生效

配置完成后，Claude Desktop 主界面的模型选择器会显示网关提供的模型，对话请求经 Yunqi 转发到你配置的上游渠道。

## 四、配置模型列表

Claude Desktop 提供两种方式填充模型选择器：

- **Model discovery（自动发现，默认开启）**：自动请求 `{Gateway base URL}/v1/models` 拉取模型列表。Yunqi 已实现该端点。Claude Desktop 会过滤非 Claude 风格的模型 ID；只有名称符合客户端规则，或网关返回有效的 `anthropic_family_tier` 时，才会进入自动发现结果。
- **Model list（手动列表）**：点击 **Add model** 逐个添加模型 ID，会覆盖自动发现的结果，**第一项为默认模型**。

在 Yunqi 的「服务与接入」页面，使用独立的「客户端模型映射」区域配置：

1. 客户端选择 **Claude Desktop**。
2. `Haiku`、`Sonnet`、`Opus`、`Fable`、`Mythos` 各占一行，从当前激活的 Messages 渠道模型中选择目标。多个档位可以使用同一个模型。
3. 点击「保存映射」，再执行 Desktop 连接测试。选择「不映射」会删除该档位的映射；切换客户端保留未保存的草稿。

例如，`Haiku` 指向 `deepseek-flash`，其余四档均可指向 `deepseek-v4-pro`。映射保存在全局配置，方向为客户端档位到实际模型：

```json
{
  "clientModelMappings": {
    "claude-desktop": {
      "haiku": "deepseek-flash",
      "sonnet": "deepseek-v4-pro",
      "opus": "deepseek-v4-pro",
      "fable": "deepseek-v4-pro",
      "mythos": "deepseek-v4-pro"
    }
  }
}
```

模型发现会额外提供 `yunqi-claude-desktop-haiku` 等独立别名，并带对应 `anthropic_family_tier`。使用别名的 Messages 请求及 token 计数请求先解析到实际目标，再执行原有渠道选择、入口权限、模型准入和能力检查。映射不改变目标的实际上下文、价格、供应商身份或协议验证状态；客户端档位不表示与 Claude 能力等价。

下拉列表来自默认入口下激活渠道声明的模型、启用 Key 的模型清单或已批准的导入目录，并应用渠道模型白名单。仅有通配规则时，需要先在渠道中声明具体模型；前缀专用渠道保持独立，不通过全局别名暴露。已保存但当前不可用的目标会保留显示，无法通过该别名发起请求。相同目标的多个激活渠道继续由调度器选择。

同一区域也支持 **Claude Code**（五个档位，Messages）、**Codex**（Default，Responses）和 **OpenCode**（Default / Small，Chat）。它们的映射互相独立；把界面显示的 `yunqi-...` 别名填入接入向导或客户端相应模型设置。例如 Claude Code 可将 `ANTHROPIC_DEFAULT_HAIKU_MODEL` 设为 `yunqi-claude-code-haiku`，Codex 可使用 `yunqi-codex-default`，OpenCode 可使用 `yunqi/yunqi-opencode-default` 和 `yunqi/yunqi-opencode-small`。

旧渠道 `claudeDesktopModelTiers` 和 `modelCapabilities.anthropicFamilyTier` 在未保存新配置时继续沿用。旧的精确 tier 可在新编辑区回显；首次保存后，全局 Desktop 映射负责发现的档位，空配置也会持久化，避免重载后恢复已清除的旧设置。旧渠道配置不被改写。配置页面本身不执行推理测试。

模型发现响应使用 `anthropic_family_tier`、`is_family_default`、`supports_1m` 和 `max_input_tokens`；原有驼峰字段继续保留。配置 JSON 中的 `anthropicFamilyTier` 与 HTTP 响应中的下划线字段属于不同接口。可用以下请求检查当前运行实例：

```bash
curl "$YUNQI_BASE_URL/v1/models?limit=1000" \
  -H "Authorization: Bearer $PROXY_ACCESS_KEY" \
  -H "anthropic-version: 2023-06-01"
```

参考：[Anthropic 网关文档](https://claude.com/docs/third-party/claude-desktop/gateway)、[模型配置](https://claude.com/docs/third-party/claude-desktop/models)。客户端过滤规则和字段名同时在 Claude Desktop 6.28899.6 中核对；不同版本的界面和能力可能不同。

## 五、多配置管理

如果有多套网关（例如本地 Yunqi 与远程部署的 Yunqi），可以通过配置切换：

1. 配置窗口右上角 **Select configuration** 下拉框当前为 `Default`
2. 在下拉框中复制现有配置、重命名或新建配置，每套配置独立保存 base URL、API key 和模型列表
3. 菜单栏 **Developer → Open Developer Config File…** 可直达当前配置对应的 JSON 文件，便于备份和迁移
4. **Export** 按钮可导出 `.mobileconfig`（macOS）或 `.reg`（Windows），供企业 MDM 批量部署

## 常见问题

### 提示 Can't reach 127.0.0.1:8443

Claude Desktop 连不上 HTTPS 包装层：

1. `caddy run` 是否仍在运行
2. Gateway base URL 的端口与 Caddyfile 监听端口是否一致
3. Yunqi 本身是否在运行（`http://localhost:3000` 能否访问管理界面）

### Test connection 返回 401

Gateway API key 与 Yunqi 的 `PROXY_ACCESS_KEY` 不一致。检查 Yunqi 启动配置和配置窗口中填写的密钥。

### 能连通但模型选择器为空

先检查 `/v1/models` 的 `data` 是否包含模型。若列表包含 DeepSeek 等非 Claude 模型，但 Desktop 报 `Gateway returned no usable models`，检查响应是否带有效的 `anthropic_family_tier`。仅有 HTTP 200、`display_name` 或 `supports1m` 不能通过客户端筛选。可使用第四节的显式 tier 映射，或在 **Model list** 手动添加实际模型 ID。

### 能否直接填 `http://localhost:3000`

不行。Claude Desktop 第三方推理要求网关地址为 HTTPS，`http://` 地址无法完成连接。必须按第二节做 HTTPS 包装。若 Yunqi 部署在远程服务器且已有有效 TLS 证书，可直接填 `https://你的域名`，无需 Caddy。

### 切换回官方 Claude

打开 **Developer → Configure Third-Party Inference…**，把 Inference provider 从 `Gateway` 切回官方登录（或直接登录 Claude 账号），即可恢复官方服务。
