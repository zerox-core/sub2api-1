# 需求梳理：生图路由 / 思考挡位 / 模型能力过滤

日期：2026-10-15
状态：**4 项决策已确认（见第三节）；P0（国模/羊毛组关生图）已于 2026-10-15 执行并验证（见第五节）**

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
| 5 国模 | 国产 LLM | **否（2026-10-15 已关）** | 组内无生图账号 | `{"gpt-image-*":[5]}`（routing 已关闭） |
| 14 免费快快的薅羊毛 | 羊毛池 | **否（2026-10-15 已关，189→154）** | 43 百炼 | 无 |
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

### R2 国模 / 薅羊毛关闭生图，但保留多模态 —— **已执行（P0）**

- 目标：分组 5、14 不提供生图；视觉理解（图片输入）能力保留。
- 结果：分组 5、14 的 `allow_image_generation` 已置 `false`；分组 14 白名单 189 → **154**，
  移除 **35 个**非文本模型（12 生图 + 23 语音/实时类），保留 11 个多模态模型。
  分组 5 白名单本就只有 4 个 LLM，未改动。详见第五节。
- 排期说明：分组 5 的 `model_routing` 里虽留有 `{"gpt-image-*":[5]}`，但
  `model_routing_enabled=false` 使其不生效，故本次未改动该字段。

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

## 三、决策记录与剩余待确认

### 已确认（2026-10-15）

| # | 问题 | 决策 |
| --- | --- | --- |
| D1 | 分组 14 的语音模型 | **连语音一起移除**（含 `-realtime`、`livetranslate`、`s2s`、`audio-*` 实时类，以及 TTS/ASR） |
| D2 | 生图收费口径 | **继续用倍率**（公开组 1.2×、自用 1.0×），不引入 `image_price_*` 固定单价 |
| D3 | 国模"打开思考"语义 | **只走挡位切换**：端侧自带思考挡位选择，网关透传/记录并按挡位计费，**不**主动注入 thinking 参数（2026-10-15 用户确认；实测已可用） |
| D4 | 账号层真实支持性过滤 | **被动学习**：某模型 404/403/无权限时自动拉黑（管理端可见、可清理） |

### 仍需确认

1. "专门生图组"指分组 11「claude-生图」，还是账号 5 这条供给线？是否需要配置分组级
   `fallback_group_id`（否则"备用"只是账号级兜底，见 1.1 第 3 点）。
2. 分组 8（自用丽珠组）是否也要按 R1/R2 重新配置。
3. R3 的调用方式：客户端走 OpenAI 兼容 `reasoning_effort`，还是 Gemini 原生
   `generationConfig.thinkingConfig.thinkingLevel`？（决定实现路径 A 还是 A+B）
4. R5 举例里的 "copilot"：具体是哪个账号、在哪个界面看到"扫描出很多不支持的模型"？
   （账号 45「Copilot直连」的映射只有 3 个模型，与描述不符，需要定位具体场景）
5. R6 能力目录来源：是否接受以 `models.dev` 元数据 + 内置规则为准，并允许人工覆盖。

---

## 四、建议实施顺序

| 批次 | 内容 | 理由 |
| --- | --- | --- |
| P0 | R2（关生图）+ R1 收口确认 | 纯配置，立刻消除"国模/羊毛组能生图" |
| P1 | R7 分组选模型 UI（搜索/筛选/全选） | 立刻降低日常操作成本，且为 R6 铺路 |
| P2 | R6 模型能力目录 + R5 被动学习 | 数据层能力，相互依赖 |
| P3 | R3 Gemini 挡位 + R4 国模思考 | 改动最深，需回归测试计费与限流 |
| P4 | R5 主动探测（可选） | 有上游成本，视 P2 效果决定 |

---

## 五、已执行变更

### 2026-10-15 P0：国模 / 羊毛组关闭生图

| 对象 | 变更前 | 变更后 |
| --- | --- | --- |
| 分组 5「国模」 | `allow_image_generation=true` | `false`（白名单 4 个 LLM 不变） |
| 分组 14「免费快快的薅羊毛」 | `allow_image_generation=true`，白名单 189 | `false`，白名单 **154** |

- 移除的 **35 个** = 12 个生图（`qwen-image-*` 9 个、`wan2.7-image`、`wan2.7-image-pro`、`z-image-turbo`）
  + 23 个语音/实时类（`MiniMax/speech-*` 4 个、ASR/TTS 4 个、
  `*-realtime` / `livetranslate` / `s2s` / `audio-*-realtime` 15 个）。
