# Uso — Gateway local

Como usar o KeyHybrid nos seus projetos, agentes e CLIs. Para instalação, veja [INSTALL.md](INSTALL.md).

## Configuração

Depois de salvar uma chave (ex.: `openrouter`) na interface, o KeyHybrid gera o token virtual **`kh-openrouter`**.

- **Base URL:** `http://127.0.0.1:8090/v1`
- **API key:** `kh-openrouter`

Funciona em qualquer SDK compatível com a API da OpenAI — basta trocar a base URL e a chave.

## Chat / Completions

```bash
curl http://127.0.0.1:8090/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer kh-openrouter" \
  -d '{
    "model": "openrouter/auto",
    "messages": [
      {"role": "user", "content": "Explique a arquitetura Zero-Knowledge do KeyHybrid."}
    ]
  }'
```

## Text-to-Speech

```bash
curl http://127.0.0.1:8090/v1/audio/speech \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer kh-openrouter" \
  -d '{
    "model": "elevenlabs/eleven-v4-turbo",
    "input": "Cofre KeyHybrid operacional, chefe.",
    "voice": "alice",
    "response_format": "mp3"
  }' --output resposta.mp3
```

## Geração de vídeo (assíncrona)

```bash
curl http://127.0.0.1:8090/v1/videos \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer kh-openrouter" \
  -d '{
    "model": "heygen/heygen-video-1",
    "prompt": "Apresentador em estúdio futurista falando sobre cibersegurança."
  }'
```

## Chaves de serviço (Vercel, Supabase, GitHub…)

Para APIs que não são de IA, escolha no seletor **Provedor** um item do grupo **Serviços** (na API, `kind: "service"`), informando alias e URL base (`https://`). O segredo não sai do KeyHybrid.

**Proxy:**

```bash
curl -H "X-KeyHybrid-Token: $(cat ~/.keyhybrid/.cli.token)" \
  http://127.0.0.1:8090/proxy/<alias>/v9/projects
```

**`kh run`** injeta a variável só no processo filho, para CLIs que não aceitam trocar a URL da API:

```bash
kh run --env VERCEL_TOKEN=<alias> -- vercel deploy
```

Fora de terminal (agente, pipe, log) o segredo sai mascarado como `****`; em terminal interativo a saída passa direto.

## Limitações conhecidas

- Um processo malicioso rodando com o seu usuário ainda pode ler a chave.
- Não há PIN para liberar chave de produção.
- APIs que exigem dois cabeçalhos com a chave (ex.: PostgREST do Supabase, `apikey` + `Authorization`) não funcionam; a Management API do Supabase funciona.
- A interface usa apenas `Authorization: Bearer`; cabeçalho customizado só pela API.
- No Windows, `kh run` funciona via binário (`keyhybrid-server.exe run …`); o atalho `kh` é só Linux.
- O cofre protege a chave em repouso e no uso por ferramentas externas — ele não isola o seu próprio usuário do sistema.
