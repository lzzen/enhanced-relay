# 可观测性与安全

## 1. 慢请求分段

每个请求至少记录：

```text
request_body_receive_ms
rule_processing_ms
image_probe_ms
image_compress_ms
queue_wait_ms
routing_ms
dns_ms
tcp_connect_ms
tls_ms
upstream_request_write_ms
upstream_ttfb_ms
upstream_download_ms
oss_upload_ms
response_write_ms
total_ms
```

每次重试单独建立 Attempt，并记录渠道、出口、状态、错误类别及是否确认上游受理。

## 2. 参数审计分级

### 默认可记录

- 方法、路径、Content-Type；
- 原始模型和实际上游模型；
- size、quality、n、stream、output_format 等安全枚举；
- 字段存在性和 JSON 类型；
- 字符串长度、数组数量；
- 图片数量、格式、尺寸和字节数；
- 状态码、用量、上游 request ID 和耗时。

### 默认只记录摘要

- prompt/messages：是否存在、长度、条数、角色分布、可选哈希；
- tools：数量，名称是否记录由策略决定；
- 图片/音频：格式、尺寸和哈希，不保存正文。

### 默认禁止记录

- Authorization、Cookie、API Key；
- prompt/messages 原文；
- 图片、音频和文件正文；
- Base64 数据；
- 带签名参数的完整 URL；
- 完整上游响应正文。

## 3. 调试快照

受控快照必须：

- 明确指定目标范围；
- 设置最长有效时间；
- 使用独立加密密钥；
- 设置最短必要保留期；
- 限制管理员权限；
- 记录查看、下载和删除行为；
- 支持紧急全局关闭。

## 4. 图片与 URL 安全

- 校验真实 MIME，不信任扩展名和客户端 Content-Type。
- 限制压缩前文件大小、解码像素、帧数和图片数量。
- 防御图片解压炸弹和恶意元数据。
- 下载上游 URL 时使用域名白名单。
- 禁止私网、环回、链路本地及云元数据地址。
- 重定向后重新执行地址校验。
- 设置最大重定向次数、下载大小和总时间。

## 5. 超时与取消

不同阶段独立配置超时，同时受请求总预算约束。客户端取消后立即停止：

- 请求体读取；
- 图片处理等待；
- 上游请求；
- OSS 上传；
- 后续非必要处理。

已由异步上游受理的任务不能简单删除，必须转入取消或后台收敛流程。

## 6. 告警

- 总耗时和各阶段 P95/P99 异常；
- 连接池等待；
- 图片 worker 队列过长；
- OSS 失败率；
- Trace 丢失率；
- 重试放大率；
- 异步任务积压；
- 计费预留长期未结算；
- 同一幂等键出现多个上游任务。