- **保留的多模态（11 个）**：`qwen3-vl-plus`、`qwen-vl-ocr`、`qwen-vl-ocr-2025-11-20`、`qwen3.5-ocr`、
  `vanchin/deepseek-ocr`、`qwen3-omni-flash`、`qwen3.5-omni-flash`、`qwen3.5-omni-flash-2026-03-15`、
  `qwen3.5-omni-plus`、`qwen3.5-omni-plus-2026-03-15`、`qwen3.8-omni-flash`。
- 验证（变更后实测）：
  - 分组 14 `/v1/models`（key 55）→ HTTP 200，**153** 个模型，生图/音频命中 **0**；
  - 分组 5 `/v1/models`（key 12）→ HTTP 200，3 个模型，生图命中 **0**；
  - 生图请求 → `404 model_not_found`：`Model "qwen-image-2.0" is not available for this group`。
- 执行前核对：分组 14 近 30 天实际调用的模型（`auto`、`glm-5.3`、`sensenova-6.8-flash-lite`、
  `qwen3.8-omni-flash`、`deepseek-*`、`kimi-k3` 等）**没有一个在移除清单内**。
- 备份：`docs/ops/backup-groups-5-14-20261015.json`（变更前两组的完整配置，含 `model_allowlist`、
  `allow_image_generation`、`model_routing`）。
- 回滚：按备份中的原值 `PUT /api/v1/admin/groups/{5,14}` 即可。

### 2026-10-15 P1：羊毛生图组 / 羊毛语音组建立与接入

按用户要求以"建组"方式落地：

| 对象 | 变更 |
| --- | --- |
| 新建分组 **24「羊毛组生图」** | openai、`is_exclusive=true`、`rate_multiplier=1.0`、`allow_image_generation=true`、白名单 **12 个免费生图模型** |
| 新建分组 **25「羊毛语音组」** | openai、`is_exclusive=true`、`rate_multiplier=1.0`、白名单 **23 个语音模型**（TTS/ASR/实时语音/实时翻译） |
| 分组 5 国模 | 白名单 4 → **16**，`allow_image_generation=true`，`model_routing` = `{"qwen-image-*":[43],"wan2.7-image*":[43],"z-image-turbo":[43]}`（enabled） |
| 分组 14 羊毛 | 白名单 154 → **166**，同上 |
| 分组 15 大羊毛 | 白名单 17 → **25**，同上 |
| 账号 43 百炼 | 绑定分组 [14, 15, 24, 25] |
| 用户 1 | `user_allowed_groups` 授权 24、25 |

**12 个生图模型与 23 个语音模型全部由账号 43「百炼(阿里云)」独家声明。**

#### ⚠️ 阻断发现：分组 24/25 目前没有可用调用通道

实测（直连百炼，绕过网关）：

| 端点 | 结果 |
| --- | --- |
| `POST /compatible-mode/v1/images/generations`（qwen-image-2.0 / z-image-turbo） | **404**，兼容模式不提供该端点 |
| `POST /compatible-mode/v1/audio/speech`、`/audio/transcriptions` | **404** |
| `POST /api/v1/services/aigc/multimodal-generation/generation`（qwen-image-2.0 / qwen-image-3.0） | **200**，返回图片 URL |
| 同上（qwen3-tts-flash） | **200**，返回音频 URL |

而 sub2api 网关侧的入站校验只认 `gpt-image-*` / `grok-imagine*` / `gemini-*-image`
（`service/openai_images.go:478-503`），转发也只走上游 OpenAI 兼容路径，**不支持百炼原生
`multimodal-generation` 协议**。因此分组 24/25 建好但暂时无法被调用。

→ 这也是"账号扫出一堆用不了的模型"的根因之一：百炼 `/v1/models` 列出 177 个模型，
其中生图/语音类**不在兼容端点提供**，光看模型清单无法分辨 → 强化了 R5（账号层真实支持性）
与 R6（按模态过滤）的必要性。

#### ✅ R4 验证通过：国模思考挡位切换可用

分组 5 用 key 12 发 `POST /v1/chat/completions`，`model=glm-5.3-flash` +
`reasoning_effort=high` → HTTP 200，上游返回 `reasoning_content`，
`usage_logs` 记录 `reasoning_effort=high` / `requested_reasoning_effort=high`；
不带 effort 的对照请求记录为 `-`。**无需改代码**。

