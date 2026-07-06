# 技术架构

## 1. 推荐拓扑

最终推荐同时部署 Ingress 和 Egress 两个逻辑入口，可由同一套 Go 程序以不同路由承载：

```text
客户端
  │
  ▼
Enhanced Gateway Ingress
  │  原始请求审计、规则、参考图处理
  ▼
现有网关
  │
  ▼
Enhanced Gateway Egress
  │  实际请求审计、网络出口、上游适配
  ▼
真实供应商
  │
  ▼
响应处理、OSS、结算、客户端
```

现有网关的渠道 Base URL 指向固定的内部 Egress 路由，例如：

```text
https://gateway.internal/egress/provider-17
```

真实目标只能来自服务端 Provider 配置，禁止从客户端 Header 或查询参数读取，以防止形成开放代理或 SSRF。

## 2. 核心组件

### 2.1 API Ingress

- OpenAI 风格同步接口。
- 对外异步任务接口。
- 鉴权、限流、Body 上限和 Trace 初始化。
- 客户端断开检测和取消传播。

### 2.2 请求标准化层

- 识别 JSON、form 和 multipart。
- 生成统一 Request Context，但保留原始协议语义。
- 未命中任何规则时走快速透传路径。

### 2.3 规则引擎

处理阶段：

```text
after_parse
before_route
before_upstream
after_upstream
before_response
```

规则在发布时完成解析、校验和编译；运行时读取内存中的不可变快照。每次请求保存规则版本。

### 2.4 媒体处理器

- 图片元信息探测应尽量避免完整解码。
- 完整解码和重编码进入有界 worker pool。
- 设置全局、租户和请求三级并发限制。
- 记录处理前后格式、尺寸、字节数和耗时。

### 2.5 路由引擎

层次结构：

```text
逻辑模型 → 渠道组 → 渠道 → API Key / 网络出口
```

路由评分综合静态优先级、权重、健康度、延迟、并发、地域、能力和成本。路由决策形成不可变快照。

### 2.6 上游适配器

统一接口覆盖：

- 同步 JSON；
- multipart 图片编辑；
- SSE；
- Base64 图片响应；
- URL 图片响应；
- 异步任务创建、查询、取消；
- webhook。

适配器负责协议转换，不负责业务计费。

### 2.7 任务引擎

状态机：

```text
created → queued → submitting → submitted → processing
        → uploading → succeeded
                    ↘ failed / cancelled / expired
```

状态迁移必须使用乐观锁或等价并发控制。最终结算使用唯一幂等键。

### 2.8 计费引擎

```text
请求估价 → 额度预留 → 上游执行 → 实际结果结算 → 退款/补扣
```

计费快照至少包含：规则版本、表达式、模型、渠道、分组倍率、请求关键参数、预估金额和路由决策。

### 2.9 可观测性

- OpenTelemetry Trace：跨 Ingress、现有网关、Egress、上游和 OSS。
- Prometheus Metrics：延迟、吞吐、错误率、重试、队列、worker、连接池。
- 结构化事件：用于 trace 详情和请求差异展示。

## 3. 建议代码结构

```text
cmd/gateway/              程序入口
internal/api/             HTTP 路由与协议入口
internal/pipeline/        前后处理流水线
internal/rules/           规则模型、编译和执行
internal/media/           图片探测、压缩和转换
internal/router/          渠道组、评分、熔断
internal/upstream/        同步/异步供应商适配器
internal/task/            异步任务状态机
internal/billing/         估价、预留和结算
internal/storage/         DB、Redis、OSS
internal/observability/   Trace、Metric、Audit
internal/config/          配置快照与热更新
web/                      管理控制台
tests/mockprovider/       上游模拟器
tests/integration/        集成测试
testdata/                 协议快照与固定样本
```

## 4. 数据存储

- PostgreSQL/MySQL：规则、渠道、任务、账本、审计索引。
- Redis：配置快照通知、限流、锁和短期状态。
- OSS/S3：图片和受控调试快照。
- Prometheus：指标。
- Loki 或 ClickHouse：规模扩大后的结构化事件检索。

生产环境不建议使用 SQLite。

## 5. 性能约束

- 没有规则和内容处理时不完整解析 Body。
- JSON、form、multipart 在单次请求中最多完整解析一次。
- HTTP Client 和 Transport 全局复用。
- SSE 边读边写并及时 Flush。
- 日志异步批量写入，队列满时按策略降级。
- OSS 流式上传，避免形成第二份完整图片缓冲。
- 图片 worker 与普通代理请求资源隔离。
- 超时使用递减总预算，重试不得重新获得完整预算。

## 6. 关键边界

计费所有权必须在正式开发前确定。若现有系统仍是余额和账本权威，增强网关需要稳定的预留、结算和退款接口；若无法获得该接口，则复杂后置计费不能保证强一致，应将增强网关逐步提升为计费权威，现有系统仅承担渠道能力。
