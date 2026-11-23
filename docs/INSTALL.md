# Instalação — KeyHybrid

Guia completo para Linux e Windows. Para a visão geral do projeto, volte ao [README](../README.md).

## 🐧 Linux

### 1. Pré-requisitos

Escolha o comando da sua distribuição.

#### Ubuntu / Debian / Linux Mint / Pop!_OS

```bash
sudo apt-get update && sudo apt-get install -y \
  libwebkit2gtk-4.1-dev build-essential curl wget file libxdo-dev \
  libssl-dev libayatana-appindicator3-dev librsvg2-dev libsecret-1-dev \
  libasound2-dev portaudio19-dev ffmpeg golang-go nodejs npm
```

#### Fedora / RHEL / CentOS

```bash
sudo dnf install -y \
  webkit2gtk4.1-devel openssl-devel curl wget file \
  libappindicator-gtk3-devel librsvg2-devel libsecret-devel \
  alsa-lib-devel portaudio-devel ffmpeg golang nodejs npm
```

#### Arch Linux / Manjaro

```bash
sudo pacman -S --needed \
  webkit2gtk-4.1 base-devel curl wget file openssl \
  libappindicator-gtk3 librsvg libsecret alsa-lib portaudio ffmpeg go nodejs npm
```

#### Rust (se ainda não tiver)

```bash
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
source "$HOME/.cargo/env"
```

### 2. Clonar e instalar (1 comando)

```bash
git clone https://github.com/in100tiva/KeyHybrid.git
cd KeyHybrid
chmod +x install_linux.sh run_linux.sh
./install_linux.sh
```

O `install_linux.sh`:

1. verifica/instala as bibliotecas de sistema necessárias;
2. compila o `keyhybrid-server` (Go) e copia para `~/.keyhybrid/bin/`;
3. registra o gateway como serviço `systemd --user` (`keyhybrid.service`), que sobe a cada login;
4. compila o app desktop em release e cria o atalho **KeyHybrid** no menu.

O cofre fica em `~/.keyhybrid/pb_data`. Instalações antigas com `apps/server/pb_data` são migradas na primeira execução.

### 3. Executar

Abra **KeyHybrid** pelo menu de aplicativos ou:

```bash
./run_linux.sh
```

Status do gateway: `systemctl --user status keyhybrid`.

### 4. Gerar instalador nativo (`.deb` / `.AppImage` / `.rpm`)

```bash
cd apps/desktop
npm run tauri build
```

Os pacotes ficam em `apps/desktop/src-tauri/target/release/bundle/`.

## 🪟 Windows 10 / 11

### 1. Pré-requisitos

No PowerShell:

```powershell
winget install GoLang.Go
winget install Rustlang.Rustup
winget install OpenJS.NodeJS.LTS
```

Para compilar Rust no Windows, instale também o **Microsoft C++ Build Tools**.

### 2. Clonar e executar

```powershell
git clone https://github.com/in100tiva/KeyHybrid.git
cd KeyHybrid
.\run_windows.bat
```

O `run_windows.bat` compila o `keyhybrid-server.exe` (se necessário), sobe o gateway em `127.0.0.1:8090` e abre a janela do desktop.

### 3. Gerar instalador `.msi` / `.exe`

```powershell
cd apps\desktop
npm install
npm run tauri build
```

O build compila o servidor Go (`npm run build:server`) e o empacota: o `keyhybrid-server.exe` vai para `server\` ao lado do `desktop.exe`. O instalador NSIS encerra o gateway em execução antes de atualizar ou desinstalar.

## 🎙️ Sexta-Feira (opcional)

Reconhecimento de voz contínuo pelo microfone físico (Windows ou Linux):

```bash
pip install faster-whisper sounddevice av numpy
python apps/server/friday_ear.py
```

Para controlar uma lâmpada Xiaomi na LAN (MiIO UDP 54321), crie `apps/server/.xiaomi_token` com o token hexadecimal de 32 caracteres ou defina `XIAOMI_BULB_TOKEN`.
