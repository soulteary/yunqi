# 路由收益的固定样本评估

路由预演回答“会选谁”，已有 A/B 影子统计回答“请求是否完成”。新评估器用于对比同一固定样本下的基线与候选策略，分别检查人工/独立 grader 质量、真实成本、P95 延迟、重试和成功率。HTTP 200 不会被转换为质量分，缺失价格也不会当成零费用。

评估器不发送上游请求、不运行在线探索、不修改路由或预算，也不自动启用新策略。`eligible_for_manual_review` 只表示上传证据满足当前门槛，仍须人工审查样本、评分方法和生产适用性。

## 准备固定证据

保持 prompts、工具、模型参数、数据集和 grader 不变，分别采集两个策略的实际运行数据。`fixtureSha256` 应是受控样本与运行设置清单的 SHA-256；`baselineVersion` / `candidateVersion` 使用代码或配置的不可变版本，`graderVersion` 固定评分规则。只有同一 case 的两次测量才能配对。不要用两个不同流量时段的均值拼成配对结果。

JSON 顶层为 `version: 1`、上述版本字段、`evidenceSource`（`offline_paired` 或 `shadow_paired`）、`gates` 和 `cases`。每个 case 包含唯一 `id`、场景 `stratum`、`baseline` 和 `candidate`；两个测量对象均有：

- `quality`：0–1 的独立评分；缺失时填 null。
- `costUsd`：该逻辑请求所有尝试的实际合计费用；不完整或缺少价格时填 null。
- `latencyMs`、`retries`、`success`：必填，包含失败请求，不能只留下成功样本。

不接受 prompt、输出、密钥或任意附加字段。每份最多 10,000 对、64 个分层、4 MiB。标识符仅允许短的字母数字及 `._:-`；不要把个人信息放入标识符。证据由导入方负责真实性；评估器验证结构和计算一致性，不会声称验证外部 grader 或供应商账单。

## 执行与长期记录

离线运行：

```sh
cd backend-go
go run ./cmd/routing-eval --input /secure/paired-measurements.json --check > /secure/routing-report.json
```

`--check` 在证据不足或门槛失败时返回非零，报告仍输出具体原因。省略此参数可查看尚未达到门槛的结果。规范化后按 case ID 排序并计算内容摘要，重放同一份证据会得到相同报告 ID，与上传顺序无关。

全局管理员也可使用 `POST /api/autopilot/evaluations` 保存测量集及报告，`GET /api/autopilot/evaluations` 查看最近 100 次评估，`GET /api/autopilot/evaluations/:id/dataset` 导出原始数值证据重放。有限角色和渠道范围管理员不能访问。记录保存在 `autopilot.db`，重启保留，超出保留量淘汰最早上传的评估；需要更长历史应定期导出。存储不可用返回 503，不伪装保存成功。不会改变影子采样、亲和、计数或在线策略。

## 门槛和解释

`gates` 默认 `minPairs=30`、`maxQualityLoss=0`、`maxCostRatio=1`、`maxP95Ratio=1.1`、`maxMeanRetriesIncrease=0`。质量损失容忍最多 0.1；成本/延迟比例允许显式配置但不会自动调整。每个场景分层与总体分别计算，任何分层退步都会阻止总体通过，避免大样本的简单请求掩盖工具/长上下文等场景的退步。

成本给出已知配对金额及节省额；不完整样本保持证据不足。P95 用 nearest-rank，包括失败请求耗时；重试指标为每逻辑请求平均重试次数。质量区间使用配对差值均值减去 2.045 倍标准误（至少 30 对），这是描述性的近似区间，不是因果证明、多重检验保证或生产收益承诺。每层样本少、质量/成本缺失、或质量非劣界尚未建立时均为 `insufficient_evidence`，明确退步为 `rejected`。
