# Instalação — KeyHybrid

Guia completo para Linux e Windows. Para a visão geral do projeto, volte ao [README](../README.md).

## 📦 Instalar pelo instalador (recomendado)

Baixe o instalador da última versão em [**Releases**](https://github.com/in100tiva/KeyHybrid/releases) (o gateway `keyhybrid-server` já vem embutido):

| Arquivo | Sistema | Observações |
| --- | --- | --- |
| `KeyHybrid_<versão>_amd64.deb` | Ubuntu 22.04+ / Debian 12+ (`amd64`) | Instala o app e o gateway em `/usr/lib/KeyHybrid/server/` |
| `KeyHybrid_<versão>_x64-setup.exe` | Windows 10 / 11 (`x64`) | Instalador NSIS (recomendado para atualizar: encerra o gateway sozinho) |
| `KeyHybrid_<versão>_x64_en-US.msi` | Windows 10 / 11 (`x64`) | Pacote MSI para implantação corporativa |

Tags de pré-release (ex.: `v0.3.0-rc.1`) publicam só os binários do gateway: o `.msi` exige versão `X.Y.Z`.

### Conferir integridade e procedência

No Linux, valide o hash com `SHA256SUMS.txt` e a atestação de build do GitHub:

```bash
sha256sum -c SHA256SUMS.txt --ignore-missing
gh attestation verify KeyHybrid_<versão>_amd64.deb --repo in100tiva/KeyHybrid
```

No Windows (PowerShell), confira o hash SHA-256 e compare com a linha correspondente do `SHA256SUMS.txt`:

```powershell
Get-FileHash .\KeyHybrid_<versão>_x64_en-US.msi -Algorithm SHA256
```

### Linux (`.deb`)

```bash
sudo apt install ./KeyHybrid_<versão>_amd64.deb
```

Abra **KeyHybrid** pelo menu de aplicativos. O gateway sobe sozinho em `127.0.0.1:8090` e o cofre é gravado em `~/.keyhybrid/pb_data`. Para desinstalar (o nome do pacote Debian é `key-hybrid`):

```bash
sudo apt remove key-hybrid
```

### Windows (`.msi` e `-setup.exe`)

- Os instaladores ainda não possuem assinatura Authenticode: se o **SmartScreen** exibir um aviso, clique em **Mais informações** → **Executar assim mesmo**.
- O instalador `KeyHybrid_<versão>_x64-setup.exe` (NSIS) é o recomendado para instalar e atualizar, pois encerra o `keyhybrid-server.exe` automaticamente antes de substituir os arquivos.
- Se usar o `.msi`, antes de atualizar ou desinstalar, feche o app e encerre o gateway em execução:

```powershell
taskkill /F /IM keyhybrid-server.exe
```

As seções abaixo são para quem deseja compilar o KeyHybrid a partir do código-fonte.

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

#### Node.js 22.17+ e pnpm 10 (obrigatório para a interface)

O `nodejs` da distribuição costuma ser mais antigo que o exigido pelo SvelteKit 3. Se `node --version` mostrar algo abaixo de `22.17`, instale o LTS atual em <https://nodejs.org> e depois:

```bash
npm install -g pnpm@10
pnpm --version   # 10.x
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
pnpm install --frozen-lockfile
pnpm run tauri build
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

> **Node.js 22.17+** e **pnpm 10** são obrigatórios para a interface: `npm install -g pnpm@10`. O `OpenJS.NodeJS.LTS` do winget já costuma atender ao requisito de Node.

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
pnpm install --frozen-lockfile
pnpm run tauri build
```

O build compila o servidor Go (`pnpm run build:server`) e o empacota: o `keyhybrid-server.exe` vai para `server\` ao lado do `desktop.exe`. O instalador NSIS encerra o gateway em execução antes de atualizar ou desinstalar.
