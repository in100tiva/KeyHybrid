#!/usr/bin/env bash
set -e

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"

# 1. Garantir que o Gateway Go esteja compilado e rodando em 127.0.0.1:8090
if [ ! -f "$ROOT_DIR/apps/server/keyhybrid-server" ]; then
  echo "[KeyHybrid] Compilando servidor Go..."
  (cd "$ROOT_DIR/apps/server" && go build -o keyhybrid-server .)
fi

if ! nc -z 127.0.0.1 8090 >/dev/null 2>&1; then
  echo "[KeyHybrid] Iniciando Gateway Seguro em http://127.0.0.1:8090 ..."
  (cd "$ROOT_DIR/apps/server" && nohup ./keyhybrid-server serve --http=127.0.0.1:8090 >/tmp/keyhybrid-server.log 2>&1 &)
  sleep 1
fi

# 2. Iniciar a interface Desktop Tauri (release, se já compilado pelo install_linux.sh)
echo "[KeyHybrid] Abrindo interface Desktop..."
RELEASE_BIN="$ROOT_DIR/apps/desktop/src-tauri/target/release/desktop"
if [ -x "$RELEASE_BIN" ]; then
  exec "$RELEASE_BIN"
fi
cd "$ROOT_DIR/apps/desktop"
if command -v bun >/dev/null 2>&1; then
  bun run tauri dev
else
  npm run tauri dev
fi