- 备份：`docs/ops/backup-groups-5-14-15-before-image-relink.json`
- 备注：分组 5 当前唯一可调度账号是 41「WorkBuddy直连」（账号 42 火山仍在 403 冷却）。

### 2026-10-15 P2：百炼原生适配实现（已上线并验证）

用户决策：**给网关加百炼原生适配**。实现（commit `b23139de0` + `6bb0fce07`）：

| 位置 | 改动 |
| --- | --- |
| `service/openai_images_dashscope.go`（新增） | 账号 base_url 主机为 `dashscope[.intl].aliyuncs.com` 时，把 OpenAI images 请求翻译成原生 `multimodal-generation` 请求体，再把原生响应（`output.choices[].message.content[].image`）改写成 OpenAI images 响应，之后复用既有收尾（b64 回填 / 图片计数 / 计费） |
| `service/relay_audio_dashscope.go`（新增） | 同思路的语音合成：构建原生 TTS 请求体 → 下载返回的 OSS 音频 → 以正确 Content-Type 写回；计费仍按字符数 × 模型价格比（`AudioUsage{Mode:"tts"}`） |
| `service/openai_images.go`（+8 行） | `validateCompatibleImagesModel` 放行百炼生图模型族（`qwen-image*` / `wan2.7-image*` / `wanx*` / `z-image*`）；`forwardOpenAIImagesAPIKey` 入口分流到原生适配 |
| `service/relay_audio.go`（+4 行） | `ForwardRelayAudioSpeech` 入口分流到原生 TTS 适配 |

分流只按**账号 base_url 主机**判定，其它 OpenAI 账号的既有路径不变。

#### 联调中发现并修复的两个缺陷（`6bb0fce07`）

1. **字段名**：本网关 audio relay 读 `text`（audio-proxy 约定），而 OpenAI SDK 发 `input`。
   只发 `input` 的请求会拿到空文本。适配器现在两者都接受（`text` 优先）。
2. **静默 200**：audio handler 在 `result == nil` 时直接 return 而**不写响应**，
   于是适配器自身的错误（如空文本）表现为**空的 HTTP 200**。适配器现在自己写
   OpenAI 风格错误体，失败可见为 400/502。

#### ✅ 上线验证（镜像 `sub2api-ag-quota:dashscope-native2-20261015`）

| 场景 | 结果 |
| --- | --- |
| 分组 24 生图 `qwen-image-2.0` | **200** + 图片 URL（2048×2048），usage 记录 `image_count=1` |
| 分组 14 生图 `z-image-turbo` | **200** + 图片 URL（1024×1536） |
| 分组 25 配音 `qwen3-tts-flash`（`text` 字段） | **200**，`audio/x-wav`，103 724 字节（RIFF/WAVE 头正确） |
| 分组 25 配音（`input` 字段，OpenAI SDK 写法） | **200**，76 844 字节 |
| 空文本容错 | **400** + `invalid_request_error`（不再是空 200） |
| 网关日志 | `adapter=dashscope-native`（生图 / 配音各一行，含 voice 与字符数） |

已为用户 1 建好两个网关 Key：**id 57 → 分组 24「羊毛组生图」，id 58 → 分组 25「羊毛语音组」**
（密钥页可直接复制）。

#### 遗留

1. `relay_audio_dashscope.go` 目前把音频 URL 全量读入内存后写回；
   若后续出现大音频，需要改为流式透传（`writeGrokMediaContentResponse` 已具备该形态）。
2. R1 / R3 / R5 / R6 / R7 仍未实现（后续进展见 P4：R3 已完成、R6 落地雏形、
   R1 配置到位但卡在上游渠道）。

### 2026-10-15 P3：mp3 输出 + 分组 5 生图打通（用户决策后）

用户两项决策：**「1 允许配置生图」**、**「2 需要 mp3」**。

#### ① 分组 5「国模」生图打通

账号 43 之前未绑定分组 5，`model_routing` 里指向 43 的规则在该组无效。
现执行 `INSERT INTO account_groups (account_id, group_id) VALUES (43, 5)`，
账号 43 绑定关系变为 5/14/15/24/25，分组 5 的 12 个生图模型随之可用
（用户确认**允许这些组配置生图**，故不再移除生图模型）。

验证（key 12 → 分组 5）：`z-image-turbo` **200**（1024×1536）、`qwen-image-2.0` **200**（2048×2048），
usage 记 `group_id=5, account_id=43, image_count=1`。

#### ② 配音 mp3 输出

