# 反重力 61 号测试失败（404）与 Gemini 3.8 Flash 首字耗时归因

时间：2026-10-29 19:31 – 21:26 (Asia/Shanghai)
执行人：DSH agent（经用户确认）
影响范围：生产 `accounts.id=61`「google小号」的 `credentials.model_mapping`

## 一、现象

用户在后台新增反重力 Google 账号 61（`tadayoshi.ryuhei@gmail.com`，OAuth，plan=Pro，
project_id=`aicode-consumers`），本地登录验证成功，但管理端「测试连接」失败：

```
API 返回 404: {"error":{"code":404,"message":"Requested entity was not found.","status":"NOT_FOUND"}}
```

账号其他条件均正常，可排除配额与鉴权：

| 项 | 值 |
| --- | --- |
| 配额快照 | gemini-weekly 0.9994 / gemini-5h 0.9978 / 3p-weekly 1.0 / 3p-5h 1.0 |
| status / schedulable | active / true |
| priority / proxy_id | 10 / 2（`mihomo-yk-jp`） |
| 分组 | 12「反重力」、23「反重力 (Copy )」 |

## 二、根因：存储态裸名自映射抑制了思考变体解析

Antigravity 上游 `cloudcode-pa` 的模型目录**只有带思考档位后缀的变体**，
裸名（如 `gemini-3.8-flash`）在上游不存在，直接转发必然 404。
服务端本应在转发前把裸名解析成变体：

```
getMappedModelForThinkingLevel()
  → resolveGeminiThinkingVariantForLevel()   ① 变体解析
  → mapAntigravityModel()                    ② 未命中时回退
```

`backend/internal/service/antigravity_gemini_thinking_variant.go:188-192` 有两个
「放弃解析并尊重原样透传」的条件：

```go
if mapped, matched := resolveRequestedModelInMapping(mapping, model); matched {
    if strings.TrimSpace(mapped) != model || accountRawModelMappingHasKey(account, model) {
        return "", false
    }
}
```

第 2 个条件——**裸名映射到自身、且该条目真实存在于 `credentials.model_mapping`**——被判定为
「用户意图」而被尊重。61 号恰好命中：

`frontend/src/components/account/CreateAccountModal.vue`（OAuth 路径第 6954 行、
上游路径第 5847 行，均为 `credentials.model_mapping = antigravityModelMapping`）
在建号时把**整张 `DefaultAntigravityModelMapping`** 写入
`credentials.model_mapping`，其中包含 `"gemini-3.8-flash": "gemini-3.8-flash"`
（`backend/internal/domain/constants.go:175`）。

于是变体解析被跳过，裸名直接发上游 → 404。

对照账号 34「反重力-hayesshermeka」：`credentials` 里**没有** `model_mapping`（当前
`jsonb_object_keys` 计数为 0），走运行时注入的 `ensureAntigravityDefaultPassthroughs`
（`backend/internal/service/account.go:664-689`）。运行时注入**不算用户意图**
（`accountRawModelMappingHasKey` 返回 false），变体解析照常生效，因此 34 号正常。

### 因果验证（受控实验）

1. 备份 61 号全量 `credentials` 与 `model_mapping`
2. 仅删除 `gemini-3.8-flash` 一个键 → 「测试连接」连续两次通过
3. 恢复备份 → 「测试连接」再次 404
4. 结论成立，随即执行正式修复

## 三、变更内容（已执行）

2026-10-29 20:11:10，从 61 号 `credentials.model_mapping` 删除 3 个**裸名自映射**：

| 删除的键 | 说明 |
| --- | --- |
| `gemini-3.6-flash` | 保留其 `-high/-low/-medium/-tiered` 变体 |
| `gemini-3.7-flash` | 同上 |
| `gemini-3.8-flash` | 同上 |

- 键数：**66 → 63**
- 未触碰：31 个其他裸名自映射、所有带后缀变体的映射
- 核验：`gemini-3.8-flash` 及 `-high/-medium/-low` 全部通过；`gemini-3-flash`、
  `gemini-2.5-flash`、`gemini-3.1-flash-lite`、`claude-sonnet-4-6`、`claude-opus-4-6` 均通过

## 四、纠正一个错误结论：404 不触发模型级冷却

排查中曾判断「404 会触发该账号 30 分钟 per-(account,model) 冷却」，**该结论对
Antigravity 不成立**，现予纠正：

