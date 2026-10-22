# 上游容量降载的号池避让 + 丽珠组国内模型排查（2026-10-16）

现场报的两个问题：**gpt 调不动**、**走 sub2api 的丽珠分组调不动**（丽珠只调几个国内模型）。

两者根因完全不同，且**都与上一轮 R6/R7 改动无关**：R6 只裁剪模型列表与准入，这些请求全部通过准入之后才在上游失败。

---

## 一、丽珠组：`/responses` 优先把模型按模型打坏了

### 证据（直连上游 vs 走 sub2api）

| 模型 | 丽珠 `/v1/chat/completions` | 丽珠 `/v1/responses` | 走 sub2api |
| --- | --- | --- | --- |
| `Qwen-3.8-Flash` | **200** | **400 `模型服务调用失败`** | **400** ❌ |
| `GPT-5.6-Luna` | 200 | 200 | 200 |
| `GLM-5.3-Flash` | 200 | — | 200 |
| `deepseek-v4-flash` | 200 | — | 200 |

`usage_logs` 佐证：`Qwen-3.8-Flash` **最后一次成功是 10-14 10:22**，之后 75 次调用再无成功记录；同期主力模型 `GLM-5.3-Flash`（2508 次/7 天）一直正常。所以不是"整个分组挂了"，而是**按模型坏**。

### 根因

`accounts.extra.openai_responses_supported` 是**账号级**开关（由 `/responses` 能力探测写入，见 `openai_apikey_responses_probe.go`），探测只按账号、**不区分模型**。账号 40（丽珠）被标记为 `true` 后，sub2api 把该账号的**所有**请求优先发往 `/responses`；丽珠的 newapi 只对部分模型实现了该端点，`Qwen-3.8-Flash` 在那边直接 400。

### 处理（已生效，配置级可回滚）

```
UPDATE accounts SET extra = jsonb_set(extra, '{openai_responses_supported}', 'false') WHERE id = 40;
```

回退到上游原生的 `/v1/chat/completions`。清 `apikey:auth:*` 后重启，实测 6 个常用国内模型 + 出图全部 **200**：
`Qwen-3.8-Flash`、`GLM-5.3-Flash`、`丽珠::deepseek-v4.1-flash`、`deepseek-v4-flash`、`GPT-5.6-Luna`、`Qwen3.8-27B`、`gpt-image-2`。

### 另外两条不是我们的问题

- `kimi-k2.5`：**直连上游就是** `HTTP 503 {"code":"model_not_found","message":"No available channel for model kimi-k2.5 under group vip_video (distributor)"}` —— 丽珠侧渠道没开。**已按需求方决定从丽珠组白名单摘掉**（`groups.id=8` 的 `model_allowlist.models` 19 → 18 项，含缓存刷新生效），现在调用得到明确的 `404 model_not_found: Model "kimi-k2.5" is not available for this group`，而不是让用户撞上游 503；等丽珠补了渠道再加回来。
- `mimo-v2.5-tts`：上游要求 `messages` 含 assistant 角色（TTS 约定），不是故障。

> 结构性风险：丽珠组只绑了账号 40 **一个号**，没有任何备用，上游一坏就是硬失败。

---

## 二、gpt：号池为什么"不避开报错上游"

先纠正一个误判：`gpt-6.1-sol` 实测是 **200** 的；`gpt-5.4` 稳定 502 是因为 **zyuou 和 wocnm 两个上游都回 404**：

```
Model "gpt-5.4" is not supported by any configured account in this group
```

4 个候选号全灭才 502。这是上游能力问题，不是调度问题。group 13 的 5 个账号对 `gpt-6.1-sol` 均为**无映射 = 原样透传**，不存在任何模型改写。

### 三层机制的真实现状

| 机制 | 现状 | 证据 |
| --- | --- | --- |
| 同请求内换号 | ✅ 有效 | 一次 `gpt-5.4` 请求 `switch_count` 1→4，依次 30(502)→1(429)→21(404)→22(404) |
| 404 模型不支持 | ✅ 30 分钟账号+模型冷却 | DB 中 21/22 挂着 `gpt-5.4`、5 号挂着 `gpt-image-1` |
| 5xx 断路器 | ⚠️ 仅 **10s / 45s**（连击 2/3 次） | `openAIModelTransientShortCooldown/LongCooldown`，且只在进程内存 |
| **容量降载**（`Our servers are currently overloaded`） | ❌ **直接早退、不写任何状态** | `openai_account_runtime_block_fastpath.go` 中 `isOpenAIRequestScopedCapacityShed` 命中即 `return false` |
| 临时不可调度规则 / 过载冷却设置 / apikey 健康熔断 | ❌ 全部未配置（熔断还只对池模式生效，这几个号不是池模式） | `settings` 表中 `%UNSCHED%/%OVERLOAD%/%BREAKER%` 全空 |

