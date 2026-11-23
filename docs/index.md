---
layout: default
---

# 🛡️ KeyHybrid

**Cofre *zero-knowledge* de chaves de IA + gateway local compatível com a API da OpenAI.**

Seus agentes, scripts e IDE falam com `http://127.0.0.1:8090/v1` usando um token virtual (`Bearer kh-<alias>`). A chave real do provedor nunca sai do cofre criptografado e nunca aparece em arquivos `.env`, histórico de shell ou logs.

[![Release](https://img.shields.io/github/v/release/in100tiva/KeyHybrid?style=flat-square)](https://github.com/in100tiva/KeyHybrid/releases)
[![License: MIT](https://img.shields.io/badge/license-MIT-3DA639?style=flat-square)](https://github.com/in100tiva/KeyHybrid/blob/main/LICENSE)
[![Container](https://img.shields.io/badge/ghcr.io-keyhybrid-2496ED?style=flat-square&logo=docker&logoColor=white)](https://github.com/in100tiva/KeyHybrid/pkgs/container/keyhybrid)

## Comece aqui

| Documento | Para quê |
| --- | --- |
| [README](https://github.com/in100tiva/KeyHybrid#readme) | Visão geral, pilares de segurança e quickstart |
| [Instalação](INSTALL.md) | Pré-requisitos, build do gateway, do app e empacotamento |
| [Uso](USAGE.md) | Gateway, aliases `kh-*`, CLI `kh run` e integração com agentes |
| [Especificação do cofre](SPEC-cofre-universal.md) | Modelo de dados, criptografia e ciclo de vida da chave mestra |
| [Segurança](https://github.com/in100tiva/KeyHybrid/blob/main/SECURITY.md) | Como reportar vulnerabilidades e escopo |
| [Contribuir](https://github.com/in100tiva/KeyHybrid/blob/main/CONTRIBUTING.md) | Ambiente, padrões e processo de PR |
| [Changelog](https://github.com/in100tiva/KeyHybrid/blob/main/CHANGELOG.md) | O que mudou em cada versão |

## Três formas de rodar

### 1. App desktop

Baixe o instalador na página de [Releases](https://github.com/in100tiva/KeyHybrid/releases), cadastre uma chave, defina o PIN e copie o token virtual do alias. O app já sobe o gateway embutido.

### 2. Gateway via Docker

```bash
docker run --rm -p 127.0.0.1:8090:8090 \
  -v keyhybrid-data:/data \
  ghcr.io/in100tiva/keyhybrid:latest
```

### 3. Compilando do código

```bash
git clone https://github.com/in100tiva/KeyHybrid.git
cd KeyHybrid
make build
```

## Compatibilidade

O gateway aceita clientes que falam o protocolo da OpenAI: SDKs oficiais, LangChain, LlamaIndex, Continue, Cursor, Aider, entre outros. Basta apontar `base_url` para `http://127.0.0.1:8090/v1` e usar o token virtual do alias.

## Licença

[MIT](https://github.com/in100tiva/KeyHybrid/blob/main/LICENSE) © Luan Oliveira dos Santos