先做参数探测，结论明确：**百炼 qwen3-tts-flash 只输出 WAV**——
`parameters.format` / `input.format` / 顶层 `format` / `parameters.response_format`
四种写法全部无效（下载后一律 `audio/x-wav` + RIFF 头）。官方文档
[Qwen-TTS API](https://help.aliyun.com/zh/model-studio/qwen-tts-api) 的请求体也只有
`model` / `input.text` / `input.voice` / `input.language_type`，没有格式参数。

因此改在**网关侧转码**：新增 `dashScopeWAVToMP3`，用纯 Go 的
[shine-mp3](https://github.com/braheezy/shine-mp3)（无需 CGO，也不必在运行镜像里装 ffmpeg）。
适配器按 `response_format`（缺省 mp3，OpenAI 约定）决定是否转码：

| `response_format` | 行为 |
| --- | --- |
| 缺省 / `mp3` | 转码为 MP3，`Content-Type: audio/mpeg` |
| `wav` | 原样返回百炼 WAV |
| 其它（`opus`/`aac`/…） | 无对应编码器，回退返回 WAV |

转码失败同样回退 WAV 并记 warning，不会让整个请求失败。

**踩到的两个坑（均已修，并有回归测试）**：

1. `EncodeBufferInterleaved` **每调用一次只产出一帧**（576 样本 ≈ 0.05 秒），必须用会循环的
   `Write`。最初误用它，导致 2 秒音频只转出 0.25 秒。现测试断言
   「帧数 == ceil(输入样本数 / 576)」（24kHz 单声道 128 kbps），覆盖 1/2/8/20 秒。
2. 百炼 WAV 的 **data chunk size 字段是坏值**（实测 `0x3FFFFFFD`，Python `wave`
   直接报出 `frames=1073741773`）。解析器已对 size 越界做兜底（按实际剩余字节读），
   否则取不到 PCM。

#### ✅ 验证（镜像 `sub2api-ag-quota:dashscope-native4-20261015`）

| 场景 | 结果 |
| --- | --- |
| 分组 5 生图 `z-image-turbo` / `qwen-image-2.0` | **200** + 图片 URL，usage 记 `image_count=1` |
| 配音缺省格式 | **200**，`audio/mpeg`，帧头 `ff f3`，`file(1)` 识别为 `MPEG ADTS, layer III, v2, 128 kbps, 24 kHz, Monaural` |
| 配音 `response_format=mp3` | **200**，`audio/mpeg` |
| 配音 `response_format=wav` | **200**，`audio/x-wav`（RIFF，不转码） |
| 时长一致性 | 同文本 mp3 7.37s vs wav 7.52s（差 0.15s，属帧对齐范围） |

> 过程说明：曾用「mp3 请求 / wav 请求各发一次」的方式比时长，出现过 1.69s 差异并一度
> 误判为截断。真实原因是**两次独立 TTS 调用本身的输出长度会波动**（同文本实测
> wav 341 804 → 361 004 字节，mp3 140 928 → 117 888 字节）。改用对同一份输入的
> 帧数断言复核后，确认转码既不截断也不重复。

#### 分组 5/14/15 生图配置状态

按用户决策保持现有配置：三组各 12 个生图模型 + `model_routing`
`{"qwen-image-*":[43],"wan2.7-image*":[43],"z-image-turbo":[43]}`（enabled），
账号 43 绑定 5/14/15/24/25 —— 五个组现在都能生图。

### 2026-10-15 P4：Gemini 思考挡位打通（R3）+ R1 收口 + 能力目录雏形

用户话术：「继续收尾后进行 gemini 的」。

#### ① R3 根因：挡位参数被记录了，却没影响路由

线上实测（修复前，key 54 → 分组 12，账号 34）：

```
22:20:27 | effort=NULL | upstream=gemini-3.8-flash-high
22:20:30 | effort=low  | upstream=gemini-3.8-flash-high   ← 应为 -low
22:20:32 | effort=high | upstream=gemini-3.8-flash-high
```

`usage_logs.requested_reasoning_effort` 如实记下了客户端意图，但上游始终跑
`gemini-3.8-flash-high`。根因在 `antigravity_gateway_compat.go`：

```go
mappedModel := s.getMappedModelForThinkingLevel(
    account,
    request.originalModel,
    geminiThinkingLevelFromClaudeThinking(claudeRequest.Thinking), // ← 只读 Claude thinking
)
```

挡位推导只看转换后的 `claudeRequest.Thinking`，而 OpenAI 风格客户端发的是
`reasoning_effort` —— 该字段其实早就被提取进 `antigravityCompatRequest.reasoningEffort`
（`extractCCReasoningEffortFromBody` / `ExtractResponsesReasoningEffortFromBody`），
只是**没有任何调用点读它**。于是所有挡位请求都落到默认 high。
这正是用户描述的「不能开思考能力的挡位，只能按思考能力换不同 id 调用」。

#### ② 修复

1. 新增 `geminiThinkingLevelFromReasoningEffort`：
   `none/minimal/low → low`、`medium → medium`、`high/xhigh/max → high`
   （Gemini 上游只有三档，没有"不思考"变体，故 none 取最浅一档；也没有比 high 更高的档）。
2. 新增 `geminiThinkingLevelFromAntigravityCompat`：**客户端显式 effort 优先**，
   未给时才回退到 Claude thinking 配置（保持原有 Claude 客户端行为不变）。
3. `prepareAntigravityCompatCall` 改用后者。
4. ~~分组级挡位策略放开 antigravity 平台~~ —— **已回退**。
   曾放开 `reasoningEffortValuesForPlatform`（后端）与 `groupsReasoningEffort.ts`（前端），
   让 antigravity 分组能保存 `max_reasoning_effort`。但实测发现**转发链没有应用该上限的执行逻辑**：

   ```
   分组 23 设 max_reasoning_effort=low，客户端发 reasoning_effort=high
   → HTTP 200，usage: effort=high | upstream=gemini-3.8-flash-high   ← 上限未生效
   ```

   即"能配置但不生效"的假功能，故前后端一并回退（`groups.max_reasoning_effort`
   保持空值，分组 12/23 已确认恢复）。
   **客户端挡位切换本身不受影响**——它走的是 §２ 的挡位推导，与分组上限无关。

   若后续要支持分组级上限，需要同时：
   - 覆盖 3 条转发链（`antigravity_gateway_gemini.go` / `_compat.go` / `_claude.go`）；
   - 实现 `max_reasoning_effort_over_limit` 的 deny 语义（超限返回错误而非静默降级）。

#### ✅ 验证（镜像 `sub2api-ag-quota:gemini-dial-20261015`）

| 客户端 `reasoning_effort` | 上游模型 | 结果 |
| --- | --- | --- |
| 未指定 | `gemini-3.8-flash-high` | 200 |
| `low` | **`gemini-3.8-flash-low`** | 200 |
| `medium` | **`gemini-3.8-flash-medium`** | 200 |
| `high` | `gemini-3.8-flash-high` | 200 |
| `minimal` | `gemini-3.8-flash-low` | 200 |
| `max` | `gemini-3.8-flash-high` | 200 |

6/6 HTTP 200，账号 34，`usage_logs` 的 `upstream_model` 与
`requested_reasoning_effort` 一一对应。

#### ③ 已知取舍：挡位变体缺失时仍会升档

`resolveGeminiThinkingVariantForLevel` 的兜底顺序 `[preferred, high, medium, low, tiered]`
会在请求 low 而 low 变体不存在时落到 high（按 high 计费）。
曾改为"只向下降档"，但既有测试
`TestResolveGeminiThinkingVariant/only_-high_exists:_low_request_degrades_to_high`
明确锁定了"宁可升档也不让请求失败"的语义，故**回退该行为变更**，并在代码注释中记录取舍；
若要改成绝不升档，需同时决定失败时客户端看到什么（404 还是降级提示）。

#### ④ R1 收口：配置已到位，卡点在上游渠道

分组 12（公开）白名单已补齐生图模型并清 `apikey:auth:*` 缓存，但请求仍 400。
**直连探测账号 5（zyuou）**结论：

| 请求 | 结果 |
| --- | --- |
| `{model,prompt,n}`（最小体） | **424** `image_generation_capacity_limited`（上游生图即时额度不可用） |
| `+ size` | 400 `The image request is invalid` |
| `+ response_format` | 400 同上 |
| `gpt-image-2.5` | 502 `Upstream service temporarily unavailable` |
| `gpt-image-1` | 404（zyuou 无此模型） |
| `GET /models` | 200，含 `gpt-image-2` / `2.5` / `2.5-flare` / `2.5-sunburst` |

同一个最小请求两次分别得到 424 与 400，说明 **zyuou 上游自身不稳定**：
生图额度不可用 + 参数校验前后不一致。这是**渠道侧问题**，网关配置无误
（分组 23 自用走丽珠账号 40 生图 200 正常）。

待用户决策：公开组（12）是否改用/追加可用的生图账号，还是维持现状等 zyuou 恢复。

#### ⑤ R6 能力目录雏形（`model_capability.go`）

为后续「账号层真实支持性过滤 / 分组层功能性过滤」打底，先落地纯函数分类器：
模型名 → 模态（text/vision/image/audio/video/embedding）+ 是否思考 + 挡位，
并把 `gemini-3.8-flash-high` 这类**挡位变体还原为 base + level**
（`BaseModel` / `ThinkingLevel`），使 UI 能把同一模型的挡位变体折叠展示。
另附 `ModelCapabilityFilter`（按模态过滤白名单、可排除挡位变体），
供分组层"一键按模态过滤"使用；`ModelCapabilityCatalog` 支持用
`accounts.extra.upstream_model_metadata` 覆盖内置判断。
6 个单测覆盖模态判定、挡位还原、过滤器与元数据合并优先级。

### 2026-10-15 P5：R5 账号层真实支持性过滤（被动学习）

用户决策 **D4 = 被动学习**：上游明确拒绝的模型自动记账，管理端可见、可清。

#### 为什么需要持久记账

已有 `RateLimitService.HandleUpstreamModelNotFound` 只做 **30 分钟临时冷却**
（per-(账号,模型) rate limit）：冷却到期或进程重启后同一个账号又会被选中，
而管理端也看不到"哪个账号到底吃不下哪些模型"。

#### 实现

1. **数据**（`account_unsupported_models.go`）：
   `accounts.extra.unsupported_models` =
   `{"<模型名>": {status, reason, count, first_seen, last_seen}}`，
   模型名归一化（小写、去 `models/` 前缀、去首尾空白）。
   - **阈值 2 次**才生效：避免一次偶发 404 就永久拉黑；
   - **TTL 7 天**内未再命中自动失效：给上游修复留恢复窗口；
   - 写入时顺带清理失效记录，避免 extra 无限增长。
2. **判定**：`Account.IsModelMarkedUnsupported`，在 `IsModelSupported` 的**最前面**判定
   —— 必须在透传短路之前，因为透传账号同样会遇到"上游根本没有这个模型"，
   而那正是 per-(账号,模型) 404 冷却被反复触发的来源。
   无该键时走零分配快速路径（`IsModelSupported` 在调度热路径上）。
3. **写入**：复用既有 `HandleUpstreamModelNotFound` 触发点，在设冷却的同时调用
   `persistUnsupportedModel` 落库，并同步进程内 extra 副本使本次判定立刻生效。
   `shouldSkipCodexPlanGatedImageModelCooldown` 的"端点错配"分支**不记账**，
   —— 那是用错端点，不是账号能力缺失。
4. **管理端**：账号响应本就返回 `extra`，清单直接可见；
   新增 `DELETE /api/v1/admin/accounts/:id/unsupported-models` 清空并放行。

#### ✅ 验证（镜像 `sub2api-ag-quota:r5-20261015`）

受控构造真实 404：账号 5（zyuou）的 mapping 含 `gpt-image-1`，而上游对它的
`/v1/images/generations` 稳定返回 404 model_not_found。临时把该模型加进分组 23 白名单后：

| 步骤 | 结果 |
| --- | --- |
| 第 1 次请求 | HTTP 502（上游失败且无备用账号），**`accounts.extra` 出现记录** |
| 记录内容 | `{"gpt-image-1": {"count": 1, "status": 404, "reason": "upstream_404_model_not_found", ...}}` |
| 第 2 / 3 次请求 | HTTP 503 `No available compatible accounts`（账号 5 已被排除） |
| 清理 | 白名单与 extra 记录均恢复（`group23_has_gpt_image_1=false`、`account5_extra=<cleared>`） |

> 说明：第 1 次 404 后 per-(账号,模型) 30 分钟冷却立即生效，第 2 次请求没再打到上游，
> 因此 `count` 停在 1（未达阈值）。端到端验证覆盖的是「404 → 记账 → 账号被排除 → 清理」；
> 「达阈值后由 `IsModelSupported` 持久排除（不依赖冷却）」由单测覆盖：
> `TestIsModelSupportedExcludesUpstreamRejectedModel`、
> `TestPersistUnsupportedModelWritesAccountExtra`、`TestUnsupportedModelRecordExpiresAfterTTL` 等 8 个用例。

#### 遗留

管理端 UI 尚未渲染该清单（数据已随账号详情返回，清除端点已就绪）。
