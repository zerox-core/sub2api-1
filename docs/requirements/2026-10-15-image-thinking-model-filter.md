# 需求梳理：生图路由 / 思考挡位 / 模型能力过滤

日期：2026-10-15
状态：**待用户确认**（本文档只梳理，不含实现）

---

## 一、现状核查（事实基线，均来自生产库与代码）

### 1.1 生图（图片生成）

分组级开关与计费字段（`groups` 表）：`allow_image_generation`、`image_rate_independent`、
`image_rate_multiplier`、`image_price_1k/2k/4k`（**全部为 NULL**）、`model_routing`。

| 分组 | 定位 | `allow_image_generation` | 生图账号来源 | model_routing |
| --- | --- | --- | --- | --- |
| 23 反重力 (Copy ) | **gemini 自用**（独占，key 27） | 是 | 40 丽珠(组内优先级 10) → 5 zyuou(50) 兜底 | 无 |
| 12 反重力 | **gemini 对外公开**（key 54） | 是 | 强制 `gpt-image-*` → 账号 5「zyuou-生图原价」 | `{"gpt-image-*":[5]}` |
| 11 claude-生图 | **专门生图组**（无 key，纯供给池） | 是 | 5 zyuou | `{"gpt-image-*":[5]}` |
| 5 国模 | 国产 LLM | **是（需关）** | 组内无生图账号 | `{"gpt-image-*":[5]}`（routing 已关闭） |
| 14 免费快快的薅羊毛 | 羊毛池 | **是（需关）** | 43 百炼 | 无 |
| 8 丽珠组 | 自用丽珠直连 | 是 | 40 丽珠 | 无 |
| 15 大羊毛 | — | 否 | — | 无 |

关键结论：

1. **你要的形态其实已基本成立**：`model_routing` 的 `[5]` 是**账号 ID 列表**（`service/group.go:245`
   `GetRoutingAccountIDs`），所以分组 12 的公开生图确实被强制到"生图账号"并按时 `image_rate_multiplier=1.2` 计费；
   分组 23 没有 routing，靠 `account_groups.priority`（40→10、5→50）实现"丽珠优先、zyuou 兜底"。
2. `image_price_1k/2k/4k` 全空 → 目前"收费"只体现为**倍率**，不是固定单价。
3. 分组 11「claude-生图」没有任何 API key，也没有被 12/23 的 `fallback_group_id` 引用
   （两者均为 0）→ 它是"隐式备用"（因为账号 5 同时挂在 12/23 里），**不是分组级 fallback**。
4. 分组 23 的 `gpt-image-1`/`gpt-image-1.5` **只有账号 5 提供**，丽珠 40 不提供这两个模型，
   所以这两个模型无法"丽珠优先"。

### 1.2 思考挡位（reasoning effort）

- 平台支持面：**只有 anthropic / openai / composite 三个平台能配置分组级思考策略**
  （`service/openai_reasoning_effort_policy.go:139-148` `reasoningEffortValuesForPlatform` 对其余平台返回 nil，
  `:155-163` 直接报错）；前端 `frontend/src/views/admin/groupsReasoningEffort.ts:47-51` 同步限制。
- `max_reasoning_effort` 的语义是**上限，空 = 不限制**（`handler/dto/types.go:154`）；
  当前**全部分组都是空**，`max_reasoning_effort_over_limit` 全是 `downgrade`。所以国模组并非被限制。
- 实际使用情况（近 7 天 `usage_logs`）：`high` 3417 次、`medium` 252、`low` 241、`xhigh` 52；
  另有 5369 次未带 effort。**分组 4/5（国模）的调用 effort 全为 NULL**。
- Antigravity（gemini）链路：上游模型目录**只有带后缀的变体**
  （`gemini-3.8-flash` + `-low/-medium/-high/-tiered`），裸名会被上游 404
  （`service/antigravity_gemini_thinking_variant.go:10-14` 注释明确）。
  sub2api 其实**已经能把请求里的挡位解析成对应后缀 id**：`resolveGeminiThinkingVariantForLevel`
  （`:124-171`）+ `getMappedModelForThinkingLevel`（`antigravity_gateway_service.go:313-323`），
  调用点 5 处（gemini/claude/compat/count_tokens）。
