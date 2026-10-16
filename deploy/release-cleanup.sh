#!/usr/bin/env bash
# 发布后清理：回收旧版本与构建垃圾，避免磁盘被历史镜像 / 构建缓存吃满。
#
# 用法（在服务器上，构建并 docker compose up -d 成功之后执行）：
#   bash deploy/release-cleanup.sh
#
# 安全约束（故意写死，不接受参数覆盖）：
#   1. 正在运行的镜像由**运行中的 sub2api 容器**推导，而不是命令行传参 —— 打错字也删不掉线上镜像；
#   2. 旧 tag 逐个 rmi，被任何容器引用的会失败并跳过（docker 自身兜底）；
#   3. 只清 exited/dead 容器，跑着的出口链路容器一律不动：
#      llm-mihomo（别名 mihomo，是 proxies 表 mihomo-jp 的宿主，15 个账号的出口依赖它）、
#      llm-tlsproxy、audio-proxy-bridge（socat → 127.0.0.1:8790 的 MP3 音频桥）。
set -uo pipefail

CONTAINER="${SUB2API_CONTAINER:-sub2api}"
COMPOSE_DIR="${SUB2API_COMPOSE_DIR:-/home/ubuntu/sub2api}"
KEEP_BACKUPS="${KEEP_BACKUPS:-2}"

say() { printf '%s\n' "$*"; }

say "== before =="
df -h / | tail -1
sudo docker system df

running_image=$(sudo docker inspect --format '{{.Config.Image}}' "$CONTAINER" 2>/dev/null || true)
if [ -z "$running_image" ]; then
  say "FATAL: container '$CONTAINER' not found; refusing to prune images"
  exit 1
fi
say "running image: $running_image"

say
say "== exited/dead containers =="
sudo docker ps -a --filter status=exited --filter status=dead --format '  {{.Names}} | {{.Image}} | {{.Status}}'
sudo docker container prune -f

say
say "== superseded sub2api image tags (running one preserved) =="
sudo docker images --format '{{.Repository}}:{{.Tag}}|{{.ID}}' \
  | awk -F'|' -v keep="$running_image" '$1 ~ /^sub2api-ag-quota:/ && $1 != keep {print $2}' \
  | sort -u | while read -r id; do
      [ -n "$id" ] || continue
      sudo docker rmi "$id" >/dev/null 2>&1 || say "  image $id still referenced, skipped"
    done
sudo docker image prune -f

say
say "== build cache (usually the biggest win) =="
sudo docker builder prune -af 2>&1 | tail -2

say
say "== compose backups (keep newest $KEEP_BACKUPS) =="
if [ -d "$COMPOSE_DIR" ]; then
  (cd "$COMPOSE_DIR" && ls -1t docker-compose.yml.bak.* 2>/dev/null \
    | tail -n "+$((KEEP_BACKUPS + 1))" \
    | while read -r f; do rm -f "$f" && say "  removed $f"; done)
fi

say
say "== release temp scripts =="
# 只清本仓库历次发布/验证留下的命名约定，避免误删服务器上其它项目的临时数据。
rm -f /tmp/deploy_r*.sh /tmp/verify_r*.sh /tmp/e2e_*.sh /tmp/fin*.sh /tmp/go*.sh \
      /tmp/v[0-9]*.sh /tmp/p[0-9]*.sh /tmp/q.sql /tmp/x.sh \
      /tmp/r[0-9]_*.json /tmp/f_*.json /tmp/auth_keys.txt /tmp/*build.log \
      /tmp/*_old_ids.txt 2>/dev/null
say "  done"

say
say "== after =="
df -h / | tail -1
sudo docker system df
