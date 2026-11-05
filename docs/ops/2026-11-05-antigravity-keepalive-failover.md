# 2026-11-05 Antigravity：心跳字节不再关闭换号通道

## 现象

Gemini 长任务（尤其带工具的 Claude Code / Codex 风格请求）偶发 `empty_stream`，
HTTP 200 但没有任何内容。生产日志里上游先返回 `MALFORMED_FUNCTION_CALL`，
网关随后报 `empty Antigravity compatibility stream after keepalive`。

## 根因

`antigravity_gateway_compat_stream.go` 的首字节前保活（`writePreContentKeepalive`）
会往客户端写 `: ping\n\n`。这段字节一旦写出：

1. HTTP 200 即被提交；
2. `c.Writer.Size()` 从 -1 变成 8；
3. handler 的换号判定全部是 `if c.Writer.Size() != writerSizeBeforeForward { 禁止换号 }`。

于是**只要上游的失败发生在保活之后，就再也不能换号**，只能给客户端写错误帧。
`bd48b2b2d`（2026-09-28）引入这个保活时在注释里明确写了这是有意的取舍。
上游 `MALFORMED_FUNCTION_CALL` 的典型到达时间是 16 秒，正好落在 15 秒保活之后，
所以几乎必然命中「无法换号」。

## 修复

心跳字节不是语义响应，换号判定不应被它污染 —— 这与仓库里 compact 请求已有的
`OpenAICompactKeepaliveAdjustedWrittenSize`（#3887）是同一个道理。

1. 新增 `backend/internal/service/antigravity_stream_keepalive.go`
   - `recordAntigravityStreamKeepaliveBytes(c, n)`：把心跳字节累加到 gin.Context；
   - `AntigravityKeepaliveAdjustedWrittenSize(c)`：返回扣掉心跳后的真实写出量，
     语义对齐 `c.Writer.Size()`（一个字节都没写、或只写了心跳，都返回 `-1`）。
2. `antigravityClientWriter` 新增 `WithContext(c)`、`WriteKeepalive(p)`、
   `FprintfKeepalive(format, ...)`；三条流式链路（gemini / claude / upstream）
   的心跳写入全部改为 `*Keepalive` 版本，并绑定请求上下文。
3. handler 的换号判定改用调整口径：`gateway_handler.go`（Gemini 与 Claude 两条
   `/v1/messages` 路径、以及 `gatewayForwardErrorAlreadyCommunicated`）、
   `gateway_handler_chat_completions.go`、`gateway_handler_responses.go`。
4. `gateway_helper.go` 的 `gatewayStreamHasOnlyHeartbeats` 兼容 service 层登记的心跳。
5. `antigravity_gateway_compat_stream.go` 里所有「已经发过保活就就地报错」的分支
   全部改为继续换号：`finish()`、`preContentTimeout()`、
   `handleAntigravityCompatEmptyStream`、`handleAntigravityCompatReadError`。
   错误帧由 handler 在换号全部失败后统一补写。

## 行为变化

| 场景 | 修复前 | 修复后 |
| --- | --- | --- |
| 上游 16s 才返回 MALFORMED_FUNCTION_CALL | 保活已提交 200 → 客户端收到 `empty_stream` | 换到其它账号重试 |
| 首字节前上游读错误 / 超时（已发过保活） | 就地写 `stream_read_error` / `stream_timeout` | 换号重试 |
| 上游确实长时间无输出、最终空流 | 报错 | 换号；全池失败后由 handler 统一写错误帧 |

客户端在换号期间只会看到 SSE 注释 `: ping`，这是合法 SSE，不会污染回复内容。

## 验证

- `go build ./...`、`go vet ./internal/service ./internal/handler`、`gofmt` 干净。
- `go test ./internal/service -run TestAntigravity -count=1` 通过（含按新语义改写的
  `TestAntigravityCompatHandlerPreContentKeepalive`、
  `TestAntigravityCompatHandlerRepeatsPreContentKeepalive`、
  `TestAntigravityCompatHandlerPreContentDeadlineWithCommentOnlyStream`、
  `TestAntigravityCompatHandlerErrorsAfterPreContentPing`、
  `TestAntigravityCompatEmptyAfterKeepaliveReportsStreamError`）。
- 镜像内 Dockerfile 验证阶段同样跑 `TestAntigravity`，通过。
- 基线失败与本改动无关：`TestRecordUnsupportedModelNeedsThreshold`、
  `TestGatewayCodexModels_NonOpenAIGroupsUseMappedModels/DeepSeek`
  （已用 stash 证伪）。
- 生产探测（2026-11-05 16:20）：
  - 工具调用请求 `dc3dbab9-2f21-4dfc-9641-52cdfebc66de`：HTTP 200、`read_file` 正确、
    TTFT 5.785s；
  - 274KB 长上下文 + 工具 `7fc4cae6-9b16-41c9-88ad-e40f31c54b2b`：HTTP 200、TTFT 6.392s；
  - 短请求 `093de47c-a3ab-46fb-9562-c2cd45341a40`：`PONG`、TTFT 2.715s；
  - 部署后 `ops_error_logs` 中 antigravity 记录 0 条，`empty_stream`/`malformed` 0 条。

## 部署

- 镜像 `sub2api-ag-quota:antigravity-keepalive-20261105`
  （`sha256:b14c80c9ed8a39cdd7e9ccee2fc925d213503cb9c9a4f6b7c08afbadfd4d54fc`）。
- commit `ce6a8214f`（父 `83e69b5f2`），分支 `fix/capacity-shed-stream-accounting`。
- 部署校验：`{"status":"deployed","frontendUnchanged":true,"health":"healthy"}`；
  compose 备份 `/home/ubuntu/sub2api/docker-compose.yml.bak_antigravity_20261105T161832`。
- 回滚：`sub2api-ag-quota:antigravity-malformed-20261105`（`cec6776b12f2`），
  `rollback-pre-antigravity-20261103` 当前也指向它。