- `RateLimitService.HandleUpstreamModelNotFound` 仅在 `requestedModel` 非空时生效
- Antigravity 的 `handleUpstreamError`
  （`antigravity_gateway_retry.go:1286`）调用 `HandleUpstreamError` 时**未传 model**，
  因此 404 冷却分支不执行
- Antigravity 自身的 `handleModelRateLimit`（同文件 1125 行）首行即
  `if p.statusCode != 429 && p.statusCode != 503 { return Handled: false }`
- 数据库佐证：34/52/61 的 `extra` 中**无** `model_rate_limits`，亦无
  `unsupported_models`

结论：Antigravity 的 404 是「无账号级惩罚」的普通错误。且
`shouldFailoverUpstreamError`（同文件 795 行）只含 `401/403/429/529` 与 `>=500`，
**404 不换号**，错误直接脱敏回传客户端。

`enable_model_fallback = false`，故 `isModelNotFoundError` 触发的 fallback 分支
（`antigravity_gateway_gemini.go:195-196`）当前不生效。

## 五、3.6 / 3.7 仍然 404 是另一回事（未修）

`gemini-3.6-flash*` 与 `gemini-3.7-flash*` **整代已在上游退役**，与自映射无关。
证据：账号 34（无存储映射，走默认表）与 52 在同样这批变体上同样 404。

## 六、Gemini 3.8 Flash 首字耗时归因（非 sub2 bug）

用户反馈「本地真实调用要十几秒，sub2 只显示 5s」。实测归因为**上游非流式返回**。

### 客户端实测（`/v1/responses`，key 27，服务器本机）

| prompt | 输出量 | TTFT | 总时长 | 输出阶段耗时 |
| --- | --- | --- | --- | --- |
| 一句话讲海洋 | 62~67 词 | 4.11 / 4.69 / 6.43 s | 4.16 / 4.89 / 6.48 s | 0.05~0.2 s |
| 120 词讲海洋 | ~500 字符 | 23.88 / 28.42 s | 24.79 / 29.15 s | 0.7~0.9 s |

逐事件时间线显示，到 `response.created` 时已耗时 23.9s，第一个
`response.output_text.delta` 几乎同刻到达，之后 0.9 秒内全部输出完：

```
t=23.883  response.created
t=23.883  response.output_text.delta   chars=66     ← 首字 ≈ 23.9s
t=24.021  response.output_text.delta   chars=123
t=24.445  response.output_text.delta   chars=141
t=24.785  response.completed                        ← 结束
```

**TTFT 随输出量线性增长，且首字与结束几乎同期**，是典型的「上游生成完毕才整段返回」，
而非边生成边吐。sub2 记录的 5s 是短请求场景的真实值，与用户截图
（账号 34，20:59:10 / 20:59:17，首字 5266 / 5231 ms，总长 5340 / 5455 ms）一致。

### 顺带发现的计时口径差异（已知，不影响本条结论）

两条路径对「首字」定义不同：

- `/v1/messages` 路径（`antigravity_gateway_upstream.go:260`）：
  `if firstTokenMs == nil && len(line) > 0` —— **任意非空行**即计首字，
  连 `event: message_start` 这类元数据行也算
- `/v1/responses`、`/v1/chat/completions` 路径
  （`antigravity_gateway_compat_stream.go:246-264`）：要求 `isMeaningfulAntigravityCompatEvent`
  判定为真有文本（`Text`/`Thinking`/`PartialJSON`/`tool_use`），口径正确

用户截图走 `/v1/responses`，口径正确。

## 七、缓存成本影响（结论：中性至略优）

删除自映射后裸名会按 thinking 档位解析成不同变体，理论上存在「同会话内变体切换导致上游
缓存前缀失效」的风险。实测排除：

- **裸名从未成功透传**：全历史 4 个反重力账号的 `upstream_model` 中，
  `gemini-3.8-flash`（裸名）记录数为 **0**；只有 `-low` 4398、`-high` 4372、
  `-medium` 381 条。自映射存在期间请求要么 404，要么走变体解析，因此**不存在
  「缓存被拆散、重复读取」**
- 变体解析是确定性的：同一 thinking 配置恒得同一变体
  （`geminiThinkingLevelFromBody` / `geminiThinkingLevelFromClaudeThinking` 阈值一致）
- 抖动稀少：7 天内 22 次，其中唯一密集的一组（10-23 22:15，8 条）全部为
  345 token / 个位数输出，属探针请求

## 八、备份与回滚

服务器 `/home/ubuntu/` 下：

