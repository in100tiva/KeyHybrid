#!/usr/bin/env bash
set -e

echo "========================================================"
echo "  KeyHybrid - Instalador Oficial para Linux (Desktop)   "
echo "========================================================"

# 1. Instalar dependências de sistema do Tauri v2 + WebKit2GTK se necessário (Ubuntu/Debian)
if command -v apt-get >/dev/null 2>&1; then
  echo "[1/4] Verificando bibliotecas de sistema (WebKit2GTK / LibSecret / ALSA)..."
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
echo "[2/4] Compilando o Gateway Local em Go (apps/server/keyhybrid-server)..."
cd "$(dirname "$0")/apps/server"
go mod tidy
go build -o keyhybrid-server main.go
chmod +x keyhybrid-server

# Copiar também para ~/.keyhybrid/bin para auto-start pelo Tauri
mkdir -p "$HOME/.keyhybrid/bin"
cp -f keyhybrid-server "$HOME/.keyhybrid/bin/keyhybrid-server"

# 3. Instalar dependências do Frontend Desktop (Svelte 5 + Tauri v2)
echo "[3/4] Instalando dependências do Desktop App (apps/desktop)..."
cd "../desktop"
if command -v bun >/dev/null 2>&1; then
  bun install
else
  npm install
fi

echo "[4/4] Instalação concluída com sucesso!"
echo "Para iniciar o KeyHybrid no Linux, execute:"
echo "  ./run_linux.sh"
