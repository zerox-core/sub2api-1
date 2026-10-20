# sub2api 开发与交付流程规范

> 本文件自包含，不依赖任何前置上下文。适用于 `F:\sub2api` 仓库的所有代码改动。

## 0. 仓库基本事实

| 项目 | 值 |
|---|---|
| 本地路径 | `F:\sub2api` |
| 远端 origin | `https://github.com/zerox-core/sub2api-1.git` |
| 上游 upstream | `https://github.com/Wei-Shaw/sub2api.git`（只读参考，不推送） |
| 默认分支 | `main` |
| 生产主机 | `ssh zeroxcore`（152.136.204.41，用户 ubuntu） |
| 生产容器 | `sub2api`（`sub2api-ag-quota:<tag>`，监听 127.0.0.1:8080） |
| Go 工具链 | `F:\tools\go\bin\go.exe`（go1.27.3 便携版，非系统安装） |
| 模块缓存 | `GOMODCACHE=F:\sub2api\.gomodcache`，`GOCACHE=F:\sub2api\.gocache-tmp` |

> 本机**没有**系统级 Go 安装，所有 Go 命令都需先注入环境变量（见 §2）。

## 1. 分支规范（强制）

**每个功能从 `main` 拉独立分支，一个分支只承载一个功能。**

```powershell
git checkout main
git pull origin main
git checkout -b feat/<短名>      # 新功能
# 或
git checkout -b fix/<短名>       # 缺陷修复
```

规则：

- ✅ 分支名用 `feat/*` 或 `fix/*` 前缀，短名用 kebab-case，描述功能本身（如 `feat/scheduler-tier`、`fix/sticky-escape-ttft`）
- ✅ 分支只含该功能的提交；发现无关改动时另开分支
- ✅ 分支用完即删，**不使用长命分支**
- ❌ 不直接 `git push origin main`
- ❌ 不做 `git push origin <branch>:main` 这类 fast-forward 直推
- ❌ 不在功能分支上混入多个不相关功能

### 提交粒度

一个逻辑变更一个提交，中文或英文均可，但需满足：

- 首行 `type(scope): 摘要`（type ∈ feat/fix/refactor/test/docs/chore/build）
- 正文说明**为什么改**，而不只是改了什么
- 一个提交内不混合格式化与逻辑变更

## 2. 本地验证（提交前必做）

```powershell
$env:GOROOT='F:\tools\go'
$env:PATH="F:\tools\go\bin;$env:PATH"
$env:GOMODCACHE='F:\sub2api\.gomodcache'
$env:GOCACHE='F:\sub2api\.gocache-tmp'
$env:GOFLAGS='-mod=mod'
$env:GOPROXY='https://goproxy.cn,direct'
cd F:\sub2api\backend
```

### 2.1 格式与静态检查

```powershell
gofmt -l internal/<改动的包路径>     # 输出为空才算通过
go vet ./internal/<包路径>/
go build ./...
```

### 2.2 单元测试

后端测试**必须带 `-tags unit`**，否则测试文件不会被编译：

```powershell
# 只跑改动相关的测试
go test -tags unit ./internal/service/ -run '<正则>' -count=1 -v

# 全量回归
go test -tags unit ./internal/service/ -count=1 -timeout=1800s
```

集成测试用 `-tags integration`，需要真实依赖，常规改动不跑。

### 2.3 已知基线失败（不是你的改动导致的）

以下测试在干净 `main` 上就会失败，**不要试图修复**，也不要用它们判断自己的改动是否有问题：

- `TestCompatibleImagesNativeAccountsRejectGeminiBeforeForwarding`（`oauth/mapping=true`、`setup-token/mapping=true` 两个子用例）

`TestCommandCodeGatewayPassesThroughCatalogProtocols` 与 `TestOllamaProbeCallback_StaleLongDoesNotOverrideNewShort` 在全量并发跑时会因测试间干扰失败，单独跑能过，属已知噪声。

**判断方法**：改动前后各跑一次，对比失败集合；或用 `git stash` 暂存改动后复跑。

### 2.4 完整构建（涉及 embed/前端时）

```powershell
cd F:\sub2api\backend
go build -tags embed -ldflags="-s -w -X main.Version=<ver> -X main.BuildType=release" -o ..\_build\sub2api.exe ./cmd/server
```

`-tags embed` 才会把 `internal/web/dist` 打进二进制，不加则前端 404。

## 3. 开 PR（强制）

```powershell
git push -u origin feat/<短名>
gh pr create --base main --head feat/<短名> --title "<type>(<scope>): 摘要" --body "<正文>"
```

PR 正文模板（仓库无官方模板，用这个）：

```markdown
## 变更内容
<说明改了什么，涉及哪些文件/函数>

## 动机
<为什么要改，解决什么问题，有无关联 issue>

## 验证
- [ ] `gofmt -l` 干净
- [ ] `go vet` 通过
- [ ] `go build ./...` 通过
- [ ] 单元测试：<列出实际跑的命令与结果>
- [ ] 已知基线失败未新增

## 风险与回滚
<影响面、默认行为是否变化、如何回滚>
```

