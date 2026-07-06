# 自动化测试策略

> 本文定义“测什么”。关于“谁来跑、如何自动判定、如何防止 AI 写假测试”的 AI 自动验收体系，见 `docs/ai-testing-acceptance.md`。

## 1. 目标

测试体系必须让常规开发无需人工重复验证，并将每次线上事故转化为永久回归用例。测试关注真实协议、业务合同、计费一致性、资源边界和故障恢复，而不是单纯追求覆盖率。

## 2. 测试层级

### 2.1 单元测试

使用确定性表驱动测试覆盖：

- 规则条件与操作；
- JSON、form 和 multipart 字段访问；
- 模型和接口映射；
- 路由评分、熔断与回退；
- 图片尺寸和压缩策略；
- 异步任务状态迁移；
- 计费、退款、补扣及幂等。

图片规则边界样例：

```text
2048x2048  允许
2049x2048  允许
2049x2049  拒绝
2560x1440  允许
2560x1441  拒绝
2999x1000  允许
3000x1000  拒绝
```

### 2.2 Mock Provider

仓库内维护可编程上游模拟器，支持：

- JSON 和 multipart；
- Base64、Data URL 和临时 URL；
- SSE；
- 异步任务与 webhook；
- 延迟首字节、慢速分块响应；
- 连接中断、非法 JSON；
- 429、500、502；
- 已受理但响应丢失。

模拟器能够断言网关实际发送的 URL、Headers、参数、图片尺寸和字节数。

### 2.3 集成测试

测试环境启动：

```text
gateway
mock-provider
PostgreSQL
Redis
MinIO
Toxiproxy
```

核心链路：

```text
上传参考图 → 压缩 → 参数规则 → 路由 → 模拟上游
→ Base64/URL 结果 → MinIO → 返回 URL → 最终结算 → Trace 校验
```

### 2.4 故障注入

必须覆盖：

- DNS、TCP、TLS 变慢；
- 上游几十秒完成但网关总耗时异常；
- 上游结果下载慢；
- OSS 卡住；
- 客户端读取慢或提前断开；
- 连接池耗尽；
- Redis/数据库抖动；
- 海外出口中断；
- 上游已创建任务但提交响应丢失。

测试中缩短超时配置或注入时钟，禁止真的等待数百秒。

### 2.5 协议快照

保存脱敏 Golden 文件：

```text
testdata/openai-image-request.json
testdata/image-edit-multipart.golden
testdata/async-task-created.json
testdata/async-task-succeeded.json
testdata/billing-breakdown.json
```

协议变更必须显式审核快照差异。

### 2.6 管理端 E2E

使用 Playwright 覆盖：创建规则、校验、影子运行、灰度、发布、回滚、Trace 查询、任务查询和计费明细。

### 2.7 性能回归

建立以下基线：

- 无规则透明转发；
- JSON 修改；
- multipart 上传；
- 图片压缩；
- Base64 转 OSS；
- SSE；
- 异步创建和查询。

监控 P50/P95/P99、吞吐、每请求分配、峰值内存、Goroutine、连接数和队列长度。

## 3. CI 门禁

```text
格式与静态检查
→ 单元测试
→ Race 检测
→ 集成测试
→ 协议快照
→ 前端 E2E
→ 性能冒烟
→ 二进制构建
→ Docker 构建
→ 漏洞/SBOM 扫描
```

最低后端门禁：

```bash
go test ./...
go test -race ./...
go vet ./...
```

## 4. 事故回归规范

每次事故修复必须附带：

1. 能复现事故的 Mock Provider 行为或固定输入；
2. 修复前失败、修复后通过的测试；
3. 对应 Trace 阶段和指标断言；
4. 若涉及费用，准确的账本结果断言。
