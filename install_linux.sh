#!/usr/bin/env bash
set -e

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "========================================================"
echo "  KeyHybrid - Instalador Oficial para Linux (Desktop)   "
echo "========================================================"

# 1. Instalar dependências de sistema do Tauri v2 + WebKit2GTK se necessário (Ubuntu/Debian)
if command -v apt-get >/dev/null 2>&1; then
  echo "[1/5] Verificando bibliotecas de sistema (WebKit2GTK / LibSecret / ALSA)..."
  sudo apt-get update -y
  sudo apt-get install -y \
    libwebkit2gtk-4.1-dev \
    build-essential \
    curl \
    wget \
    file \
    libxdo-dev \
    libssl-dev \
    libayatana-appindicator3-dev \
    librsvg2-dev \
    libsecret-1-dev \
    libasound2-dev \
    portaudio19-dev \
    ffmpeg
fi

# 2. Compilar o Servidor Gateway em Go (PocketBase + AES-256-GCM Vault)
echo "[2/5] Compilando o Gateway Local em Go (apps/server/keyhybrid-server)..."
cd "$ROOT_DIR/apps/server"
go build -trimpath -buildvcs=false -ldflags="-s -w" -o keyhybrid-server .
chmod +x keyhybrid-server

# O cofre fica em ~/.keyhybrid/pb_data (não ao lado do binário)
mkdir -p "$HOME/.keyhybrid/bin"
cp -f keyhybrid-server "$HOME/.keyhybrid/bin/keyhybrid-server"
mkdir -p "$HOME/.local/bin" && ln -sf "$HOME/.keyhybrid/bin/keyhybrid-server" "$HOME/.local/bin/kh"
if [ ! -e "$HOME/.keyhybrid/pb_data" ] && [ -f pb_data/data.db ]; then
  cp -a pb_data "$HOME/.keyhybrid/pb_data" && chmod 700 "$HOME/.keyhybrid/pb_data"
fi

# 3. Gateway como serviço do usuário: sobe sozinho a cada login
echo "[3/5] Instalando o serviço keyhybrid (systemd --user)..."
mkdir -p "$HOME/.config/systemd/user"
cp -f keyhybrid.service "$HOME/.config/systemd/user/keyhybrid.service"
pkill -x keyhybrid-serve || true # instância avulsa do run_linux.sh seguraria a porta 8090
systemctl --user daemon-reload
systemctl --user enable keyhybrid.service
systemctl --user restart keyhybrid.service # reinstalação: pega o binário novo

# 4. Desktop App (Svelte 5 + Tauri v2) em release
echo "[4/5] Compilando o Desktop App (apps/desktop)..."
cd "$ROOT_DIR/apps/desktop"
if command -v bun >/dev/null 2>&1; then
  bun install && bun run tauri build --no-bundle
else
  npm install && npm run tauri build -- --no-bundle
fi

# 5. Atalho no menu de aplicativos
echo "[5/5] Criando atalho no menu..."
mkdir -p "$HOME/.local/share/applications"
cat > "$HOME/.local/share/applications/keyhybrid.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=KeyHybrid
Comment=Cofre local de chaves de API
Exec=$ROOT_DIR/apps/desktop/src-tauri/target/release/desktop
Icon=$ROOT_DIR/apps/desktop/src-tauri/icons/128x128.png
Terminal=false
Categories=Utility;Security;
EOF

echo "Instalação concluída. Abra o KeyHybrid pelo menu de aplicativos (ou ./run_linux.sh)."
