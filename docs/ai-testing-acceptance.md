# AI 自动化测试与验收体系

## 0. 本文目标

`testing.md` 定义了“**测什么**”。本文定义“**谁来跑、如何自动判定通过、如何防止 AI 写出能骗过验收的假测试**”。

核心原则：

> **人类只定义一次意图（可执行规格、Golden 样本、阈值），AI 负责实现并由自动化流水线持续验收；人类只审阅 PR 证据与例外，永不手工点检。**

判定一个功能“完成”，不依赖任何人手动跑一遍，而依赖机器可判定的证据链。凡是需要人肉复现、人肉观察、人肉比对的验收，一律视为体系缺陷。

---

## 1. 五大支柱

```text
1. 可执行规格 + 需求可追溯      每条需求 → 机器可判定的验收用例
2. 确定性密封测试台            一条命令拉起全套依赖，注入时钟/种子/UUID，可无人重复
3. 分层自动门禁                单元 → 竞态 → 集成 → 契约 → 故障注入 → 性能 → 验收
4. 防作弊控制                  变异测试 / 不变量 / Golden 人类所有权 / 断言弱化检测
5. AI 验收闭环 + 机器可读证据   红→绿→全量→自评→报告，PR 携带证据产物
```

---

## 2. 支柱一：可执行规格与需求可追溯矩阵

### 2.1 需求编号

`requirements.md` 的每条需求分配稳定 ID（如 `REQ-TRACE-001`、`REQ-BILL-004`、`REQ-IMG-EXIF-002`）。每条 `P0`/`P1` 需求必须至少绑定一个自动化验收用例，否则视为**未完成**，CI 直接失败。

### 2.2 追溯矩阵（机器维护，非手工表格）

在测试代码中用标签把用例与需求绑定，构建期自动生成矩阵：

```go
// tests/acceptance/billing_test.go
func TestBilling_NoDoubleCharge_OnUpstreamAmbiguousFailure(t *testing.T) {
    req.Covers(t, "REQ-BILL-004", "REQ-TASK-IDEMPOTENT-002") // 机器可解析
    // given / when / then ...
}
```

CI 步骤 `verify-traceability`：

- 扫描所有 `req.Covers(...)`，生成 `build/traceability.json`（REQ → 用例列表 → 最近结果）。
- 任一 `P0`/`P1` 需求无绑定用例，或绑定用例最近为失败/跳过 → **门禁失败**。
- 产物随 PR 上传，人类只看这份矩阵即可判断需求闭环情况。

### 2.3 验收即规格（Given/When/Then）

高价值链路用 Gherkin 风格但落地为 Go 测试（保持单语言、可 `go test` 一键跑，避免额外运行时）。人类拥有的是场景描述与阈值，AI 负责把步骤实现为确定性代码。

---

## 3. 支柱二：确定性、密封（hermetic）测试台

AI 无人值守的前提是**任何机器上一条命令得到完全一致的结果**。

### 3.1 单一入口

```bash
make verify          # 全量：AI 与 CI 使用完全相同的入口
make verify-fast     # 快速子集：单元 + 契约（AI 内循环用）
make up / make down  # 拉起 / 销毁密封依赖
```

`make up` 通过 docker-compose 或 testcontainers 拉起：`gateway + mock-provider + PostgreSQL + Redis + MinIO + Toxiproxy`，全部固定镜像 digest、随机高端口、测试结束自动清理。

### 3.2 消除不确定性（Determinism Kit）

强制注入，禁止在业务代码直接调用 `time.Now()` / `rand` / `uuid.New()` / `net` 裸调用：

- **时钟**：统一 `Clock` 接口，测试注入可控/可推进的假时钟。故障注入靠推进时钟而非真实 sleep（呼应 `testing.md`“禁止真的等待数百秒”）。
- **随机**：全局 RNG 由种子控制，测试固定种子。
- **ID**：`IDGenerator` 接口，测试产出可预测 trace_id / task_id。
- **网络**：测试内禁止访问公网，只允许测试台内地址；用 lint/防火墙钩子强制。

> 落地建议：新增 `internal/clock`、`internal/idgen`、`internal/testutil`（harness、假时钟、种子、golden 助手）。业务代码通过依赖注入拿到这些接口。可用 `go vet` 自定义分析器禁止直接调用 `time.Now()` 等。

### 3.3 Mock Provider 即契约断言器