| 文件 | 内容 |
| --- | --- |
| `ag61_mapping_backup_20261029-201110.json` | 变更前 `model_mapping`（66 键） |
| `ag61_credentials_backup_20261029-201110.json` | 变更前完整 `credentials` |
| `ag61_mapping_backup_20261029-195645.json` | 更早一次实验前快照 |

两个备份文件均已验证为合法 JSON，且 `model_mapping` 各含 66 键。

### 回滚方式 A：只恢复 3 个裸名键（推荐，最小改动）

已实测：63 键 + 这 3 键 = 66 键，与变更前一致。

```bash
cd /home/ubuntu/sub2api
PW=$(grep -E '^POSTGRES_PASSWORD=' .env | cut -d= -f2-)
sudo docker exec -e PGPASSWORD="$PW" sub2api-postgres psql -U sub2api -d sub2api -c "
UPDATE accounts
SET credentials = jsonb_set(credentials, '{model_mapping}',
      (credentials->'model_mapping') || '{
        \"gemini-3.6-flash\": \"gemini-3.6-flash\",
        \"gemini-3.7-flash\": \"gemini-3.7-flash\",
        \"gemini-3.8-flash\": \"gemini-3.8-flash\"
      }'::jsonb, true),
    updated_at = NOW()
WHERE id = 61;
"
```

> 注意：回滚会**复现 404**。仅在需要恢复原始状态做对照时使用。

### 回滚方式 B：按备份整体恢复 `credentials`

备份文件在宿主机上，需先拷入容器再读取（避免 bash 与 psql 的双重转义）：

```bash
cd /home/ubuntu/sub2api
PW=$(grep -E '^POSTGRES_PASSWORD=' .env | cut -d= -f2-)
sudo docker cp /home/ubuntu/ag61_credentials_backup_20261029-201110.json \
  sub2api-postgres:/tmp/ag61_restore.json
sudo docker exec -e PGPASSWORD="$PW" -i sub2api-postgres \
  psql -U sub2api -d sub2api -v ON_ERROR_STOP=1 <<'SQL'
UPDATE accounts
SET credentials = pg_read_file('/tmp/ag61_restore.json')::jsonb,
    updated_at  = NOW()
WHERE id = 61;
SQL
```

> `pg_read_file` 需超级用户；容器内的 `sub2api` 角色通常即为该库 owner。
> 若报权限错误，改用方式 A，或直接对 JSON 做文本处理后再 `UPDATE`。
> 恢复后务必确认 `pg_read_file` 读到的是完整 JSON（先 `SELECT length(pg_read_file(...))`）。

两种方式任选其一，执行后建议清一次调度快照缓存，让新映射立即生效：

```bash
sudo docker exec sub2api-redis redis-cli --scan --pattern 'scheduler:*' | head
```

## 九、遗留风险与后续建议（均未执行）

1. **问题会复发** —— `CreateAccountModal.vue` 仍会把整张默认映射表写进
   `credentials.model_mapping`。下次新增反重力账号会再次踩同一个 404。
   治本改法：建号时不落库该表，交由后端 `ensureAntigravityDefaultPassthroughs`
   运行时注入。
2. **退役模型仍在默认表** —— `DefaultAntigravityModelMapping`
   （`domain/constants.go:163-179`）与 `ensureAntigravityDefaultPassthroughs`
   （`account.go:664-689`，其中 163/169/175 行为 3.6/3.7/3.8 裸名自映射，
   684 行为 `gemini-3.8-flash`）里仍保留 `gemini-3.6-flash*`、`gemini-3.7-flash*`
   全套变体以及 `gemini-3.8-flash` 裸自映射，建议清理。
3. 61 号 `model_mapping` 中 3.6/3.7 的带后缀键仍在（本次只删裸名），
   对应模型上游已死，客户端请求会拿到 404。

## 十、诊断脚本

位于 `_recon/`（已被 `.gitignore:170` 忽略，不入库），可复用者：

| 脚本 | 用途 |
| --- | --- |
| `ag_pool_now.sql` | 反重力号池与分组快照 |
| `ag_mapping_diff.sh` | 账号映射与默认表差异 |
| `ag_bare_matrix.sh` | 裸名 vs 变体矩阵测试 |
| `ag_upstream_truth.sh` | 上游模型目录真值探测 |
| `ag_speedtest.py` / `ag_chunk_timeline.py` | 客户端 TTFT 与逐事件时间线测量 |
| `ag_cache_cost*.sh` | 缓存命中率按账号/变体拆解 |
