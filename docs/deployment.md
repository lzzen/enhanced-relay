# 部署与运维

## 1. 发布形态

同时提供：

```text
gateway-linux-amd64
gateway-linux-arm64
Docker Image
```

Go 服务可以直接编译为 Linux 二进制运行，Docker 不是必需条件。

## 2. 单二进制

建议构建：

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
go build -trimpath -ldflags="-s -w" -o gateway ./cmd/gateway
```

管理端静态资源通过 Go `embed` 打入二进制。生产数据库使用 PostgreSQL/MySQL，以降低 CGO 和本地状态依赖。

图片处理首版优先纯 Go。若后续采用 libvips，应选择以下之一：

- 安装受控版本的动态库并使用 CGO；
- 使用包含 libvips 的 Docker 镜像；
- 将图片处理拆成独立服务，主网关保持静态单二进制。

## 3. systemd

```ini
[Unit]
Description=AI Enhanced Gateway
After=network-online.target
Wants=network-online.target

[Service]
User=gateway
WorkingDirectory=/opt/ai-gateway
ExecStart=/opt/ai-gateway/gateway --config /etc/ai-gateway/config.yaml
Restart=always
RestartSec=3
LimitNOFILE=1048576
EnvironmentFile=-/etc/ai-gateway/gateway.env

[Install]
WantedBy=multi-user.target
```

## 4. Docker

适用于多节点、Kubernetes、依赖 libvips 或希望环境完全一致的场景。Docker 是交付选择，不是系统架构约束。

## 5. 生产拓扑

```text
Nginx/Caddy/LB
  → Gateway Ingress 多实例
  → 现有网关
  → Gateway Egress 多实例
  → 上游

共享：PostgreSQL/MySQL、Redis、OSS、Telemetry Collector
```

Ingress 与 Egress 可由同一二进制通过启动参数选择角色，也可在小规模环境中由同一实例承载。

## 6. 发布流程

```text
CI 全量验证
→ 构建签名产物
→ 测试环境自动验证
→ 影子流量
→ 小比例灰度
→ 指标门禁
→ 全量
```

必须能够一键关闭：请求修改、图片压缩、OSS 转存、智能路由和调试快照；透明代理路径应始终保留为紧急降级方案。