沿用 `testing.md` 的能力，并强化其作为**双向断言**的角色：既模拟上游各种正常/异常行为，又断言网关实际发出的 URL、Headers、参数、图片尺寸与字节数，并把这些记录为可比对产物。

---

## 4. 支柱三：分层自动门禁（扩展 testing.md §3）

在原 CI 门禁基础上补三道，专为“自动判定 + 防作弊”：

```text
格式与静态检查
→ 单元测试（表驱动 + property-based）
→ Race 检测
→ 集成测试（密封测试台）
→ 契约 / 协议 Golden
→ 故障注入（Toxiproxy + 假时钟）
→ 需求可追溯门禁            [新增] traceability.json 全绑定且全绿
→ 变异测试门禁              [新增] 核心包 mutation score ≥ 阈值
→ 断言弱化检测门禁          [新增] 测试改动的“变松”需显式标注
→ 性能 / 内存 / 泄漏门禁     [强化] 阈值失败即红，非人工观察
→ 验收报告产出             [新增] build/acceptance-report.json
→ 二进制 / Docker 构建 → 漏洞 / SBOM 扫描
```

两种部署形态（单二进制、Docker）必须运行**同一套验收契约测试**（呼应 review-checklist）。

---

## 5. 支柱四：防作弊控制（AI 既写代码又写测试时的关键）

当同一个 AI 同时产出实现与测试，最大风险是**测试写得松、或被改绿以“通过”**。以下为强制护栏：

### 5.1 变异测试（Mutation Testing）——证明测试真的能抓 bug

对核心包（`rules`、`router`、`billing`、`task`、`media`）运行 `gremlins` 或 `go-mutesting`：随机注入变异（改比较符、删语句、改边界），若测试仍全绿说明测试无效。设分数下限（如核心包 ≥ 75%，计费包 ≥ 90%），低于阈值门禁失败。这是防止“覆盖率高但断言空”的最有效手段。

### 5.2 Property-based / 不变量测试——AI 难以针对性造假

对存在数学/业务不变量的模块，用 `testing/quick` 或 `rapid` 做随机输入的属性测试：

- 计费：任意合法请求序列下 **绝不重复扣费**；`预留 - 结算 - 退款` 守恒；金额为非负整数最小货币单位。
- 幂等：同一幂等键任意次数调用 → 至多一个上游任务、至多一次结算。
- 图片：处理后像素/字节 ≤ 限制；默认不放大；透明通道不被静默丢弃。
- 规则：`shadow` 模式**永不**改变实际转发字节。

不变量由人类定义、AI 无法通过“背样例”绕过。

### 5.3 Golden 文件人类所有权

`testdata/*.golden`（协议快照、账单明细）视为**人类拥有的规格**。CI 规则：

- 更新 golden 需专用命令 `make update-golden` 且在 PR 中单独成 commit、附差异说明标签 `golden-change`。
- 缺少标签而 golden 变更 → 门禁失败，强制人类显式审核协议/计费语义变化。

### 5.4 断言弱化检测

CI 对测试文件 diff 做静态检查，识别“断言变松”信号（删除 `require`/`assert`、`t.Skip`、放宽期望值、`if err != nil { return }` 吞错、去掉 `Covers`）。命中则要求 PR 打 `test-weakening-justified` 标签并在报告中说明理由，否则门禁失败。

### 5.5 职责分离（推荐）

对 `P0` 核心逻辑，采用**双 Agent**：实现 Agent 与对抗测试 Agent 分离。对抗 Agent 只读需求与 Golden，独立编写“想方设法证伪实现”的测试，不看实现细节。降低“自证清白”的偏差。

---

## 6. 支柱五：AI 验收闭环

### 6.1 单个变更的机器化流程（AI 必须遵循）

```text
1. 读取目标 REQ-ID 与验收规格 / Golden / 阈值
2. 先写失败用例并绑定 req.Covers(REQ-ID)     （red，证明用例有效）
3. 实现功能                                   （green）
4. 运行 make verify（全量、密封、确定性）
5. 自评清单（见 6.2）逐项机器校验
6. 变异测试 + 覆盖 + 性能 + 内存 + 泄漏门禁
7. 产出 build/acceptance-report.json（机器可读证据）
8. 开 PR：标题 + 证据产物 + 追溯矩阵差异；人类只审证据与例外
```

### 6.2 机器可判定的“完成定义”（DoD）

每项都由脚本判定，不靠人工确认：

