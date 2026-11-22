# 模型目录治理

模型目录治理把“上游发现了哪些模型”和“允许客户端使用哪些模型”分开：发现任务保留原始模型 ID 和探测证据，管理员预览差异后，再一次性启用审核过的目录。可以为客户端提供统一名称，同时保持供应商实际接收的模型 ID 不变。

本功能适用于 `messages`、`chat`、`responses`、`gemini`、`images`、`vectors` 渠道。不需要新增环境变量，策略及审核结果随运行时配置持久化。完整改进批次、提交及上游参考见[社区功能改进与修正记录](community-improvements.md)。

## 推荐操作流程

在渠道页面展开“模型同步与审核”面板，选择渠道后加载已保存状态。这个面板独立于普通渠道编辑表单，保存策略不会顺带提交另一张表单中尚未保存的地址、密钥或其他草稿。

1. 保持目录执行约束未启用，先保存筛选和名称规则。
2. 手工填写上游原始模型 ID 后预览，或明确执行重新发现。手工预览不会访问供应商；重新发现会执行现有发现流程，可能产生真实探测请求。
3. 检查新增、删除、变更，以及“客户端名称 → 上游原始 ID”的对应关系。删除模型和空目录需要分别确认。
4. 批准当前审核记录。批准会同时启用目录约束，并将渠道模型规则替换为本次审核中的原始 ID。
5. 渠道地址、凭据、策略或其他源配置发生变更后，重新加载并预览。不要重复批准旧记录。

空的已批准目录会拒绝所有模型，不会退回旧模式中“空规则表示不限模型”的行为。因此，首次配置通常应使用未启用的策略草稿，完成审核后再原子启用。

## 草稿、执行约束与退出治理

“保存了治理策略”和“启用目录执行约束”是两个状态：

- **没有 `modelImport` 配置**：保留原有发现、自动模型白名单回写、指纹学习及已发现协议自动启用行为。
- **保存了策略，`enabled: false`**：使用已有模型规则处理线上请求，但发现任务只生成待审目录；暂停自动回写模型白名单、学习指纹及自动启用新协议。未启用的草稿也属于这一状态。
- **批准审核记录**：原子启用策略和审核目录。之后只有目录内的公共名称或原始 ID 可以直接使用；任务模型及自适应映射的最终实际模型也必须在目录中。
- **显式关闭执行约束**：不再限制请求必须属于审核目录，但仍保留治理工作流，发现继续只生成待审记录。
- **移除策略**：保留当前 `SupportedModels` 规则，恢复原有自动行为。若当前模型规则为空，界面和 API 都要求单独确认，因为这会恢复旧模式下不限模型的准入。

移除策略不会撤销已执行的供应商请求，也不会将曾经删除的模型自动补回。需要恢复其他模型时，应重新发现并审核，或按原有渠道配置方式明确编辑模型规则。

## 名称与筛选规则

筛选首先作用于上游原始 ID：命中任一纳入规则后保留，未配置纳入规则时全部参与；排除规则随后执行，排除优先。正则采用 Go RE2 语法，不支持前后向断言等扩展。

对保留的 ID 按顺序执行：去除指定前缀、去除指定后缀、转换大小写、依次执行正则替换。只修改客户端名称，不修改供应商实际模型 ID。

例如上游返回 `vendor/GPT-fast-latest`，策略为：

```json
{
  "enabled": false,
  "include": ["^vendor/GPT-"],
  "exclude": ["-blocked$"],
  "stripPrefix": "vendor/",
  "stripSuffix": "-latest",
  "case": "lower",
  "rules": [
    { "pattern": "^gpt-(.*)$", "replacement": "public-$1" }
  ]
}
```

审核目录中的公共名称是 `public-fast`，上游请求仍使用 `vendor/GPT-fast-latest`。名称碰撞、公共名称遮蔽另一原始 ID、无效路径片段或超出限制都会拒绝本次预览，不会静默覆盖模型。

当前上限为每组纳入/排除各 32 条规则、16 条有序替换、每个名称或规则字段 256 字节；一次最多接收 8,192 个候选，最终目录最多 2,048 个模型。`case` 可选空字符串、`lower`、`upper`。

## API 操作

管理 API 使用管理身份，不能用管理凭据调用推理接口。读取需要 `channels.read`，策略、预览、批准和移除需要 `channels.model-import` 权限；还会校验请求 UID 的渠道范围。

以下地址支持 `?protocol=chat` 等协议参数。物理渠道 UID 可以直接定位；逻辑渠道 UID 必须能在所选协议下唯一对应一个物理渠道，否则返回错误，不会任选一个修改。

### 读取状态

```http
GET /api/channels/:uid/model-import?protocol=chat
```

返回 `channelUid`、`revision`、`configured`、`state`、`unrestrictedOnRemove`，同时返回 `ETag`，不包含供应商地址和密钥。`configured` 表示该渠道是否已经持久化治理策略；为 `false` 时须先保存草稿，再从审核面板启动发现。`state` 包含策略、已批准目录、待审记录及有界审核历史。`unrestrictedOnRemove` 表示移除策略是否会恢复不限模型的旧行为。