结论：**请求内换号是工作的**；但持久避让要么窗口只有 10–45 秒（高流量下等于没避让），要么按设计完全不记账 —— 于是每个新请求都可能再次选中同一个过载上游。

另外，容量降载在流中途发生时**物理上无法换号**（响应已写给客户端），代码会把错误码改写成可重试的 `server_error` 交给客户端退避重试，这是有意的设计。

---

## 三、修复：连续两次降载才短暂移出轮换（收窄版）

> 需求方明确要求：**不改模型、不改分组、不改密钥**，且避开动作要尽量窄。

`handleOpenAIAccountUpstreamError` 对容量降载不再"什么都不记"，而是：

1. 按账号累计**连续**降载次数（时间窗 10 分钟，任何一次成功即清零 —— 挂在 `ReportOpenAIAccountScheduleResult` 的成功分支）；
2. 连续达到 **2 次**才把该账号打 **2 分钟**过载冷却（`SetOverloaded`，调度器据此跳过它，后续请求换同组其他号）；
3. **单次降载仍然什么都不改**（保持"请求级信号"的原语义）；
4. 池模式账号保留既有豁免（一份本地凭证背后是多条上游 key，降载不代表账号整体故障）；
5. 普通 5xx 不升级为账号级冷却，仍只冷却失败的模型。

| 文件 | 改动 |
| --- | --- |
| `backend/internal/service/openai_capacity_shed_streak.go` | 新增：连续降载计数状态（窗口/阈值/冷却时长常量、懒加载、成功清零） |
| `backend/internal/service/openai_account_runtime_block_fastpath.go` | 容量降载分支：达到阈值时打 2 分钟冷却并记 `openai_capacity_shed_overload_cooldown` 日志 |
| `backend/internal/service/ratelimit_service.go` | 新增 `setAccountOverloadCooldown`：按显式时长打冷却，不占用管理员配置的 529 全局冷却 |
| `backend/internal/service/openai_account_scheduler.go` | 成功时清零连续降载计数 |
| `backend/internal/service/openai_gateway_service.go` | 新增两个懒加载字段 |
| `backend/internal/service/openai_capacity_shed_overload_cooldown_test.go` | 新增 6 个用例 |

### 验证

| 用例 | 断言 |
| --- | --- |
| 单次降载 | 不写状态、不冷却（保持原请求级语义） |
| 连续两次降载 | 恰好一次 `SetOverloaded`，冷却 ≈2 分钟，账号不被禁用 |
| 中间夹一次成功 | 计数清零，后续不冷却 |
| 池模式 | 连续 3 次也不冷却 |
| 普通 5xx | 不触发账号级冷却，仍只冷却失败模型 |
| 时间窗失效 | 超出窗口的第二次降载重新计数 |

全量门：`go test ./internal/server/... ./internal/handler/... ./internal/service/` 全绿（service 143s）。

### 线上实证（合成探针，跑完即删）

单测只证明代码逻辑，不能证明**部署到线上的那个二进制**真的这么做，所以用隔离探针实测：临时分组 + 临时账号（全部 `ZZ-*` 前缀）+ 假上游（对每次请求都回 `503 Our servers are currently overloaded` 并计数）+ 临时 key。

| 调用 | 账号 `overload_until` | 假上游累计被打 |
| --- | --- | --- |
| call#1 | `11:07:15` + **2 分钟** | 2 |
| call#2 | 未延长 | 2 |
| call#3 / call#4 | 未延长 | 2 |

- call#1 之所以一次就触发：容量降载被标记为"同账号可重试"，客户端的一次请求内部把该账号重试了一次 → 连续 2 次降载 → 立刻进入冷却。**一次请求内重试预算不受影响**（先重试，后冷却）。
- call#2–#4：假上游计数停在 2，说明**冷却期内该账号不再被选中**（该组无其他账号，客户端收到 503 `Service temporarily unavailable`）。这正是要的"避开报错上游"。
- 日志：`openai_capacity_shed_overload_cooldown {"account_id": 49, "streak": 2, "cooldown_ms": 120000}` 与 `account_overloaded {"reason": "capacity_shed"}`。
- 探针已完全删除：分组/账号/key 残留 0，假上游端口释放，生产分组数不变，容器 `healthy`，磁盘 24G 可用。