- [ ] 绑定的 `P0`/`P1` REQ 全部有绿色用例
- [ ] `go test ./... -race` 全绿，无 `Skip`（除已登记 quarantine）
- [ ] 核心包变异分数 ≥ 阈值
- [ ] Golden 无未标注变更
- [ ] 性能基线未回退超过阈值（P50/P95/P99、每请求分配、峰值内存）
- [ ] 无新增 goroutine 泄漏（`goleak`）
- [ ] 若涉及计费：账本断言精确到最小货币单位且幂等
- [ ] 若涉及新协议适配器：契约测试存在且通过
- [ ] 若为事故修复：附“修复前失败、修复后通过”的回归用例

### 6.3 验收报告产物（示例结构）

```json
{
  "commit": "…",
  "requirements": { "covered": ["REQ-BILL-004"], "missing": [] },
  "tests": { "passed": 812, "failed": 0, "skipped": 0, "quarantined": 1 },
  "coverage": { "core_pkgs": 0.91 },
  "mutation": { "billing": 0.93, "router": 0.81, "threshold_ok": true },
  "performance": {
    "transparent_proxy_p95_ms": 3.7, "budget_ms": 5.0, "ok": true,
    "img100_peak_mem_mb": 780, "budget_mb": 1024, "ok_mem": true
  },
  "golden": { "changed": false },
  "incident_regression": null
}
```

人类审阅这份 JSON（或其渲染）即可决定合并，无需手工复现任何链路。

---

## 7. 非功能需求的自动验证（把“观察”变“断言”）

呼应 review-checklist 的验证清单，全部落为会让 CI 变红的测试：

| 验证问题 | 自动化断言 |
| --- | --- |
| 一条 Trace 能解释 800 秒请求各段耗时 | 故障注入慢上游 → 断言各 `*_ms` 分段非空且求和≈总耗时 |
| 日志系统全挂业务是否可用 | 注入日志后端故障 → 断言请求成功、透明代理不阻断 |
| 100 大图并发是否 OOM | 峰值内存断言 ≤ 阈值（worker pool 有界生效） |
| 客户端断开是否终止可取消工作 | 断开客户端 → 断言上游请求/OSS/图片等待均收到 cancel |
| 上游已受理但响应丢失是否重复生图/扣费 | mock 丢响应 → 断言不重试跨渠道、不重复扣费 |
| 历史费用能否用冻结规则重算 | 用快照重放 → 断言金额逐分一致 |
| 规则错误能否秒回透明代理 | 注入坏规则 → 断言自动降级为透明转发 |
| 外部 URL 下载能否防 SSRF/重定向绕过 | 私网/环回/元数据地址 + 重定向 → 断言全部被拒 |

---

## 8. 测试系统自身的健康度（元可观测）

把“测试体系”也当作被监控对象，纳入告警（补充 observability-security §6）：

- 需求覆盖率（有绑定的 REQ 占比）
- 变异测试分数趋势
- flaky 率（同 commit 重跑不一致的用例比例）
- AI 通过验收的平均迭代次数与耗时

### Flaky 治理

不确定性 = bug。flaky 用例进入 quarantine 清单（登记 + 到期），期间不阻断门禁但**独立告警**并限期修复；根因优先指向违反支柱二（未注入时钟/种子/网络）。

---

## 9. 建议目录补充

```text
internal/clock/            时钟接口与假时钟
internal/idgen/            ID 生成接口与可预测实现
internal/testutil/         harness、种子、golden 助手、goleak 封装
tests/acceptance/          按 REQ 组织的验收用例（携带 req.Covers）
tests/property/            不变量 / property-based 测试
tests/perf/                性能与内存基线（阈值门禁）
build/                     traceability.json / acceptance-report.json 产物
migrations/                显式数据库迁移（非 AutoMigrate）
```

## 10. 建议工具链

- 测试运行：`go test`（统一单语言入口）
- 竞态：`-race`
- 变异测试：`gremlins`（首选）/ `go-mutesting`
- Property：`pgregory.net/rapid` 或 `testing/quick`
- 泄漏：`go.uber.org/goleak`
- 依赖编排：`testcontainers-go` 或 docker-compose（固定 digest）
- 故障注入：Toxiproxy
- 前端 E2E：Playwright（仅管理端，纳入同一 `make verify`）
- 覆盖/门禁脚本：自研 `verify-traceability`、`check-golden`、`check-test-weakening`
