# 前端设计流水线（Cosmic 设计树）

本目录记录**生产面板前端（Cosmic 深色设计）的唯一源码树**、构建/发布方式，以及它的守卫与回滚路径。

> 生产站点：`https://api.zeroxcore.tech`（nginx site `sub2api-zeroxcore`，静态目录 `/var/www/sub2api-cosmic`）

## 1. 为什么设计不在这个仓库里

仓库 `frontend/` 是上游 `sub2api` 的前端，**不包含**本站在用的 Cosmic 深色设计。
线上设计的源码是 upstream `v0.2.11` 解包树 + 若干定制文件，位于本机：

| 位置 | 说明 |
| --- | --- |
| `F:\aily_scratch\fe_build\src\sub2api-0.2.11\frontend` | **设计树（唯一源码）**；`vite.override.config.ts` 单包构建 |
| `F:\backups\sub2api-design-20261015\frontend-design-src` | 移植前的纯净快照（回退基线） |
| `F:\backups\sub2api-design-20261015\frontend-design-src-ported_*` | 移植后的快照 |
| `/var/www/sub2api-cosmic*` | 生产静态目录及其历次备份 |

关键设计文件：`tailwind.config.js`（Cosmic 调色板 `#07080C/#0A0C12/#0D0F16/#0E1118/#131620/#EF4343`）、
`src/components/common/CosmicBackground.vue`、`index.html`（深色主题引导脚本，见 §4）。

## 2. 移植补丁（本目录 `port_all_cosmic_20261015.patch`）

补丁 = 「设计树（纯净） → 设计树（含本站功能）」的全部源码差异，共 10 个文件：

| 文件 | 内容 |
| --- | --- |
| `src/views/admin/GroupsView.vue`、`src/api/admin/groups.ts` | 模型白名单编辑器：每个模型条目显示**来源账号后缀**（`账号名#id`），并提示组内未映射（透传）账号 |
| `src/i18n/locales/{zh,en}/admin/overview.ts` | 上述新增文案（`sourceTooltip` / `unmappedHint`） |
| `src/components/account/AntigravityQuotaCell.vue`（新增）、`src/api/admin/accounts.ts`、`src/views/admin/AccountsView.vue` | 账号列表「容量」列的反重力配额徽章（G/C 周 + 5h 剩余百分比、按阈值着色、5 分钟轮询、强制刷新按钮） |
| `src/components/modelPlaza/ModelPlazaContent.vue` | 模型广场改为读取后台生成的 `/plaza-data.json`（原来写死演示分组，会盖掉真实分组数据），并同步线上「价格计算规则」看板、筛选 tab、搜索占位符 |
| `src/components/home/VendorCards.vue` | 厂商卡片型号名同步线上（`gpt-6.1-sol` / `claude-opus-5-5` / `gemini-3.8-flash-high`） |
| `index.html` | 深色主题引导脚本固化进源码（见 §4） |

应用方式（在纯净设计树上）：

```bash
cd /d F:\aily_scratch\fe_build\src\sub2api-0.2.11\frontend
git apply --no-index -p0 --directory=. ../../../../docs/frontend-design/port_all_cosmic_20261015.patch  # 或用编辑器逐条应用
```

## 3. 构建与发布

```powershell
# 构建（本机，Node 24 可用；等价于 pnpm run build 的 vite 单包变体）
cd F:\aily_scratch\fe_build\src\sub2api-0.2.11\frontend
node node_modules\vite\bin\vite.js build --config vite.override.config.ts
# 产物：dist-override\{index.html, logo.svg, assets\index-*.js, assets\style-*.css}
# 注意 emptyOutDir=true：每次构建会清空 dist-override（主题 index.html 由源码 index.html 带出）
```

```bash
# 发布（服务端，带守卫 + 全量备份）
scp dist-override/{index.html,logo.svg} zeroxcore:/home/ubuntu/fe_stage_xxx/
scp dist-override/assets/* zeroxcore:/home/ubuntu/fe_stage_xxx/assets/
scp docs/frontend-design/fe_publish_design.sh zeroxcore:/tmp/
ssh zeroxcore 'sudo STAGE=/home/ubuntu/fe_stage_xxx bash /tmp/fe_publish_design.sh'
```

`fe_publish_design.sh` 的发布前守卫（任一不满足即拒绝发布，避免"设计又消失"）：

1. `index.html` 必须含 `localStorage.setItem("theme"`；
2. 打包 JS 必须含 `antigravity-quota`（配额徽章）；
3. 打包 JS 必须含 `model-allowlist-sources`（白名单来源账号）；
4. 打包 JS 必须含 `plaza-data.json`（广场必须是数据驱动，不能是演示数据）；
5. CSS 必须含 `#07080c` 与 `custom-open-fab`（Cosmic 设计）。

