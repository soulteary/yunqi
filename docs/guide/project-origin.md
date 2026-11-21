# 云起 Yunqi：名称、沿革与维护方向

云起（Yunqi）是一项**多模型 API 网关与协议转换服务**。展示标题为“云起 Yunqi — 连接客户端与模型。”，仓库与命令统一使用 `yunqi`。

## 为什么叫云起

名字出自王维《终南别业》：

> 行到水穷处，坐看云起时。

“云起”自然顺口，有云端服务的联想，也寄托了项目经过精简后重新生长的方向。这个名字不依附某一个模型品牌，适合表达连接、转换、汇流与择路的作用。

## 从 Claude to X 到 CCX

本项目记录并尊重上游沿革；当前仓库从快照建立，早期 Git 历史可通过上游仓库查询。以下事实可通过原始提交及文件直接核对：

1. **Claude to X**：早期 README 将其定位为 Claude API 兼容层，将请求转换为 Gemini、OpenAI 等厂商 API，仓库地址为 `glidea/claude-to-x`。参见[早期 README](https://github.com/BenedictKing/ccx/blob/0ed3924b10c947f2354d74c4079eeefe118f5a6f/README.md)。
2. **claude-worker-proxy**：glidea 在 2025 年 8 月 9 日的 [`60ddcad7`](https://github.com/BenedictKing/ccx/commit/60ddcad77cb60429c25d4799397949785ce206c6) 更名提交中，将地址调整为 `glidea/claude-worker-proxy`，通过 Wrangler 部署到 Cloudflare Workers。
3. **claude-proxy → CCX**：BenedictKing 在 2026 年 1 月 31 日的 [`6f0d6bd8`](https://github.com/BenedictKing/ccx/commit/6f0d6bd89f61251c44094123c8154ac6db4ed833) 中，明确将 `claude-proxy` 更名为 `ccx`，当时标题为“Claude / Codex / Gemini API Proxy (CCX)”。

目前没有找到作者对 C、C、X 三个字母的正式展开说明。“Claude、Codex，加上表示扩展或转换的 X”只能视为推测，不能作为官方释义。

Yunqi 的维护仓库为 [soulteary/yunqi](https://github.com/soulteary/yunqi)，上游项目为 [BenedictKing/ccx](https://github.com/BenedictKing/ccx)。感谢原作者与所有贡献者。上游 MIT 许可证及版权声明保留在 `LICENSES/MIT-upstream.txt`；Yunqi 整体的 AGPL-3.0-only 许可及边界见本文末尾。

## 项目解决什么问题

一次典型请求的路径是：

```text
Claude Code / Codex / 其他客户端
  → Yunqi
  → 选择渠道与密钥
  → 转换协议或转发请求
  → 模型上游
```

- **统一接入**：支持 Claude Messages、OpenAI Chat、Codex Responses、Gemini、Images 和 Embeddings 六类 API。
- **协议适配**：衔接客户端协议与上游协议；可用功能取决于具体适配器，不能假定任意协议之间完全等价。
- **渠道与密钥管理**：管理多个服务商、渠道和 API Key，配置模型映射、请求头、代理及路由前缀。
- **调度与容错**：依据模型能力、上下文限制、优先级和健康状态选择渠道，处理故障转移、熔断与恢复。
- **运行治理**：能力测试、健康及余额探测、订阅同步，以及 Autopilot 的诊断、策略和追踪。
- **观测与管理**：请求日志、会话记录、流量指标、成本报表，以及 Web 控制台的配置与诊断。

## 当前仓库与本轮范围

本仓库从上游 `3a08db977187a37ed4236d6a755fb8ab3aebc214` 快照建立，初始提交为 [`02d7a24`](https://github.com/soulteary/yunqi/commit/02d7a24f3b97d229f32872f00aa76a3dd3bb13c1)。初始化阶段完成项目更名、许可调整、暂停继承的发布动作和移除广告赞助。当前已逐项迁入 Web-only 改造、启动装配和前端业务模块拆分；Wails 桌面外壳已经移除，管理入口统一为内嵌 Web 控制台。Gin 已完全替换为 Fiber v3，Go 最低版本为 1.26；代理传输继续保留 net/http 的流式、取消、HTTP/2 与 WebSocket 语义。迁移范围与验证见[迁移记录](migration-ccx.md)。

## 名称与兼容边界

- Go 服务模块为 `github.com/soulteary/yunqi`。服务命令及本地构建输出为 `dist/yunqi`；桌面不再作为构建和发布目标。
- 原有代理与管理 API、认证方式、配置 JSON 和数据库格式保持兼容。默认 `.config` 目录不变；桌面旧数据目录、IPC 标识、`X-CCX-*` 协议头、浏览器认证/偏好键、模型注册表文件名中的旧标识按兼容需要保留。
- 更新检查改为读取 Yunqi 仓库的 Releases，浏览器版本缓存独立命名，避免误指向上游更新。预设更新地址指向 Yunqi 的 Pages 清单。发布或站点尚未就绪时，可以设置 `PRESET_UPDATE_ENABLED=false` 或自定义 `PRESET_UPDATE_URL`。
- 新仓库的发布资源和镜像在首次发布前不保证存在，Compose 默认本地构建。更新已有服务时保留原数据路径，由部署者安排重启。本次更名不会移动数据或停止现有服务。
- 历史 CHANGELOG 保留原始命名与来源；本轮广告清理只删去推广内容，技术变更仍可追溯。

## 许可与代码共享

Yunqi 作为整体改用 **GNU Affero General Public License version 3 only**（`AGPL-3.0-only`），完整正文见根目录 [LICENSE](https://github.com/soulteary/yunqi/blob/main/LICENSE)。选择它的目的，是让分发和经修改后的网络服务遵循相应的源码共享义务；这不等于放弃版权或将代码置于公有领域。网络交互情形按 AGPL 第 13 条提供对应版本源码。

上游代码原有的 MIT 授权不会被撤销，其完整许可与版权声明保留在 [LICENSES/MIT-upstream.txt](https://github.com/soulteary/yunqi/blob/main/LICENSES/MIT-upstream.txt)，来源和许可边界见 [NOTICE](https://github.com/soulteary/yunqi/blob/main/NOTICE)。第三方依赖仍适用各自的许可。

协议来源：[GNU AGPL 3.0 正文](https://www.gnu.org/licenses/agpl-3.0.html)。