- 缺口：
  - 分组级挡位策略**对 gemini/antigravity 关闭**（见上），所以前端没有"思考强度"面板；
  - 原生 `/v1beta`（Gemini）路由**不记录** `requested_reasoning_effort`；
  - 缺省（无 thinkingConfig）时 Antigravity 默认走 `high`（`antigravity_gemini_thinking_variant.go:52-58`）；
  - 国产 OpenAI 兼容上游（Kimi/GLM/Qwen/MiniMax 的 thinking 型号）代码只"记录" effort，
    **不会主动开启上游 thinking**（`gateway_request.go:1571-1581` `DefaultEffortForThinkingEnabled`）。
- 风险（实现时必须处理）：
  - 账号 `credentials.model_mapping` 里显式写了裸名会**静默压过**挡位推导（`:149-153`）；
  - 兜底顺序 `[preferred, high, medium, low, tiered]`（`:158-163`）会让请求 `low` 时**静默升到 high**（多计费）；
  - 挡位同时通过"模型 id"和"thinkingConfig"表达，两者可能不一致；
  - 换 id 会影响按模型计费的限流/配额与 `reasoning_effort_multipliers` 计费；
  - `gemini-3.x-flash-tiered` 是能力/价格档，**不是第四个挡位**。

### 1.3 账号模型是怎么"扫描"出来的

- `POST /admin/accounts/:id/models/sync-upstream` → `service/upstream_models.go:210`
  `SyncUpstreamModelCatalog` → 直接请求上游 **`GET {base}/v1/models`**（`:1011,1033,1217`），
  平台不同端点不同（OpenAI OAuth 用 `chatgpt.com/backend-api/codex/models`，Gemini 用 `/v1beta/models`）。
- 拉回来的结果**只写 `accounts.extra.upstream_model_metadata`**（`:297`），
  **不会写 `credentials.model_mapping`**。
- 是否"真的能用"没有任何校验：`account.go:860-883` `IsModelSupported` 的规则是
  "透传 → 放行；**mapping 为空 → 放行一切**；mapping 非空 → 精确键或尾部 `*` 通配符命中"。
  即 **model_mapping 是"声明"而非"验证过的能力"**。
- 现有探测只有账号级、与具体模型无关：`openai_responses_supported`（一次 `POST /v1/responses`）、
  `openai_compact_supported`、`upstream_billing_probe`。Anthropic/Gemini/copilot 类 API-key 账号**完全没有探测**。
- 对外可见性：`gateway_service.go:1400,1452-1463` 会把"可调度账号的 mapping 键"并集后
  当作 `/v1/models` 返回，所以**声明即对外可见**——这就是"账号权限不支持却能被扫出来/列出来"的直接原因。
- 前端 `frontend/src/components/account/ModelWhitelistSelector.vue`：候选项是**硬编码清单**
  （`useModelWhitelist.ts:8-280`），仅按平台过滤；`fillRelated`（`:280-290`）≈ 全选本平台模型；
  `syncUpstreamModels`（`:292-351`）把上游所有 id 合并进来，**无任何能力过滤**。

### 1.4 分组选模型 UI 的现状

`frontend/src/views/admin/GroupsView.vue`（创建面板 864-912 行，编辑面板同构）：

- 列表 = `state.items`，每行一个 checkbox + 上移/下移按钮；**没有搜索框、没有按能力/类型筛选、没有排序控件**；
- 有"全选"按钮（`selectAllModelAllowlistItems`，803/2491 行）——它作用于**全部候选项**，
  没有"按当前筛选全选"的概念；
- 顺序 = 候选项顺序，可用上下箭头手工调整（这就是"排序逻辑不知道"的来源）；
- 分组 14 的白名单有 **189 个模型**，靠 checkbox 一个个点确实不可用。

---

## 二、需求清单

### R1 生图路由与计费（现状已接近目标，做确认与收口）

