# 生产变更记录：火山账号 42 移出分组 8

时间：2026-10-15 17:30 (Asia/Shanghai)
执行人：DSH agent（经用户确认）

## 背景

用户反馈火山（方舟）额度被用尽。排查 `usage_logs` 后确认：

| 维度 | 值 |
| --- | --- |
| 账号 | 42「火山收费」`https://ark.cn-beijing.volces.com/api/v3` |
| 分组 | 8「丽珠组」 |
| API Key | 20「自用-丽珠」（user 1 Zeroxcore） |
| 模型 | `GLM-5.3-Flash` → 上游 `glm-5-3-flash-260828` |
| 时间窗 | 10-15 15:08:27 – 16:45:12（约 97 分钟） |
| 量 | 312 次请求，1,713,971 input + 203,285 output tokens |
| 调用方 | `deepseek-harness/0.2.0-rc.2`，单一 IP，单一 session（测试性消耗） |

## 根因

1. 分组 8 的账号池只有 40「丽珠」（`newapi.livzon.cn`）与 42「火山收费」；原上游账号
   11「聚合站-丽珠通道」（`http://llm-hub:8787/v1`）已不在组内且 `schedulable=false`。
2. 账号 40 今天 15:07:46 起持续返回上游 403 `Upstream access forbidden`，进入临时冷却；
   15:08:27 会话首次选号时 40 不可用，唯一可用的 42 被选中。
3. **会话粘性优先于优先级排序**：`selectBySessionHash` 命中绑定后不再比较 `account.Priority`
   （40=10 本应优于 42=70），绑定 TTL 1 小时且每请求刷新，于是 97 分钟内 312 次请求全部
   复用 42。该机制见 `backend/internal/service/openai_sticky_compat.go`、
   `openai_account_scheduler.go`；`openaiStickySessionTTL = time.Hour`
   （`openai_gateway_service.go:35`），当前 `openai_advanced_scheduler_enabled=false`。
4. 16:45 火山 42 返回 403 `your account has an overdue balance`（欠费）。

## 变更内容

经用户确认，仅把账号 42 从分组 8 摘除（保留其分组 5「国模」成员身份）：

```
PUT /api/v1/admin/accounts/42   {"group_ids": [5]}      -> 200
group_ids: [8, 5] => [5]
```

- 备份：`account_groups_bak_20261015_1730`（47 行，变更前快照）
- 数据库核对：`account_groups` 中 `42|8` 已删除，`42|5` 保留
- 调度侧核对：分组 8 的 `model-allowlist-sources`（34 个模型）来源账号全部只剩
  `40「丽珠」`；40 的 `model_mapping` 覆盖了 42 原先服务的
  `GLM-5.3-Flash`、`glm-5.3-flash`、`deepseek-v4.1-flash`，无模型覆盖缺口。

## 后续影响与回滚

- **当前风险**：账号 40 的上游 `newapi.livzon.cn` 仍在返回 403，因此分组 8 的
  `GLM-5.3-Flash` 等模型在 40 恢复前会返回「无可用账号」。修复 40 是关键。
- 火山 42 仍可在分组 5「国模」被调度（近 7 天分组 5 未消耗过账号 42）。
- 回滚：把 8 加回 `group_ids` 即可，或从 `account_groups_bak_20261015_1730` 恢复
  `INSERT INTO account_groups SELECT * FROM account_groups_bak_20261015_1730 WHERE account_id = 42 AND group_id = 8;`
- 若希望避免长粘性导致的单账号集中消耗，可调小
  `Gateway.OpenAIWS.StickySessionTTLSeconds`（默认 3600）或开启高级调度器的
  sticky escape（按错误率/TTFT 逃逸）。
