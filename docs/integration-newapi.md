# new-api 整合方案（ADR）

## 1. 现状与问题

当前存在两个独立仓库：

- `new-api`（`D:\mstokens\github\new-api`）：成熟单体 Gin 应用，`module github.com/QuantumNous/new-api`。已内建**用户/令牌/登录(OAuth/2FA/passkey)/充值/订阅会员/兑换码/支付/渠道/倍率计费/React 控制台/relay 适配器**。
- `enhanced-relay`（本仓库）：干净的框架骨架（pipeline/hook/event/plugin + 确定性套件 + AI 验收体系），`module github.com/lzzen/enhanced-relay`，**尚无 new-api 源码**。

问题：既然确定“重构 new-api 且复用其充值/会员/登录体系”，增强能力就必须落在 new-api 代码所在处；本仓库的框架需要与 new-api **合并到同一个模块**里，而不是隔空调用。new-api 是 `package main` 应用，其 `model/controller/service` 等内部包并非按可导入库设计，**无法作为外部依赖干净引入**。

## 2. 原则

- **单一权威，绝不重复造轮子**：用户、令牌、登录、充值、会员、兑换码、支付、渠道、控制台一律复用 new-api 现有实现。
- **计费权威归本网关**：在 new-api 现有 quota/账本体系之上扩展预留/结算/退款，不另起平行账本。
- **增量 Strangler**：relay 数据平面逐步迁移到新流水线，随时可一键回退透明代理。

## 3. new-api ↔ 增强框架 映射

| 关注点 | new-api 现有 | 整合后 |
| --- | --- | --- |
| 客户端鉴权 | `middleware.TokenAuth()` | 直接复用；结果写入 `RequestContext` |
| 渠道/路由选择 | `middleware.Distribute()` + channel 缓存 | 复用；决策写入 `RequestContext` 并冻结快照 |
| relay 入口 | `controller.Relay(c, format)` | 由 `pipeline.Execute` 包裹，按阶段派发钩子 |
| 上游适配 | `relay/channel` `Adaptor` 注册表 | 泛化为 upstream-adapter 插件（接口不变） |
| 参数改写 | `relay/common/override.go` | 重构为内置 `rules` 钩子插件 |
| 计费 | `service/quota.go` 预扣/结算 | 作为 `billing` 插件挂在 before_upstream/after_upstream/事件，权威保留在 quota |
| 共享上下文 | `relaycommon.RelayInfo` | 与 `RequestContext` 桥接（渐进替换） |
| 慢请求分段 | 无 | `RequestContext.Timings` 落各阶段耗时 |
| 控制台 | `web/default`（React/TanStack） | 复用；新增 Trace/规则/计费明细页 |

## 4. 流水线注入点（relay 路径）

```text
/v1/* → CORS/Decompress/Stats
      → TokenAuth            (复用：登录/令牌 → rc.User/Token)
      → ModelRequestRateLimit(复用)
      → Distribute           (复用：渠道/分组/出口 → rc.Route 快照)
      → controller.Relay ──▶ pipeline.Execute(rc):
            after_parse → before_route → before_upstream(每次尝试)
            → after_upstream → before_response
            └─ 异步 Event：审计/指标/Trace/结算触发
```

Ingress/Egress 为同一进程两个阶段；真实上游目标只来自服务端 Channel 配置。

## 5. 分阶段（在 new-api 代码内）

1. **移植地基**：把 `internal/{clock,idgen,reqctx,hook,event,plugin,pipeline}`、`cmd/acceptance`、`cmd/dashboard`、`acceptance/`、`scripts/ci.sh`、`.cursor/rules`、`docs/` 迁入 new-api（或其 fork），`make ci-docker` 在该库跑通。
2. **Phase 0**：在 `controller.Relay` 外包一层 pipeline，只做 Trace 分段与只读观测，零行为改动，可开关。
3. **Phase 1+**：override→rules 插件、图片/OSS、路由增强、异步任务、计费插件，逐条带 `REQ-ID` 与测试。

## 6. 待决策：代码落地形态

见下方问题。三选一后即可开始 Phase 0 编码；在此之前不写与落地形态耦合的代码。

- **A. 在 new-api 的 fork 内开发**（推荐）：把本仓库产物移入 new-api fork，单模块 `github.com/QuantumNous/new-api`，原生复用一切。
- **B. 本仓库吸收 new-api 源码**：把 new-api 源码并入本仓库并统一模块名，等价于 A 但以本仓库为基。
- **C. 保持双仓库、依赖引入**：不推荐——new-api 非库化，成本高、边界脆弱。
