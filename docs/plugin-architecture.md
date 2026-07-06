# 事件、钩子与插件架构

## 0. 目标与约束

本次重构确定引入**事件机制**与**钩子机制**，并以此为基础支持后续**插件式开发**。设计必须同时满足既有硬约束：

- 透明代理额外 P95 < 5 ms → 在途扩展点必须可零开销跳过、可预算约束、可超时。
- 日志/控制平面故障不阻断正常请求 → 旁路扩展点必须异步、非阻塞、可降级。
- 静态单二进制交付（`CGO_ENABLED=0`）→ 首选编译期插件，Go 原生 `plugin` 包不采用（仅 Linux、破坏静态链接、易崩）。
- 一切扩展点可观测、可版本化、可回退到透明代理。

设计基线复用 new-api 现有资产：`relay/channel` 的 `Adaptor` 注册表（事实上的插件雏形）、`RelayInfo` 共享上下文、`relay/common/override.go` 参数改写、`TaskAdaptor` 的计费钩子（`EstimateBilling`/`AdjustBillingOnSubmit`/`AdjustBillingOnComplete`）。本设计把它们统一到同一套扩展框架。

---

## 1. 三种扩展点的分工

```text
                 同步·在途·可改可拒                 异步·旁路·只观测
客户端 ──▶ [Hook: on_request_start] ─▶ 解析
        ─▶ [Hook: after_parse]      ─▶ 路由
        ─▶ [Hook: before_route]     ─▶ 建连/上游（每次 attempt）
        ─▶ [Hook: before_upstream]  ─▶ 上游
        ─▶ [Hook: after_upstream]   ─▶ 响应处理/OSS
        ─▶ [Hook: before_response]  ─▶ 写回客户端
        ─▶ [Hook: on_request_end / on_error]
                     │
                     └────────── 发布 Event ──────────▶ [事件总线] ─▶ 审计/指标/Trace/webhook/异步结算
```

| 维度 | Hook（钩子） | Event（事件） |
| --- | --- | --- |
| 时序 | 同步，在请求路径内 | 异步，请求路径外 |
| 能力 | 可读写上下文、可短路拒绝 | 只读快照，不可影响请求 |
| 失败影响 | 受隔离策略约束（可 fail-open/closed） | 永不影响请求成败 |
| 延迟预算 | 计入 5ms 预算，需超时 | 不计入热路径 |
| 典型用途 | 鉴权、参数规则、模型映射、注入头、计费预留 | 审计落库、指标、Trace 导出、webhook、异步结算、告警 |

> 原则：**能异步就别放钩子。** 只有必须影响本次请求结果的逻辑才用 Hook；观测与派生动作一律走 Event。

---

## 2. 钩子机制（Hook）

### 2.1 钩子点

与规则引擎阶段对齐，并补充生命周期与 attempt 级钩子：

```text
on_request_start     请求进入、Trace 初始化后
after_parse          Body/协议解析完成，形成统一上下文
before_route         路由决策前
before_upstream      每次上游 attempt 发起前（重试会多次触发）
after_upstream       每次上游 attempt 返回后
before_response      写回客户端前
on_request_end       请求结束（成功/失败均触发）
on_error             发生错误时
```

### 2.2 钩子契约

```go
type HookStage int

type Decision int
const (
    Continue     Decision = iota // 放行
    Modified                     // 已修改上下文，继续
    Reject                       // 拒绝请求（带状态码与原因）
    ShortCircuit                 // 直接返回自定义响应，不再走上游
)

type Hook interface {
    Name() string
    Version() string
    Stages() []HookStage
    // Handle 必须使用 ctx.Deadline 与注入的 Clock/HTTPClient；禁止裸 time/net。
    Handle(ctx context.Context, rc *RequestContext) (Decision, error)
}
```

### 2.3 执行语义（硬约束）

