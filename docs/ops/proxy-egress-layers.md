# 代理出口分层（mihomo 多实例）

2026-10-28 起，生产不是单一大网关，而是**三个相互隔离的 mihomo 实例**，每个实例对应
`proxies` 表里的一条记录，账号通过 `accounts.proxy_id` 各自绑定。

```
proxies 表
  id=1  mihomo-jp          → llm-mihomo         :7890   原有，20 个账号在用
  id=2  mihomo-yk-jp       → llm-mihomo-gemini  :7890   34 号专用，固定日本
  id=3  mihomo-fast-pool   → llm-mihomo-fast    :7890   低延迟池，未挂账号
```

三个容器都在 `llm-hub` 项目的 `llmnet` 网络上，sub2api 同时接了两个网络，
可以按容器名解析到它们。

## 为什么要拆

原来所有账号共用 `llm-mihomo`，而它的规则把 Google 流量分给了**两个不同机场**：

| 域名 | 路由目标 | 节点来源 | 出口地区 |
| --- | --- | --- | --- |
| `cloudcode-pa.googleapis.com`（反重力 API） | `AG-LONGTHINK` | 一元机场 hy2hop | 新加坡/美国 |
| `oauth2.googleapis.com`（token 刷新） | `GOOGLE-JP` | YKKCLOUD | 日本 |
| `generativelanguage.googleapis.com` | `STUDIO` | YKKCLOUD | 日本 |

结果是同一个账号的 API 调用从新加坡发出、token 刷新从日本发出。对上游来说，
这是同一账号在几秒内跨三个国家访问，属于强异常信号，也是 34 号被风控的可疑诱因之一。

另外 `AG-LONGTHINK` 的 fallback 节点在 2026-10-28 一度全部 `alive=False`，
`cloudcode-pa` 每 5 分钟就有一次 `context deadline exceeded`，反重力实际上完全不可用。

## 2026-10-28 的变更

### 1. 反重力改走单一日本节点

`llm-mihomo/config.yaml`：

```diff
- DOMAIN,cloudcode-pa.googleapis.com,AG-LONGTHINK
+ DOMAIN,cloudcode-pa.googleapis.com,GOOGLE-JP
```

出口从「一元机场新加坡/美国」变为「YKKCLOUD 日本」，与 token 刷新同一条链路，
跨国家问题消失。备份见 `config.yaml.bak_agroute_20261028_164519`。

### 2. 新增 `llm-mihomo-gemini`（id=2）

- 只包含 YKKCLOUD 的 5 个日本节点（`routely.moe:13001-13005`）
- 单一 `select` 组 `GEMINI`，`rules: ["MATCH,GEMINI"]`，所有流量强制走日本
- 34 号绑定到这条，出口固定 `23.162.24.135`

### 3. 新增 `llm-mihomo-fast`（id=3）

- 合并两份订阅的港/台/日/新节点，共 34 个，去重后单组
- `url-test` 自动选最快，`interval: 120`、`tolerance: 50`，节点不固定
- 实测可用 19/34，最快 134ms（HongKong 05）
- **未绑定任何账号**，随时可用

### 4. 一元机场节点可用率偏低

34 个池内节点里有 15 个不通，且集中在 `YY-` 前缀（新加坡 rl、台湾商宽等）。
订阅到期日是 2027-06-16，但当前实际可用率低，需要单独排查该机场。

## 两个订阅的身份

这两份订阅长期被手动合并使用，容易搞混，明确记录：

| 名称 | 来源 | 特征 |
| --- | --- | --- |
| YKKCLOUD | `config.moe` | 含 `routely.moe`、`ykkcloud.link`；日本/香港/新加坡延迟低 |
| 一元机场 | `47.86.50.8` | 无 `routely.moe`；大量独立 IP，含 `cf.622228.xyz` |

判断依据：`routely.moe` 只在 YKKCLOUD 里出现（26 次），一元机场 0 次。
生产的 `Japan 01-05` 都是 YKKCLOUD。

## 回滚

```bash
# 34 号回到原出口
echo "update accounts set proxy_id=1 where id=34;" | docker exec -i sub2api-postgres psql -U sub2api -d sub2api

# 反重力路由回到 AG-LONGTHINK
cp /home/ubuntu/llm-hub/mihomo/config.yaml.bak_agroute_20261028_164519 \
   /home/ubuntu/llm-hub/mihomo/config.yaml
docker restart llm-mihomo

# 删掉新增的两个实例
cd /home/ubuntu/llm-hub && docker compose rm -sf mihomo-gemini mihomo-fast
```

`docker-compose.yml` 的备份是 `docker-compose.yml.bak_gemini_20261028_174123`。

## 已知未解决

- `llm-mihomo` 的 `MATCH,DIRECT` 兜底规则会让所有未匹配域名走直连。国内机器直连
  境外站点必然失败（日志里 `api.openai.com` 被 DNS 污染解析到 `199.59.149.235`
  后超时）。这是既有隐患，未改动。
- 混合分组的视觉能力判定（`groupCodexModelSupportsImageInput`）只要有一个纯文本账号
  参与就把整个模型标成不支持读图，尚未修复。