### 保存完整策略

```http
PUT /api/channels/:uid/model-import/policy?protocol=chat
If-Match: <读取到的 revision>
Content-Type: application/json
```

请求体使用前面的策略格式。必须明确提供布尔类型的 `enabled`；缺失、`null` 和未知字段均被拒绝。保存策略会使之前的待审记录失效，随后应使用新响应中的版本预览。

### 手工预览

```http
POST /api/channels/:uid/model-import/preview?protocol=chat
If-Match: <最新 revision>
Content-Type: application/json

{"models":[{"id":"vendor/GPT-fast-latest"}]}
```

返回审核记录的 `id`、`baseRevision`、`observedAt`、`source`、`models`、`diff` 及可选的旧模型规则。`diff` 分为 `added`、`removed`、`changed`；名称不变但原始 ID 或端点证据改变也会显示为变更。

手工输入即使携带 `source: "probe"` 或 `verified: true`，也会被降级为操作员声明，不能伪造探测证据。

### 批准审核

```http
POST /api/channels/:uid/model-import/approve?protocol=chat
Content-Type: application/json

{"reviewId":"<待审记录 id>","allowEmpty":false,"allowRemovals":false}
```

批准绑定具体审核 ID 和该记录的源版本，不需要再提供 `If-Match`。仅在确实审核过空目录或删除差异时，将对应确认字段设为 `true`。新的预览会生成新的审核 ID，不能沿用旧 ID。

### 移除策略

```http
DELETE /api/channels/:uid/model-import?protocol=chat
If-Match: <最新 revision>
Content-Type: application/json

{"allowUnrestricted":false}
```

当前模型规则为空时，必须明确提供 `allowUnrestricted: true`。移除仅删除治理状态，保留当前模型规则并恢复旧自动流程。

### 错误处理

- `400`：策略、模型名称、确认项或输入格式不合法；也可能是逻辑渠道选择不唯一。
- `403`：管理角色或渠道范围不足。
- `404`：渠道或所选协议不存在。
- `409`：读取、发现或审核依据的配置已过期。重新加载最新状态并重新预览，不要只替换旧请求的版本号后重发。
- `500`：持久化失败。失败操作会回滚运行时变更，不应将界面草稿当作已保存结果。

## 执行与能力证据边界

公共别名会在调度和执行时还原为审核过的原始 ID。精确审核别名优先于模型家族映射；虚拟任务模型或协议联邦映射只能执行目录内的实际模型。最终执行层仍会再次校验，防止调度后映射绕过目录。

同一原始 ID 用于请求预览、报价及实际请求构造。别名不能借用另一个同名模型更便宜的价格或更大的上下文窗口；缺少原始模型能力或价格时仍按未知处理。图片 multipart 改写保留文件内容，Gemini 使用原始模型 URI，向量请求使用原始模型字段。

`/v1/models` 中的 `supported_endpoint_types` 仅列出具有已审核成功探测证据的端点类型；`endpoint_metadata` 单独标记端点来源和 `verified`。供应商声明、配置推断、手工输入都不等于成功探测。目录改变会使旧发现缓存失效，上游暂时不可用时不能通过旧缓存重新暴露未经审核的模型。

目录审核不是供应商协议适配器。一个模型声明支持某种端点，不代表网关新增了该供应商的专用传输实现，也不代表所有 Key 都有该模型权限。原有 Key/endpoint 原始库存与准入保护仍然生效。

分享导出只携带策略，不转移已批准目录、待审记录和审核证据。新增渠道和分享导入会拒绝伪造的服务端审核状态；普通渠道编辑即使携带旧快照，也不能替换或清空服务端目录。可信的完整状态备份与分享包用途不同，仍按现有状态备份流程处理。

## 参考与验证

本轮参考上游模型同步白名单 [PR #7953](https://github.com/QuantumNous/new-api/pull/7953) / [issue #7951](https://github.com/QuantumNous/new-api/issues/7951)，名称规范化 [PR #7955](https://github.com/QuantumNous/new-api/pull/7955) / [issue #7952](https://github.com/QuantumNous/new-api/issues/7952)，以及端点元数据 [PR #8296](https://github.com/QuantumNous/new-api/pull/8296) / [issue #8295](https://github.com/QuantumNous/new-api/issues/8295)、[PR #8233](https://github.com/QuantumNous/new-api/pull/8233) / [issue #8232](https://github.com/QuantumNous/new-api/issues/8232)。这些引用说明设计依据，不表示本项目合并了上游代码或关闭了上游问题。

回归覆盖原始 ID 发送、旧目录缓存失效、协议联邦最终模型准入、别名价格隔离、图片文件保真、Gemini URI、向量构造、权限范围、过期版本、持久化失败及界面切换竞态。各批次提交与完整 CI 结果以[改进记录](community-improvements.md)为准。
