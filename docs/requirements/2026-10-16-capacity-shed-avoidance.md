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

- `kimi-k2.5`：**直连上游就是** `HTTP 503 {"code":"model_not_found","message":"No available channel for model kimi-k2.5 under group vip_video (distributor)"}` —— 丽珠侧渠道没开。要么让他们补渠道，要么把这个模型从分组白名单摘掉（否则它一直出现在模型列表里却调不通）。
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
