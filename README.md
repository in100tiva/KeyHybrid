# KeyHybrid — Zero-Knowledge API Key Vault & Local AI Gateway

O **KeyHybrid** é um cofre desktop minimalista de chaves de API (**OpenRouter, Google Gemini, Anthropic**) e **Reverse Proxy Local Zero-Knowledge** (`http://127.0.0.1:8090/v1`), construído com **Tauri v2 (Rust)**, **Svelte 5** e **Go (PocketBase)**.

Em vez de espalhar chaves reais (`sk-or-v1-...`) em variáveis de ambiente, arquivos `.env`, terminais ou agentes de IA, você cadastra sua chave uma única vez no cofre do **KeyHybrid** e utiliza apenas o **Token Virtual** (`Bearer kh-openrouter`).

---

## 🔒 Arquitetura de Segurança

1. **Zero-Knowledge Proxy (`127.0.0.1:8090/v1`):**
   - Os clientes, agentes de código e scripts enviam apenas `Authorization: Bearer kh-<alias>`.
   - O Gateway Go intercepta a chamada localmente, descriptografa a chave real em memória RAM apenas no milissegundo do envio para o provedor (`OpenRouter`, `Gemini`, `Anthropic`) e descarta a referência.
2. **Criptografia Dupla em Repouso (`AES-256-GCM` + Cofre Nativo do SO):**
   - **No Banco Local (`SQLite`):** Todas as chaves salvas em `vault_keys` são criptografadas em repouso com **AES-256-GCM** (`enc:v1:...`) usando uma chave mestra local de 256 bits (`~/.keyhybrid/.master.key` com permissão `0600`) + campo `Hidden: true` no PocketBase.
   - **No Sistema Operacional (Rust `keyring` + `zeroize`):** Integração nativa com **Windows DPAPI (Credential Manager)** no Windows e **Linux Secret Service (`libsecret` / `gnome-keyring`)** no Linux, limpando segredos da memória RAM automaticamente via `ZeroizeOnDrop`.
3. **Bloqueio por PIN de 6 Dígitos:**
   - Tela de bloqueio rápida por PIN numérico de 6 dígitos armazenado no cofre nativo do sistema operacional (com fallback seguro em `~/.keyhybrid/.pin.vault` para ambientes Linux minimalistas).
4. **Proxy Multimodal Completo & Fallback Inteligente de Guardrails:**
   - `POST /v1/chat/completions` (com fallback automático para `openrouter/auto` caso um modelo específico seja bloqueado pelo Guardrail da conta).
   - `POST /v1/audio/speech` (TTS via OpenRouter: `elevenlabs/eleven-v4-turbo`, `heygen/heygen-voice-1-turbo`).
   - `POST /v1/videos`, `GET /v1/videos/{id}`, `GET /v1/videos/{id}/content` (Geração de vídeo assíncrona).
   - `GET /v1/models` (Catálogo completo de modelos).

---

## 🐧 Instalação nos PCs Linux (Ubuntu / Debian / Pop!_OS / Mint)

### Pré-requisitos rápidos
Certifique-se de ter **Go (`>= 1.22`)**, **Rust (`rustup`)** e **Node.js (`>= 20`) / Bun** instalados.

```bash
git clone https://github.com/in100tiva/KeyHybrid.git
cd KeyHybrid
chmod +x install_linux.sh run_linux.sh
./install_linux.sh
```

### Executar no Linux
```bash
./run_linux.sh
```

### Gerar pacote instalável `.deb` / `.AppImage` no Linux
```bash
cd apps/desktop
npm run tauri build
```

---

## 🪟 Execução no Windows

```powershell
.\run_windows.bat
```

---

## 🚀 Como usar o Token Virtual nos seus Agentes / Terminal

Com o KeyHybrid aberto (ou o servidor rodando em `127.0.0.1:8090`):

```bash
curl http://127.0.0.1:8090/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer kh-openrouter" \
  -d '{
    "model": "openrouter/auto",
    "messages": [{"role": "user", "content": "Olá!"}]
  }'
```