PR 要求：

- ✅ 一个 PR 只做一件事，文件数尽量可控
- ✅ CI（`.github/workflows/backend-ci.yml`）必须绿
- ✅ 涉及调度/计费/鉴权等核心路径时，PR 里说明**默认行为是否改变**
- ❌ 不自行合并 PR，除非用户明确授权
- ❌ 不 force-push 到已被审查的分支（需 rebase 时先说明）

## 4. 合并与清理

审查通过、CI 绿后：

```powershell
gh pr merge <编号> --squash --delete-branch
git checkout main
git pull origin main
```

- 优先 `--squash`，让 main 历史保持一个功能一个提交
- 合并后删除远端与本地功能分支

## 5. 生产部署（独立于 PR 流程）

只有**合并到 main 之后**才允许部署。部署不是 PR 的一部分。

### 5.1 构建镜像

```powershell
cd F:\sub2api
docker build -t sub2api:<tag> `
  --build-arg GOPROXY=https://goproxy.cn,direct `
  --build-arg GOSUMDB=sum.golang.google.cn `
  -f Dockerfile .
```

`<tag>` 约定：`<语义>-<短SHA>`，如 `tier-b6095c5fa`、`fix-sticky-6c9655b31`。

### 5.2 上线前必备备份

```powershell
# 数据库
ssh zeroxcore 'TS=$(date +%Y%m%d_%H%M%S); sudo docker exec sub2api-postgres pg_dump -U sub2api -d sub2api -Fc -f /tmp/pre_deploy_$TS.dump && echo $TS'

# compose 文件
ssh zeroxcore 'cd ~/sub2api && cp docker-compose.yml docker-compose.yml.bak_<tag>'
```

### 5.3 上传与切换

```powershell
docker save sub2api:<tag> -o F:\sub2api\_build\image.tar
scp F:\sub2api\_build\image.tar zeroxcore:/tmp/image.tar
ssh zeroxcore "sudo docker load -i /tmp/image.tar"
# 修改 ~/sub2api/docker-compose.yml 的 image 行（用 python 脚本改，避免 PowerShell 引号问题）
ssh zeroxcore "cd ~/sub2api && sudo docker compose up -d sub2api"
```

### 5.4 上线后验证

```powershell
# 容器状态
ssh zeroxcore "sudo docker inspect sub2api --format 'IMAGE={{.Config.Image}} STATUS={{.State.Status}} HEALTH={{.State.Health.Status}}'"

# 健康检查
ssh zeroxcore "curl -s -o /dev/null -w 'HTTP %{http_code}\n' http://127.0.0.1:8080/health"

# 镜像一致性（关键：证明生产跑的就是本地验证过的镜像）
docker inspect sub2api:<tag> --format '{{.Id}}'
ssh zeroxcore "sudo docker inspect sub2api --format '{{.Image}}'"

# 真实流量
ssh zeroxcore "sudo docker logs sub2api --since 5m 2>&1 | grep -E 'status_code|ERROR' | tail -20"
```

### 5.5 回滚

```powershell
ssh zeroxcore "cd ~/sub2api && cp docker-compose.yml.bak_<tag> docker-compose.yml && sudo docker compose up -d sub2api"
```

旧镜像保持在 `docker images` 中，不要清理正在用的上一个版本。

## 6. Windows / PowerShell 注意事项

- **不要用 `Set-Content` 写 Go 源码或含中文的文件**，会产生 BOM 或编码损坏。改用：
  ```powershell
  $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
  [System.IO.File]::WriteAllText("F:\path\file.go", $content, $utf8NoBom)
  ```
- **不要用 `-replace` 做批量代码改写**，容易产生重复字符或破坏字符串字面量。用编辑器工具逐处改。
- **SSH 远端命令含 `$()`、`|`、`"` 时**，不要直接 `ssh host "cmd"`，PowerShell 会抢先展开。改用：
  1. 本地写 Python 脚本 → `scp` 到 `/tmp` → `ssh host "python3 /tmp/x.py"` → 删除
  2. 或用单引号包裹整个命令：`$cmd = '...'; ssh host $cmd`
- **`strings`、`md5sum` 在生产容器里不可用**（镜像是精简 alpine），验证代码是否在镜像中用镜像 ID 比对，不要用字符串搜索。
- 生产 `docker` 需要 `sudo`（ubuntu 用户无 docker 组权限）。

## 7. 常用排查命令

```powershell
# 生产日志（应用跑在 Docker 里，journalctl 无效）
ssh zeroxcore "sudo docker logs sub2api --since 30m 2>&1 | tail -50"

# 查数据库（用 python 脚本方式，见 §6）
# 表：ops_error_logs（上游错误）、usage_logs（计费/选号）、accounts、groups、api_keys
```

**编码坑**：`ops_error_logs` 没有 `account_name` 列；`accounts` 没有 `models`/`model_mapping` 列（模型能力在 `extra` 和 `groups.model_allowlist`）；`usage_logs` 是查选号结果（`account_id` + `group_id`）的正确表，不是 `ops_request_logs`。