- **零开销快速通道**：某阶段无已注册钩子时，仅一次 nil/len 判断，不进入调度器。这是达成 5ms 的前提。
- **有序执行**：按 `priority` 稳定排序；顺序来自不可变配置快照。
- **预算与超时**：每个钩子有独立超时，且从请求剩余总预算中扣减（超时预算递减，重试不重置）。
- **panic 隔离**：每个钩子 `recover`；崩溃按其 `failure_policy` 处理，绝不拖垮进程。
- **失败策略（按插件声明）**：
  - `fail_closed`：钩子失败即拒绝请求（用于鉴权、计费预留等关键钩子）。
  - `fail_open`：钩子失败记录并跳过（用于非关键增强）。
- **shadow 语义**：影子钩子只计算差异、**永不改变实际转发字节**（由 property 测试强制，见 §6）。
- **可观测**：每个钩子执行产生独立 span + 指标（耗时、decision、error），慢钩子可在 800 秒 Trace 中被定位。

---

## 3. 事件机制（Event）

### 3.1 事件类型（示例）

```text
RequestCompleted     AttemptCompleted      RuleMatched
TaskStateChanged     ImageProcessed        OSSUploaded
BillingReserved      BillingSettled        SlowRequestDetected
ConfigSnapshotSwapped
```

事件为**不可变值对象**，携带脱敏后的上下文快照（遵循 observability-security 的审计分级）。

### 3.2 总线语义

- **发布非阻塞**：发布者写入有界缓冲（channel/ring buffer）；队列满时按策略**丢弃或降级**，绝不阻塞数据平面（呼应“日志故障不阻断请求”）。
- **投递保证分级**：
  - 观测类事件（指标、审计、Trace）：内存总线，**至多一次**，允许过载丢弃并计数告警。
  - 资金/关键类事件（结算触发、任务终态）：走 **outbox 模式**——先随业务事务落 DB，再由后台投递，**至少一次 + 幂等消费**，保证不丢不重复扣费。
- **订阅者隔离**：每个订阅者独立协程池与背压，一个慢订阅者不拖累其他订阅者与请求。
- **优雅停机**：drain 时先停止接收新请求，再冲刷事件缓冲与 outbox。

---

## 4. 插件机制（Plugin）

### 4.1 分层（按静态单二进制约束选型）

| 层级 | 载体 | 热路径 | 隔离/安全 | 用途 |
| --- | --- | --- | --- | --- |
| L1 编译期插件（首选） | Go 包 + `init()` 注册表 | 零开销 | 同进程，受能力约束 | 上游适配器、规则、图片处理、OSS、内置钩子/订阅者 |
| L2 嵌入式表达式/脚本 | Expr / Starlark | 低开销 | 沙箱、无 IO | 轻量在途规则条件与参数变换（对应规则 DSL 决策） |
| L3 WASM 插件（后续） | wazero / extism | 中等 | 强沙箱、无 CGO、跨平台 | 不可信/第三方在途逻辑 |
| L4 进程外插件（后续） | gRPC（hashicorp go-plugin） | 高（不入热路径） | 进程隔离 | 重型旁路插件、控制平面扩展、异步事件消费 |

> 首版只做 L1（+ 用 L2 承载规则条件）；L3/L4 接口预留，等生态需要再启用。现有 `relay/channel` 的 `Adaptor` 直接映射为 L1 的“上游适配器”插件类型。

### 4.2 插件清单（Manifest）

每个插件声明，纳入不可变配置快照：

```text
name / version
kind:            hook | event_subscriber | upstream_adapter | media | storage | billing
subscribes:      钩子点或事件类型
config_schema:   配置 JSON Schema（发布时校验，坏配置 fail-fast）
capabilities:    声明式权限（见 4.3）
limits:          timeout / memory / concurrency
failure_policy:  fail_open | fail_closed
```

### 4.3 能力权限（capability-based）

插件默认**零权限**，只能使用其显式声明并被授予的能力，防止插件成为 SSRF/数据外泄/开放代理入口：