**注意**：仓库里的 `fe_publish_design.sh` 是 BOM + CRLF，服务端 `bash` 会以
`syntax error near unexpected token '('` 直接失败。发布请用 `fe_publish_design_ascii.sh`
（纯 ASCII + LF，检查项完全一致，另加 `auto::rr` 一条）。

发布只覆盖 `index.html`、`assets/index-*.js`、`assets/style-*.css`、`logo.svg`；
`plaza-data.json`（后台定时生成，约每 10 分钟刷新）与历史 assets 一律保留。

回滚：`cp -a /var/www/sub2api-cosmic.bak_design_<时间戳>/. /var/www/sub2api-cosmic/`

## 4. 事故记录与教训

- **2026-10-15 主题丢失事故**：旧发布方式用上游仓库树（无 Cosmic 设计）在服务端构建后直接覆盖
  生产 `index.html`，抹掉了深色主题引导脚本，浅色系统下整个面板变浅色、"设计消失"。
  修复：恢复 `index.html`，并新增 §3 守卫；同时把主题脚本写进设计树源码 `index.html`，
  使任何一次构建都自带主题，不再依赖发布后手工补丁。
- **广场演示数据陷阱**：设计树的 `ModelPlazaContent.vue` 曾写死演示分组（`openAI--gpt6asutr` 等）。
  线上产物是"读 `/plaza-data.json`"的版本。直接重新构建会让模型广场丢失真实分组，
  因此守卫第 4 条专门拦截。厂商卡片文案同理（线上为正式型号名）。

## 5. 验证清单（发布后）

```bash
R="--resolve api.zeroxcore.tech:443:127.0.0.1 -s"
curl $R https://api.zeroxcore.tech/ | grep -c 'localStorage.setItem("theme"'   # 1
curl $R https://api.zeroxcore.tech/assets/<新>.js -o /tmp/s.js; grep -c plaza-data.json /tmp/s.js   # 1
curl $R https://api.zeroxcore.tech/assets/<新>.css | grep -o -i 07080c | wc -l                      # 4
# 后端接口（真实 admin key: settings.admin_api_key）
curl -s -H "x-api-key: $KEY" http://127.0.0.1:8080/api/v1/admin/accounts/34/antigravity-quota
curl -s -H "x-api-key: $KEY" "http://127.0.0.1:8080/api/v1/admin/groups/14/model-allowlist-sources?platform=openai"
```

## 6. 设计树与仓库 HEAD 的差异（`prod_design_delta_20261015.patch`）

`frontend/`（仓库）与设计树不是同一棵树：设计树基于 upstream `v0.2.11`，另有本站定制
（`GroupModelsModal`、去掉 `platformLabel` 回退、部分 i18n 条目差异等）。为让生产前端的
改动可回放，`prod_design_delta_20261015.patch` 记录**设计树当前状态相对仓库 HEAD 的差异**
（`git apply --check --cached` 已验证可通过）：

| 文件 | 关系 |
| --- | --- |
| `src/views/admin/groupModelAllowlist.ts` | 与仓库 HEAD **逐字节一致**（无需补丁） |
| `src/views/admin/GroupsView.vue` | 差异含本站定制 + auto 面板，补丁按文件替换 |
| `src/i18n/locales/{zh,en}/admin/overview.ts` | 差异含既有文案差异 + auto 面板，补丁按文件替换 |

即：`git apply docs/frontend-design/prod_design_delta_20261015.patch` 后，这三个文件
与当前生产设计树一致。auto 智能路由面板本身（sentinel 从模型列表移入配置面板、
提交时注入 `auto::rr`/`auto::rand`、真实 auto 模型共存警示）在仓库侧同样已实现，见
commit `61e57dc9c`、`b2ddc6c0d`。

设计树改动清单（本次）：

1. `groupModelAllowlist.ts`：新增 `parseAutoSentinelEntry` / `setModelAllowlistAutoEnabled`
   / `setModelAllowlistAutoStrategy`；`items` 不再承载 sentinel，`buildModelAllowlistConfig`
   在开启时把 sentinel 注入 `models` 首位。
2. `GroupsView.vue`（创建 + 编辑两个面板）：白名单列表上方新增「auto 智能路由」面板行
   （AUTO 徽章 + 开关 + 轮询/随机选择 + 冲突警示），并在编辑列表给真实 auto 模型加
   「真实模型」徽章；`resetModelAllowlistState` 同步重置 `autoEnabled`/`autoStrategy`
   （否则面板状态会从上一个分组残留到下一个）。
3. `overview.ts`(zh/en)：`modelAllowlist.auto.*` 文案。

