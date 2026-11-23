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

## Importar um `.env`

Quem já tem as chaves num `.env` não precisa redigitar: **Importar .env**, no dock da interface, abre uma área onde você cola o conteúdo ou solta o arquivo. O texto fica mascarado na tela e só na memória do app.

A prévia mostra, para cada chave reconhecida, o apelido, o provedor e os 4 últimos caracteres; desmarque o que não quiser trazer. A gravação usa o mesmo caminho do cadastro manual (AES-256-GCM no gateway).

| Variável | Entra como |
| --- | --- |
| `OPENROUTER_API_KEY` | IA · `openrouter` |
| `ANTHROPIC_API_KEY` | IA · `anthropic` |
| `GEMINI_API_KEY`, `GOOGLE_API_KEY`, `GOOGLE_GENERATIVE_AI_API_KEY` | IA · `gemini` |
| `OPENAI_API_KEY` | serviço · `openai` (`https://api.openai.com`) |
| `GITHUB_TOKEN`, `GH_TOKEN` | serviço · `github` |
| `SUPABASE_ACCESS_TOKEN` | serviço · `supabase` |
| `VERCEL_TOKEN` | serviço · `vercel` |

- O prefixo da chave vence o nome da variável: `OPENAI_API_KEY=sk-or-…` entra como OpenRouter. A chave só vai para quem a emitiu.
- A OpenAI entra como serviço porque o `/v1` ainda não tem rota própria para ela ([#17](https://github.com/in100tiva/KeyHybrid/issues/17)): use `kh run` ou o proxy.
- Apelido já usado no cofre não é sobrescrito (a chave nova vira `openai-2`), e a chave ativa continua a mesma.
- O que não é chave conhecida continua só no `.env` e aparece na prévia como "ficam no .env".

No fim, a interface mostra a checklist — guardar o que ficou de fora, apagar o `.env`, gerar chaves novas e só então revogar as antigas — e copia o comando que roda o projeto sem o arquivo:

```bash
kh run -e OPENAI_API_KEY=openai -e ANTHROPIC_API_KEY=anthropic -- npm run dev
```

## Limitações conhecidas

- Um processo malicioso rodando com o seu usuário ainda pode ler a chave.
- Não há PIN para liberar chave de produção.
- APIs que exigem dois cabeçalhos com a chave (ex.: PostgREST do Supabase, `apikey` + `Authorization`) não funcionam; a Management API do Supabase funciona.
- A interface usa apenas `Authorization: Bearer`; cabeçalho customizado só pela API.
- No Windows, `kh run` funciona via binário (`keyhybrid-server.exe run …`); o atalho `kh` é só Linux.
- O cofre protege a chave em repouso e no uso por ferramentas externas — ele não isola o seu próprio usuário do sistema.