上线后真实流量尚未触发（说明 6.1sol 该时段一直正常）；下一次 wocnm 类上游过载时会自动生效。

---

## 补充（2026-10-21）：流式路径漏记账修复 + 同账号重试预算收窄

### 发现的缺陷

1. **流式路径不记账（真实 bug）**
   `backend/internal/service/openai_gateway_passthrough.go` 的
   `handleOpenAIStreamTerminalAccountSideEffects` 只把 401/403/429/529 交给
   `handleOpenAIAccountUpstreamError`，而容量降载的语义状态是 503 → 掉进 `default`，
   直接 `return statusCode, false`。
   后果：`recordOpenAICapacityShedFailure` 永远无法被 SSE 路径触发，上文
   "连续两次降载 → 2 分钟冷却" 只对**非流式 503** 生效；而 OpenAI 生产里的降载形态
   恰恰是「HTTP 200 + 流内 response.failed」（即本目录单测里的 `capacityShedPayload`），
   Codex/Responses 主路径全是流式 → 调度器每个新请求都重新选中同一个降载上游。
   对照：HTTP 非 2xx 路径（`openai_gateway_upstream_errors.go`）与 WS 路径
   （`openAIWSPayloadTransientStatus` 对 server_is_overloaded/slow_down 返回 503）
   都会记账，只有 SSE 的两条路径漏了，属实现遗漏而非设计选择。

2. **首字延迟**：容量降载被标记为"同账号可重试"后沿用默认 3 次预算，
   按 500ms/1s/2s 退避，等于先在被降载的上游上耗掉约 3.5 秒才开始换号；
   而降载是上游容量问题，同账号几秒内几乎必然再次降载，只有换到独立账号/渠道
   才有机会立刻拿到首字。

### 改动

| 文件 | 改动 |
| --- | --- |
| `backend/internal/service/openai_gateway_passthrough.go` | `handleOpenAIStreamTerminalAccountSideEffects` 增加 503 容量降载分支，走 `handleOpenAIAccountUpstreamError` 记账；语义不变（不禁用账号） |
| `backend/internal/service/openai_capacity_shed_streak.go` | 新增 `openAICapacityShedSameAccountRetryMax = 1` |
| `backend/internal/service/openai_gateway_upstream_errors.go` | 请求级容量降载的 failover 错误设置 `SameAccountRetryMax = 1`（`RetryableOnSameAccount` 仍为 true） |
| `backend/internal/service/openai_capacity_shed_overload_cooldown_test.go` | 新增 3 个用例 |

### 验证

| 用例 | 断言 |
| --- | --- |
| 流式单次降载 | (503,false)，不写调度状态 |
| 流式连续两次降载 | 恰好一次 `SetOverloaded`，冷却 ≈2 分钟，账号不被禁用 |
| 流式非容量 failed | 502，调度状态零写入 |
| 降载 failover 错误 | `SameAccountRetryMax == 1`，仍保留一次同账号确认重试 |

门：`go build ./...`、`go test ./internal/service/...`、`go test ./internal/handler/...` 全绿。

### 行为变化与已知取舍

- "先重试、后冷却"的顺序不变，但确认重试从最多 3 次降为 1 次：第 2 次被打后账号即进入
  2 分钟冷却，随后立刻换号，不再把 3.5 秒退避耗在同一个降载上游上。
- 若上游是**全局**降载（两个渠道同时被打），池内账号会在少量请求内陆续进入 2 分钟冷却，
  此期间新请求可能收到"无可用账号"的 503。这是刻意取舍：继续重试只会拉长首字延迟且必然失败，
  短冷却能让容量恢复后自动回流。

---

## 补充（2026-10-22）：按池子规模收敛冷却 —— 只有存在替代账号时才把账号移出轮换

### 起因

上一节把 SSE 路径接通后，"连续 2 次降载 → 2 分钟冷却"对所有路径统一生效。但冷却写的是
`accounts.overload_until`，而选号侧是**硬过滤**：

- `backend/internal/repository/account_repo.go:2032`
  `dbaccount.Or(OverloadUntilIsNil(), OverloadUntilLTE(now))`
- `backend/internal/service/account.go:190` `IsSchedulable()` 同样剔除

也就是说被冷却的账号对**新请求完全不可见**。如果某个分组里只有这一个可调度账号，冷却
不是"轮换"，而是把这个分组整组关停 2 分钟——期间新请求直接 503 `No available accounts`
（`backend/internal/handler/openai_gateway_handler.go:2286`）。降载往往只是秒级抖动，
这个代价远大于收益。

