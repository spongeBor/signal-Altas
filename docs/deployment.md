# Deployment

## 一键启动 / One command

```sh
./deploy.sh
```

生成 `.env`（随机密码，600 权限）、构建镜像并等待健康检查，最后打印访问地址与账号。
`PORT=9000 ./deploy.sh` 换端口，`SIGNAL_ATLAS_WEB_PASSWORD=... ./deploy.sh` 指定密码；
重复执行会复用已有 `.env` 和数据目录。

## Docker Compose

```sh
cp .env.example .env      # set the console login (password 16–72 bytes)
docker compose up -d --build
```

The app listens on `${SIGNAL_ATLAS_PORT:-8768}`. `runtime/data` holds the SQLite file, the
encrypted device passwords and the approved SSH host keys; keep it backed up.

## Single binary

```sh
npm ci && npm run build --workspace @signal-atlas/web
cp -r web/dist/. api/internal/webui/dist/
go -C api build -o ../signal-atlas ./cmd/server
SIGNAL_ATLAS_WEB_USER=admin SIGNAL_ATLAS_WEB_PASSWORD='at-least-16-chars' \
  ./signal-atlas -addr 0.0.0.0:8768 -data-dir ./data
```

## PostgreSQL instead of SQLite

Set `DATABASE_URL`; the schema is created on startup. To move an existing PostgreSQL
installation into SQLite later:

```sh
go -C api build -o /tmp/atlas-migrate ./cmd/migrate
/tmp/atlas-migrate -pg "$DATABASE_URL" -sqlite /tmp/history.db
```

## Host networking

Port mapping is enough for collection. To let the process see the LAN directly (ARP,
mDNS, LLDP), use `network_mode: host` and bind the port directly
(`-addr 0.0.0.0:8768`); then no `ports:` entry is needed.

## Access control

- `SIGNAL_ATLAS_ALLOWED_HOSTS` accepts exact hosts and `*:PORT` wildcards; a wildcard only
  matches private IPv4 literals, so public hostnames (DNS rebinding) stay rejected.
- Log in once per browser; sessions are in-memory and expire after 12 hours.
