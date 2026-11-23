# 🛡️ KeyHybrid — Zero-Knowledge API Key Vault & Local AI Gateway

[![License: MIT](https://img.shields.io/badge/license-MIT-3DA639?style=flat-square)](LICENSE)
![Go](https://img.shields.io/badge/Go-00ADD8?style=flat-square&logo=go&logoColor=white)
![Rust](https://img.shields.io/badge/Rust-000000?style=flat-square&logo=rust&logoColor=white)
![Tauri](https://img.shields.io/badge/Tauri-v2-24C8DB?style=flat-square&logo=tauri&logoColor=white)
![Svelte](https://img.shields.io/badge/Svelte-5-FF3E00?style=flat-square&logo=svelte&logoColor=white)

> **Em 30 segundos:** um cofre local que guarda as chaves reais dos provedores de IA e expõe um gateway compatível com a API da OpenAI em `http://127.0.0.1:8090/v1`. Seus agentes usam tokens virtuais (`kh-openrouter`); a chave real nunca entra em `.env`, histórico de shell ou código.

O **KeyHybrid** é um cofre desktop para chaves de API de IA (**OpenRouter, Google Gemini, Anthropic**) combinado com um **reverse proxy local zero-knowledge**, construído em **Tauri v2 (Rust)**, **Svelte 5** e **Go (PocketBase)**.

Em vez de espalhar chaves reais (`sk-or-v1-...`, `AIza...`, `sk-ant-...`) por arquivos `.env`, scripts ou agentes autônomos, você armazena a chave **uma única vez** no cofre criptografado e usa apenas tokens virtuais (`Bearer kh-openrouter`).

## Por que existe

- **Chave vaza por acidente**: `.env` esquecido, histórico de shell, print de tela, commit.
- **Cada agente implementa auth do seu jeito** — e todos acabam precisando da chave real.
- **Trocar de provedor** não deveria significar editar dez arquivos de configuração.

## Como funciona

```mermaid
flowchart LR
    subgraph Client["Clientes & Agentes Locais"]
        CLI["Terminal / cURL / SDK OpenAI"]
        IDE["Agentes de IA (Cursor, etc.)"]
        UI["Desktop (Svelte 5 + Tauri v2)"]
    end

    subgraph SecurityLayer["Camada de Segurança Nativa"]
        PIN["PIN de 6 dígitos"]
        OSVault["Windows DPAPI / Linux Secret Service\n+ ZeroizeOnDrop"]
    end

    subgraph Gateway["KeyHybrid Local Gateway (Go :8090)"]
        Proxy["Reverse Proxy Multimodal\n/v1/chat, /v1/audio, /v1/videos"]
        AES["AES-256-GCM\n(~/.keyhybrid/.master.key)"]
        DB[("SQLite local\nsecret_key = enc:v1:...")]
    end

    subgraph Providers["Provedores Upstream"]
        OR["OpenRouter"]
        GM["Google Gemini"]
        AN["Anthropic"]
    end

    CLI -- "Bearer kh-openrouter" --> Proxy
    IDE -- "Bearer kh-openrouter" --> Proxy
    UI <--> PIN
    PIN <--> OSVault
    Proxy <--> AES
    AES <--> DB
    Proxy -- "injeta a chave real em RAM" --> OR
    Proxy -- "injeta a chave real em RAM" --> GM
    Proxy -- "injeta a chave real em RAM" --> AN
```

## Pilares de segurança

- **Isolamento por alias (`kh-<alias>`)**: nenhuma ferramenta externa vê a chave real; o gateway descriptografa em RAM só no instante da chamada HTTPS.
- **Criptografia dupla em repouso**: AES-256-GCM no SQLite com chave mestra local (`0600`) + DPAPI/Secret Service para credenciais e PIN, com `Zeroize` em Rust.
- **Fallback de guardrail**: se a OpenRouter recusar um modelo (`HTTP 404`), o gateway reroteia para `openrouter/auto` sem quebrar o agente.

## Quickstart

### Linux

```bash
git clone https://github.com/in100tiva/KeyHybrid.git
cd KeyHybrid
chmod +x install_linux.sh && ./install_linux.sh
```

### Windows

```powershell
git clone https://github.com/in100tiva/KeyHybrid.git
cd KeyHybrid
.\run_windows.bat
```

Pré-requisitos por distro e geração de instaladores (`.deb`, `.msi`) estão em **[docs/INSTALL.md](docs/INSTALL.md)**.

## Usar o gateway

Salve a chave na interface — o KeyHybrid gera o token virtual correspondente:

- **Base URL:** `http://127.0.0.1:8090/v1`
- **API key:** `kh-openrouter`

```bash
curl http://127.0.0.1:8090/v1/chat/completions \
  -H "Authorization: Bearer kh-openrouter" \
  -H "Content-Type: application/json" \
  -d '{"model":"openrouter/auto","messages":[{"role":"user","content":"oi"}]}'
```

Chaves de serviço (Vercel, Supabase, GitHub), `kh run` e limitações conhecidas: **[docs/USAGE.md](docs/USAGE.md)**.

## Estrutura

```text
apps/server    # gateway Go (PocketBase) + cofre AES-256-GCM + proxy /v1
apps/desktop   # app Tauri v2 + Svelte 5 (PIN, cofre, UI)
docs/          # INSTALL.md e USAGE.md
```

## Licença

[MIT](LICENSE) © Luan Oliveira dos Santos
