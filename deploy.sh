#!/bin/sh
# Signal Atlas 一键部署 / one-command deployment.
#
#   ./deploy.sh                 启动（自动生成登录密码）
#   PORT=9000 ./deploy.sh       换端口
#   SIGNAL_ATLAS_WEB_PASSWORD=... ./deploy.sh   指定密码
#
# 需要 Docker 与 Compose v2；重复执行会复用已有 .env 与数据目录。
set -eu

cd "$(dirname "$0")"

if ! command -v docker >/dev/null 2>&1; then
  echo "未找到 docker，请先安装 Docker。" >&2
  exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  echo "需要 Docker Compose v2（docker compose），请升级 Docker。" >&2
  exit 1
fi

DEMO=""
case "${1:-}" in
  --demo|-d) DEMO=1 ;;
esac

PORT="${PORT:-8768}"
if [ -f .env ]; then
  echo "沿用已有 .env（不会覆盖账号和密码）。"
else
  USER_NAME="${SIGNAL_ATLAS_WEB_USER:-admin}"
  PASSWORD="${SIGNAL_ATLAS_WEB_PASSWORD:-}"
  if [ -z "$PASSWORD" ]; then
    PASSWORD=$(LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom | head -c 24)
  fi
  cat > .env <<ENV
SIGNAL_ATLAS_WEB_USER=$USER_NAME
SIGNAL_ATLAS_WEB_PASSWORD=$PASSWORD
SIGNAL_ATLAS_PORT=$PORT
ENV
  chmod 600 .env
fi

echo "构建并启动（首次需要几分钟）…"
if [ -n "$DEMO" ]; then
  echo "写入 7 天演示数据，不需要任何设备…"
  docker compose run --rm data-init >/dev/null
  docker compose --profile demo run --rm --build demo
fi
docker compose up -d --build --wait --wait-timeout 900

IP=$(ip route get 1.1.1.1 2>/dev/null | awk '{print $7; exit}' || true)
[ -n "${IP:-}" ] || IP=$(hostname -I 2>/dev/null | awk '{print $1}' || true)
[ -n "${IP:-}" ] || IP=127.0.0.1

# .env 可能来自上一次运行，端口以它为准。
PORT=$(grep -E '^SIGNAL_ATLAS_PORT=' .env | tail -1 | cut -d= -f2)
USER_NAME=$(grep -E '^SIGNAL_ATLAS_WEB_USER=' .env | tail -1 | cut -d= -f2)
PASSWORD=$(grep -E '^SIGNAL_ATLAS_WEB_PASSWORD=' .env | tail -1 | cut -d= -f2)

cat <<DONE

Signal Atlas 已启动
  地址  http://$IP:$PORT
  账号  $USER_NAME
  密码  $PASSWORD

打开页面后到「采集与设置」扫描本网段，添加路由器和 AP。
（用 ./deploy.sh --demo 启动时已经预置了演示设备，可直接看效果。）
数据保存在 runtime/data，备份时复制这一个目录即可。
DONE
