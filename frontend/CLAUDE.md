# frontend 模块文档

[← 根目录](../CLAUDE.md)

## 模块职责

Vue 3 + Vuetify 4 Web 管理界面：渠道配置、能力测试、模型列表查询、拖拽排序、监控面板、主题切换。

## 启动命令

```bash
bun run dev       # 开发服务器
bun run build     # 生产构建
bun run preview   # 预览构建
```

## 核心组件

| 组件 | 职责 |
|------|------|
| `App.vue` | 根组件，认证、布局、主题、能力测试入口 |
| `ChannelOrchestration.vue` | 渠道编排主界面 |
| `AddChannelModal.vue` / `EditChannelModal.vue` | 添加与编辑渠道对话框 |
| `CapabilityTestDialog.vue` | 渠道能力测试结果对话框 |

## API 服务

`src/services/api.ts` 是兼容门面，保留 `api` 单例、默认导出和 `new ApiService()`。页面仍可沿用现有导入，不需要随内部文件移动修改调用方式。

- `src/services/api/client.ts`：统一请求处理，动态读取认证与语言，处理响应和 `ApiError`。
- `src/services/api/channels/`：六类协议渠道的 CRUD、密钥、排序和状态管理。
- `src/services/api/channel-management.ts`：稳定 UID 的 v2 与逻辑渠道操作。
- 其他业务模块：能力测试、监控、健康、会话、订阅、账号、路由治理、成本和系统设置。
- `src/services/types/`：按业务保存 DTO；共享协议枚举位于 `common.ts`，跨模块引用使用 `import type`。
- `src/services/api-types.ts`、`console-types.ts`：保留旧类型导入路径；运行时常量只在所属模块定义一次。

新增请求放到对应业务模块，统一经过 `ApiClient`。不要在业务模块重复读取密钥、实现响应解析或修改错误语义。门面的类型由同一份组合函数推导，新增模块只维护一次组合清单。

`api-contract.test.ts` 固定六类渠道请求、历史默认值、可选字段、鉴权和错误解析行为；`__fixtures__/api-methods.json` 记录兼容的公共方法清单。新增或明确变更公共 API 时同步更新契约。

## 根组件控制器

`src/composables/useAppController.ts` 保留 `App.vue` 使用的返回字段，组合 `composables/app/` 中的职责模块；渠道操作、熔断器表单和对话框快捷键仍在原入口。

- `useAppAuth.ts`：保存密钥的自动认证、手动登录、失败锁定、注销，以及认证状态驱动的渠道刷新和连接状态。
- `useAppNavigation.ts`：导航选项、语言、标题、新用户指引和渠道类型切换后的刷新；路由到渠道类型的同步仍由 ChannelStore 管理。
- `useAppTheme.ts`：复古主题初始化、明暗模式和系统主题监听，复用原 `useTheme.ts` 的字体设置。
- `useAppVersion.ts`：启动时版本检查、并发检查保护、更新对话框和 `yunqi-check-version` 事件。

每个模块负责注册和清理自身监听器；入口按主题、版本、导航、认证顺序组合，保持启动时先应用主题、发起版本检查，再自动认证和刷新渠道。认证模块通过参数接收翻译与提示函数，不回调根控制器。

`useAppController.test.ts` 使用真实 Pinia Store 和内存路由验证公开字段、认证与锁定、导航刷新、指引、主题、版本事件及卸载清理。请求和版本服务在测试中替换，不访问真实上游。

## 主题配置

编辑 `src/plugins/vuetify.ts` 中的 `lightTheme` 和 `darkTheme`。

## Vuetify 组件系统

项目使用 **按需导入** 方案，手动注册使用的 Vuetify 组件和指令，显著减小打包体积（首屏 JS 减少约 60%）。

**配置文件**: `src/plugins/vuetify.ts`

**新增组件步骤**:
1. 从 `vuetify/components/V<Component>` 添加导入
2. 在 `createVuetify({ components: { ... } })` 中注册

```typescript
// 1. 导入
import { VBadge } from 'vuetify/components/VBadge'

// 2. 注册
export default createVuetify({
  components: {
    // ... 现有组件
    VBadge,  // 新增
  },
  // ...
})
```

**常见组件路径**:
- 布局: `VApp`, `VMain`, `VContainer`, `VRow`, `VCol` 等
- 表单: `VTextField`, `VSelect`, `VSwitch`, `VBtn` 等
- 反馈: `VAlert`, `VSnackbar`, `VDialog`, `VTooltip` 等

**注意**: 如果模板中使用了未注册的组件，运行时会报 `Unknown custom element` 错误。

## 图标系统

项目使用 **SVG 按需导入** 方案，从 `@mdi/js` 导入单个图标 path，而非完整字体文件，显著减小打包体积。

**配置文件**: `src/plugins/vuetify.ts`

### 重要规则（必须遵守）

- 在模板里写了 `<v-icon>mdi-xxx</v-icon>` **并不代表可直接使用**
- **每新增一个图标，必须同时完成两步**，缺一不可：
  1. 从 `@mdi/js` 添加导入（驼峰命名）
  2. 在 `iconMap` 中添加映射（kebab-case）
- 只写模板、不补 `iconMap`，运行时会走到 `src/plugins/vuetify.ts` 的未找到图标分支，开发环境会报警告，界面可能显示占位文本而不是图标
- 修改前端图标相关代码时，应顺手检查新增的 `mdi-xxx` 是否已在 `iconMap` 中注册

**新增图标步骤**:
1. 从 `@mdi/js` 添加导入（驼峰命名）
2. 在 `iconMap` 中添加映射（kebab-case）

```typescript
// 1. 导入
import { mdiNewIcon } from '@mdi/js'

// 2. 映射
const iconMap = {
  'new-icon': mdiNewIcon,
}
```

**使用方式**: 模板中使用 `mdi-xxx` 格式
```vue
<v-icon>mdi-new-icon</v-icon>
```

**图标查找**: https://pictogrammers.com/library/mdi/

## 依赖与安全检查

前端只维护 Bun 依赖流程，`bun.lock` 是唯一前端锁文件。不要为了运行 `npm audit` 重新生成 `package-lock.json`，否则会引入重复锁文件并可能产生误报。

```bash
bun install                                      # 安装依赖并触发 Socket 安全扫描器
bun audit --registry=https://registry.npmjs.org # 使用 npm 官方 registry 执行漏洞审计
```

说明：如果本地 registry 指向 `npmmirror`，直接运行 `bun audit` 可能因镜像源不支持 audit 接口而返回 404；审计时显式指定 npm 官方 registry。

新增或升级依赖使用 `bun add` / `bun update`。文档站的 npm 锁文件属于独立依赖树，不与前端混用。

## 构建产物

生产构建输出到 `dist/`，会被嵌入到 Go 后端二进制文件中（`embed.FS`）。
