# 技术架构

> 路线已定稿为**重构 new-api**（非旁路新建），并引入事件/钩子/插件机制。见 `docs/review-notes.md` §1 与 `docs/plugin-architecture.md`。

## 1. 拓扑

重构方案下**不存在被环绕的第三方“现有网关”**：Ingress 与 Egress 退化为同一重构进程内的两个流水线阶段。原始请求与实际上游请求的“双观察点”由同一进程的两个钩子阶段采集。

```text
客户端
  │
  ▼
重构网关（单进程流水线）
  ├─ 入口阶段(Ingress)  鉴权、Trace 初始化、原始请求审计、参数规则、参考图处理
  │        │  [Hook: on_request_start / after_parse / before_route]
  │        ▼
  ├─ 路由与出口阶段(Egress)  网络出口、上游适配、实际请求审计
  │        │  [Hook: before_upstream / after_upstream]（每次 attempt）
  │        ▼
  ├─ 真实供应商
  │        ▼
  └─ 响应处理、OSS、结算、写回客户端
           [Hook: before_response / on_request_end]
           └── 异步发布 Event ─▶ 审计/指标/Trace/webhook/结算
```

真实上游目标只能来自服务端 Provider 配置，禁止从客户端 Header 或查询参数读取，以防止开放代理或 SSRF。若未来仍需跨进程分离 Ingress/Egress，可由同一二进制以角色参数启动（见部署文档），但内部跳段必须鉴权并检测代理循环。

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
internal/pipeline/        前后处理流水线与阶段调度
internal/hook/            同步在途钩子：接口、调度、预算与隔离
internal/event/           异步事件总线、订阅者、outbox
internal/plugin/          插件注册表、清单、能力权限与生命周期
internal/plugin/builtin/  内置插件：rules/media/oss/billing/audit
internal/secret/          密钥代理（按名授予，不暴露原始密钥）
internal/rules/           规则模型、编译和执行（作为内置钩子插件）
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