```text
read_request_meta     读脱敏上下文
mutate_request        修改转发参数（仅 Hook）
read_body / mutate_body
outbound_http         仅通过受控 SSRF-safe 客户端（域名白名单、禁私网/元数据、限重定向）
read_secret(name)     经密钥代理按名获取，不接触原始密钥表
emit_event
persist(namespace)    受限命名空间存储
```

### 4.4 生命周期

```text
Register（init 注册） → ValidateConfig（发布时） → Init（快照切换时）
   → Serve（运行，处理钩子/事件） → Shutdown（drain 冲刷后）
```

插件的启用/禁用/顺序/配置均来自不可变配置快照；**每个请求记录激活插件及其版本**（与规则版本、路由决策一同冻结，支持重算与审计）。

---

## 5. 与既有关注点的衔接

- **延迟预算**：L1 钩子计入 5ms；需要重活的逻辑改用事件（异步）。快速通道零开销由 §2.3 保证。
- **可观测**：钩子/插件执行 = 独立 span + 指标；插件版本进入 Trace，慢插件可归因。
- **失败隔离**：关键插件 `fail_closed`（auth/billing），增强插件 `fail_open`（观测/软规则）；任一插件不可 OOM 或拖垮进程。
- **配置/回退**：一键禁用任意插件即回退到透明代理路径（呼应 deployment“一键关闭请求修改/图片压缩/OSS/智能路由”）。
- **安全**：能力最小化 + 密钥经代理 + 出网经 SSRF-safe 客户端；L3/L4 用于不可信来源。
- **计费**：现有 `EstimateBilling/AdjustBillingOnSubmit/AdjustBillingOnComplete` 重构为 `billing` 类插件挂在对应钩子/事件上，快照冻结不变。

---

## 6. 测试与自动验收（对接 ai-testing-acceptance）

扩展机制本身必须被自动验收，且防止插件绕过防作弊门禁：

- **钩子隔离测试**：用假 `RequestContext` + 假时钟单测每个钩子的四种 Decision 与超时/panic 隔离。
- **快速通道零开销基线**：性能测试断言“无钩子”配置与纯 `ReverseProxy` 的 P95 差值在阈值内。
- **shadow 不变量（property）**：任意输入下 shadow 钩子**不改变实际转发字节**。
- **失败隔离测试**：注入钩子 panic/超时 → 断言按 `failure_policy` 正确 fail-open/closed，进程存活。
- **事件不丢不阻塞**：过载注入 → 断言数据平面不阻塞；资金事件经 outbox **至少一次 + 幂等**，无重复扣费（property 测试）。
- **插件契约测试**：每个插件（含每个上游适配器）必带 golden 契约测试，无契约不合并。
- **能力越权测试**：未声明 `outbound_http` 的插件发起出网 → 断言被拒。
- **确定性**：插件强制使用注入的 Clock/IDGen/HTTPClient；`go vet` 自定义分析器禁止裸 `time.Now()/net`。

---

## 7. 建议目录（并入架构代码结构）

```text
internal/pipeline/        流水线编排与阶段调度
internal/hook/            钩子接口、调度器、预算与隔离
internal/event/           事件总线、订阅者、outbox
internal/plugin/          插件注册表、清单、能力与生命周期
internal/plugin/builtin/  内置插件：rules / media / oss / billing / audit
internal/plugin/wasm/     （后续）WASM 运行时接入
internal/secret/          密钥代理（按名授予，不暴露原始密钥）
```

## 8. 落地顺序

1. Phase -1/0：定义 `RequestContext`、Hook/Event/Plugin 稳定接口 + L1 注册表 + 零开销快速通道 + 契约测试脚手架。
2. Phase 1：把参数规则、模型映射实现为内置 `hook`/`rules` 插件（重构 `override.go`）。
3. Phase 2–5：图片、OSS、路由、任务、计费逐步实现为内置插件，全部走同一钩子/事件框架。
4. 后续：按需启用 L2 表达式、L3 WASM、L4 gRPC，接口保持不变。
