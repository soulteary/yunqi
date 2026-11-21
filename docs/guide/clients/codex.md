# Codex CLI / Codex App 接入 Yunqi


## 工作方式

```text
Codex CLI / App  ->  Yunqi /v1/responses  ->  Responses 渠道  ->  上游 Responses 或 Chat 端点
```

## 一、配置 Yunqi 渠道

1. 打开 Yunqi 管理界面，进入 **Responses** 入口
2. 点击「添加渠道」
3. 根据上游能力选择服务类型

| 上游能力 | 服务类型 | 说明 |
|----------|----------|------|
| 原生 OpenAI Responses | `Responses` | 直接转发 Responses 协议 |
| OpenAI Chat 兼容 | `OpenAI Chat` | Yunqi 将 Responses 转换为 Chat Completions |
| Claude | `Claude` | Yunqi 将 Responses 转换为 Claude Messages |

如果上游是 Chat 兼容端点，且不支持 `developer` 等非标准 Chat role，建议编辑渠道并启用 **规范化非标准 Chat role**。

## 二、通用配置值

Codex CLI 和 Codex App 都使用下面这组值：

| 配置项 | 值 |
|--------|----|
| API Key | `your-yunqi-proxy-key` |
| Base URL | `http://localhost:3000/v1` |
| Model | 客户端请求模型名，例如 `gpt-5` |

::: tip
这里的 API Key 是 Yunqi 的 `PROXY_ACCESS_KEY`，不是上游提供商的 API Key。上游 API Key 只配置在 Yunqi 渠道里。
:::

## 三、Codex CLI 配置

在终端中设置：

```bash
export OPENAI_API_KEY="your-yunqi-proxy-key"
export OPENAI_BASE_URL="http://localhost:3000/v1"
```

然后运行：

```bash
codex "你好"
```

如果你的 Codex CLI 版本使用配置文件而不是环境变量，仍然填写同一组值：

```text
API Key: your-yunqi-proxy-key
Base URL: http://localhost:3000/v1
Model: gpt-5
```

## 四、Codex App 配置

在 Codex App 的模型或 Provider 设置中，选择 OpenAI 兼容 / 自定义 API 配置，并填写：

| 设置项 | 值 |
|--------|----|
| API Key | `your-yunqi-proxy-key` |
| Base URL | `http://localhost:3000/v1` |
| Model | `gpt-5` 或你的映射模型名 |

## 五、模型映射建议

Codex 默认可能请求 GPT 模型名。如果上游使用不同模型名，建议在 Responses 渠道里配置模型映射：

| 请求模型匹配 | 映射到上游模型示例 |
|--------------|--------------------|
| `gpt` | 上游主力模型 |
| `mini` | 上游轻量模型 |

::: tip
不要只给 `gpt-5` 配高能力模型而忽略 `gpt-5-mini`。Yunqi 按更长匹配键优先匹配，通常可以用 `gpt` 和 `mini` 覆盖常见 Codex 模型名。
:::

## 常见问题

### 如何快速配置自动审批模型？

如果 Codex 使用 `codex-auto-review` 发起审查，但需要交给已有模型处理，可在管理界面的「服务与客户端接入」选择 Codex，点击「Codex 自动审批映射 → 快速配置」。目标默认带入上方已填写的请求模型，核对后点击「启用并保存」；再次打开可查看设置或「停用映射」。

该映射全局作用于此网关的 `codex-auto-review` Responses 请求，仅改写模型名，目标仍走现有渠道、能力检查及故障转移。默认不启用，停用后恢复自动路由；它不会修改 Codex 客户端的审批设置。请先确认目标模型通过正常请求可用。查看配置不会写入，保存需要管理员权限。

管理 API 为 `GET/PUT /api/console/codex-auto-review`，保存请求示例：

```json
{ "enabled": true, "targetModel": "your-working-model" }
```

配置保存在 `config.json` 的 `codexAutoReview` 中，保存后生效，无需重启服务。目标不能是 `codex-auto-review` 自身。

### 渠道设置了更大的窗口，为什么 Codex 显示的上限没有变化？

Yunqi 的渠道能力用于判断实际模型能否承载请求；Codex 使用自己的本地模型窗口设置。修改渠道能力或将 GPT 模型名映射到其他模型，不会自动修改 Codex 的本地配置，也不会扩大上游模型的实际容量。

在「服务与客户端接入」中选择 Codex 后，可填写可选的「Codex 上下文窗口」。导出的 `codex-config.toml` 会在顶层加入 `model_context_window`；留空则沿用客户端默认值。例如，只有确认实际模型及所有备用路由都支持该窗口后，才使用：

```toml
model_context_window = 1000000
```

将这一行合并到 `~/.codex/config.toml` 的顶层，放在 `[model_providers.ccx]` 等表定义之前。导出不会修改本机文件，也不会设置自动压缩阈值。切换模型或备用路由后，应重新核对窗口，不要按客户端请求模型名推断上游容量。

字段说明见 [Codex 官方配置参考](https://developers.openai.com/codex/config-reference/)；是否支持该配置取决于客户端版本。

### Codex CLI 和 Codex App 是否要分开配置？

不需要。两者使用同一套 Yunqi OpenAI 兼容配置：

```text
API Key  = PROXY_ACCESS_KEY
Base URL = http://localhost:3000/v1
Model    = 客户端请求模型名
```

区别只是 CLI 在终端或配置文件里填，App 在图形界面里填。

### 返回 401 Unauthorized

检查：

1. 优先查看当前环境变量是否设置了 `OPENAI_API_KEY`，它可能覆盖你在 Codex 配置里填写的 Key

   ```bash
   printenv OPENAI_API_KEY
   ```

2. `OPENAI_API_KEY` 或 App 中的 API Key 是否等于 Yunqi 的 `PROXY_ACCESS_KEY`
3. 是否误填了上游厂商 API Key
4. Yunqi 服务是否使用了同一个 `PROXY_ACCESS_KEY` 启动

### 返回 404 或接口不存在

检查 Base URL。

正确：

```bash
export OPENAI_BASE_URL="http://localhost:3000/v1"
```

不要填写：

```bash
export OPENAI_BASE_URL="http://localhost:3000"
export OPENAI_BASE_URL="http://localhost:3000/v1/responses"
```

### 返回 model_not_found

检查 Responses 渠道：

1. 模型白名单是否包含请求模型或映射后的上游模型
2. 模型映射是否覆盖 Codex 实际请求的模型名
3. 上游模型名是否填写为该厂商真实支持的名称

### 上游报 role 不支持

如果错误中出现 `developer`、`tool`、`system` 等 role 相关提示，编辑 Responses 渠道并启用 **规范化非标准 Chat role**。这对 DeepSeek 等 Chat 兼容上游尤其常见。

### 流式输出中断

检查：

1. 上游是否稳定支持流式响应
2. 代理或反向代理是否设置了过短超时
3. 当前渠道是否已被熔断或频繁 failover
4. 客户端是否请求了上游不支持的工具调用或响应格式

### 想确认请求是否走 Responses 入口

查看 Yunqi 后端日志或渠道日志，请求路径应为：

```text
/v1/responses
```

如果你看到 `/v1/chat/completions`，说明客户端当前没有走 Codex Responses 配置，而是走了 OpenAI Chat 配置。