### 线上池子实测（只读 SELECT，2026-10-22）

按 `IsSchedulable()` + `schedulableAccountsQuery` 同口径统计 production：

| 组内可调度账号数 | 分组 |
| --- | --- |
| 6 | 13 codex-低价 |
| 4 | 14 免费快快的薅羊毛、16 PRO、17 codex-企业专线、18 claude-max满血 |
| 3 | 1 claude、23 反重力 (Copy ) |
| 2 | 12 反重力、15 大羊毛，聪明点很慢 |
| **1** | 5 国模、8 丽珠组、11 claude-生图、19 生歌配音、24 羊毛组生图、25 羊毛语音组、27 兜底专用 |
| 0 | 2、3、4、6、7、9、10、20、21（空组/临时组） |

账号与分组的交叉关系（决定"冷却后会不会变空"）：

| 账号 | 所属分组 | 关键点 |
| --- | --- | --- |
| 5 zyuou-生图原价 | 1,11,12,13,16,17,18,23 | 组 11 仅它一个 |
| 43 百炼(阿里云) | 14,15,24,25 | 组 24/25 仅它一个 |
| 34 反重力-hayesshermeka | 12,23,27 | 组 27「兜底专用」仅它一个 |
| 40 丽珠 | 8,23 | 组 8 仅它一个 |
| 31 生歌配音 | 1,18,19 | 组 19 仅它一个 |
| 30 / 1 / 3 / 21 / 22 / 23 / 24 / 25 / 26 | 单组或都在 ≥4 账号的组里 | 冷却后池子仍非空 |

结论：**主流量池（13/16/17/14，4–6 个账号）足以支撑轮换**；风险集中在 7 个单账号分组，
以及跨组账号在"某个小分组里是唯一出口"的情形。

### 改动

| 文件 | 改动 |
| --- | --- |
| `backend/internal/service/openai_account_runtime_block_fastpath.go` | 新增 `capacityShedCooldownKeepsPoolServable`：冷却前逐个检查该账号所属分组，若**任一**分组在冷却后会没有可调度账号，则跳过冷却并记 `openai_capacity_shed_cooldown_skipped_sole_candidate` |
| `backend/internal/service/openai_capacity_shed_sole_candidate_test.go` | 新增 5 个用例（唯一出口跳过、有兄弟则冷却、跨组任一组会空则跳过、全组都有兄弟则冷却、无快照 fail-open） |

判定语义：

- 用调度快照 `schedulerSnapshot.ListSchedulableAccounts(ctx, &groupID, platform, false)`
  取每个分组的当前可调度账号，平台维度与选号一致（openai 账号只和 openai 账号竞争）。
- 只要存在一个分组"除自己外没有别的可调度账号"→ 不冷却。
- 信息缺失（快照未启用 / 账号无分组归属 / 单组查询失败）→ fail-open，保持原有冷却语义，
  避免在拿不到数据时改变线上行为。
- 检查发生在**冷却写入之前**且只在连击达标（streak=2）时触发，属于冷路径，一次判定至多
  做 `len(GroupIDs)` 次内存快照查询。

### 对当前池子的实际效果

| 账号 | 是否还会被冷却 | 说明 |
| --- | --- | --- |
| 5、31、40、43、41 | **否** | 分别在组 11/19/8/24·25/5 是唯一出口，继续用比空池更合理 |
| 1、3、21、22、30、23、24、25、26、45、46 | 是 | 所在分组都还有别的候选 |
| 27、28、29、34（anthropic/antigravity） | 不适用 | 平台不是 openai，本来就不走这条容量降载记账路径 |

组 13 仍有 1/3/21/22/30 共 5 个可冷却账号（账号 5 被守卫排除），轮换能力保留，但任何时刻
都不会因为冷却把某个分组清空。

### 验证

| 用例 | 断言 |
| --- | --- |
| 组内唯一账号连续 2 次降载 | `SetOverloaded` 调用 0 次 |
| 组内另有候选，连续 2 次降载 | 恰好 1 次冷却，`overload_until ≈ now+2min` |
| 跨组账号（组 14 有兄弟、组 24 只有自己） | 0 次冷却 |
| 跨组账号（两组都有兄弟） | 恰好 1 次冷却 |
| 无调度快照 | fail-open，照常冷却 |

门：`go build ./...`、`go vet ./internal/service/`、`go test ./internal/service/...`、
`go test ./internal/handler/...` 全绿；`go test ./internal/service/ -run CapacityShed -v` 17 个用例全绿。