- 目标：gemini 自用（23）→ 丽珠账号优先、专门生图组兜底；gemini 公开（12）→ 生图组 + 收费。
- 待办：
  1. 明确"专门生图组"是**分组 11** 还是"账号 5 这条供给线"；若要分组级兜底，需配
     `fallback_group_id`（当前 12/23 都是 0）。
  2. 明确"收费"口径：继续用 `image_rate_multiplier`（1.2×），还是补 `image_price_1k/2k/4k` 固定单价。
  3. 补 `gpt-image-1/1.5` 的丽珠来源（或确认这两个模型就该只走 zyuou）。
  4. 决定分组 8（自用丽珠组）是否也要同样的路由策略。

### R2 国模 / 薅羊毛关闭生图，但保留多模态

- 目标：分组 5、14 不提供生图；视觉理解（图片输入）能力保留。
- 待办：
  1. 分组 5：`allow_image_generation: true → false`；清掉 `model_routing` 里的 `gpt-image-*`
     （白名单本身无生图模型）。
  2. 分组 14：`allow_image_generation → false`；从白名单移除 **12 个生图模型**
     （`qwen-image-2.0*` 系列 9 个、`wan2.7-image`、`wan2.7-image-pro`、`z-image-turbo`）
     与 **8 个语音模型**（`MiniMax/speech-*` 4 个、`fun-asr-flash-*`、`qwen-audio-3.0-asr-flash`、
     `qwen3-asr-flash-realtime`、`qwen3-tts-flash`）——**语音是否保留需确认**。
  3. 保留视觉理解模型（如 `qwen3-vl-plus`、`qwen-vl-ocr*`、`qwen3.5-ocr`、`qwen3.8-omni-flash`）。
  4. 15（大羊毛）已是关闭状态，无需处理。

### R3 Gemini 思考挡位（把"换 id"变成"挡位"）

- 目标：同一个模型 id + 思考挡位参数即可切换，分组里能配挡位策略。
- 实现路径（推荐 A）：
  - A. 把 antigravity/gemini 纳入 `reasoningEffortValuesForPlatform` 白名单，
    统一客户端传来的 `reasoning_effort` / `thinkingLevel` / `thinking.budget_tokens` →
    挡位 → 复用现有 `resolveGeminiThinkingVariantForLevel` 选后缀 id；
    前端 `groupsReasoningEffort.ts` + `ReasoningEffortPolicyFields.vue` 放开平台限制。
  - B.（可选，改动大）在 `pkg/antigravity/gemini_types.go:79-83` 的 `GeminiThinkingConfig`
    增加 `ThinkingLevel` 字段并透传上游。
- 必须同时处理：原始 Gemini 路由的 `requested_reasoning_effort` 记录
  （`CanonicalRequestedReasoningEffort` 读 `thinkingConfig.thinkingLevel`）、
  兜底顺序不得把低挡位静默升到 high、挡位与计费的 `reasoning_effort_multipliers` 保持一致、
  `-tiered` 不当作挡位。

### R4 国模等支持思考的分组"打开思考"

- 目标：DeepSeek / GLM / Qwen 等思考型号能真正开启思考（而非只记录 effort）。
- 现状：这几个分组 `max_reasoning_effort` 为空（= 不限制），调用里 effort 全 NULL；
  代码只记录 effort，不主动给上游加 thinking 参数。
- 待办：确认"打开"的语义（三选一，需用户定）：
  - (a) 允许客户端用 `reasoning_effort` 切换（透传 + 记录）；
  - (b) 默认就给这些型号带上 thinking 参数（`enable_thinking` / `thinking.type`）；
  - (c) 两者都要，并按分组配置默认挡位。

### R5 账号层：模型真实支持性过滤

- 目标：导入/同步时只保留"该账号真能用"的模型；不被上游 `/v1/models` 的全量清单带偏。
- 实现选项（需用户选）：
  - **被动学习（推荐起步）**：保留扫描结果，但当某模型首次返回 404/403/无效时，
    自动写入账号级"不可用模型"名单（`extra` 或新增字段），
    并在 `IsModelSupported` 里扣除；管理端可见/可清理。
  - **主动探测**：对 mapping 里的模型逐个发最小请求（成本高，需限速与配额保护），
    结果落 `extra.upstream_model_metadata`（扩展为带 `verified` 字段）。
  - **导入时确认**：sync-upstream 后弹出勾选面板，默认全不选或按元数据预筛。
- 涉及改动面：`upstream_models.go:210`（同步漏斗）、`account_handler.go:2800`（可用模型）、
  `account.go:860`（`IsModelSupported`，尤其"空 mapping = 放行一切"）、公开列表
  `gateway_service.go:1400`；写入路径 `admin_account.go:650`。

### R6 分组层：模型能力（模态）过滤

- 目标：分组选模型时可按能力过滤（LLM / 视觉 / 生图 / 语音 / 视频 / 嵌入），
  从"能选什么"这一步就挡住不该出现的模型。
- 现状：不存在任何能力分类字段；模态只能靠模型名正则猜。
- 待办：建立**模型能力目录**（模型名 + 模态 + 是否 reasoning + 是否支持图片输入），
  来源优先级：上游元数据（`models.dev`，同步时已抓 `upstream_model_metadata`）→ 内置规则 → 人工覆盖；
  在分组 5/14 这类"不该有生图"的分组上提供"一键按模态过滤"。

### R7 分组选模型 UI 重做

- 目标：搜索 + 能力筛选 + 批量选择 + 明确的排序规则。
- 待办（`GroupsView.vue` 创建/编辑两个面板）：
  1. 候选区加搜索框 + 模态筛选 chips（复用 R6 的能力目录）；
  2. "全选"= 选中**当前筛选结果**，"反选/清空"同理；
  3. 已选区显示排序规则并支持：按名称排序、按模态分组、拖拽/箭头调整，
     并明确"保存顺序 = 展示顺序"；
  4. 对 189 个模型的超大列表做虚拟滚动或分页；
  5. 显示来源账号与模态徽章（已有来源徽章 `model-allowlist-sources`，可扩展模态）。

---

## 三、待确认问题（阻塞实现）

1. "专门生图组"指分组 11「claude-生图」，还是账号 5 这条供给线？是否需要配置分组级
   `fallback_group_id`（否则备用只是账号级兜底）。
2. 生图收费口径：继续 1.2× 倍率，还是要固定单价（`image_price_1k/2k/4k`）？
3. 分组 14 的 8 个**语音**模型是否也要移除？（你只说了"生图"，语音/ASR 需要明确）
4. 分组 8（自用丽珠组）是否也要按 R1/R2 重新配置？
5. R3 的调用方式：客户端是走 OpenAI 兼容 `reasoning_effort`，还是 Gemini 原生
   `generationConfig.thinkingConfig.thinkingLevel`？（决定实现路径 A 还是 A+B）
6. R4"打开思考"的语义：(a) 允许挡位切换 / (b) 默认带 thinking 参数 / (c) 两者都要？
7. R5 的判定方式：被动学习 / 主动探测 / 导入时确认？（主动探测有成本）
8. R5 举例里的 "copilot"：是哪个账号、在哪个界面看到的模型？（账号 45「Copilot直连」的映射
   只有 3 个模型，与"扫描出很多"不符，需要定位具体场景）

---

## 四、建议实施顺序

| 批次 | 内容 | 理由 |
| --- | --- | --- |
| P0 | R2（关生图）+ R1 收口确认 | 纯配置，立刻消除"国模/羊毛组能生图" |
| P1 | R7 分组选模型 UI（搜索/筛选/全选） | 立刻降低日常操作成本，且为 R6 铺路 |
| P2 | R6 模型能力目录 + R5 被动学习 | 数据层能力，相互依赖 |
| P3 | R3 Gemini 挡位 + R4 国模思考 | 改动最深，需回归测试计费与限流 |
| P4 | R5 主动探测（可选） | 有上游成本，视 P2 效果决定 |
