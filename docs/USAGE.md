# Uso — Gateway local

Como usar o KeyHybrid nos seus projetos, agentes e CLIs. Para instalação, veja [INSTALL.md](INSTALL.md).

## Configuração

Depois de salvar uma chave (ex.: `openrouter`) na interface, o KeyHybrid gera o token virtual **`kh-openrouter`**.

- **Base URL:** `http://127.0.0.1:8090/v1`
- **API key:** `kh-openrouter`

Funciona em qualquer SDK compatível com a API da OpenAI — basta trocar a base URL e a chave.

## Chat / Completions

O endpoint `/v1/chat/completions` suporta **múltiplos provedores** nativamente (`anthropic`, `gemini`, `groq`, `openai`, `openrouter`).

### 1. Seleção de Provedor

Você pode selecionar o provedor de três formas, com a seguinte precedência:
1. **Pelo header `X-Provider`:** ex.: `-H "X-Provider: anthropic"`
2. **Pelo prefixo no model ID:** ex.: `"model": "anthropic/claude-3-5-sonnet"`, `"model": "groq/llama-3.3-70b-versatile"`, `"model": "gemini/gemini-2.0-flash"`
3. **Pelo token virtual de alias:** ex.: `Authorization: Bearer kh-anthropic`
4. **Fallback padrão (default):** Se nenhum prefixo ou header for informado, o gateway envia para o provedor ativo (`active: true`) no cofre (ex.: OpenRouter), mantendo 100% de retrocompatibilidade.

### 2. Conversando com a Anthropic pelo SDK oficial da OpenAI

O gateway traduz automaticamente o dialeto da OpenAI para a API nativa da Anthropic (`/v1/messages`), incluindo mensagens de sistema, alternância de turnos, contagem de tokens e streaming SSE.

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:8090/v1",
    api_key="kh-default", # ou kh-anthropic
)

response = client.chat.completions.create(
    model="anthropic/claude-3-5-sonnet",
    messages=[
        {"role": "system", "content": "Você é um assistente técnico."},
        {"role": "user", "content": "Explique o funcionamento do KeyHybrid."}
    ],
    temperature=0.7,
)

print(response.choices[0].message.content)
```

Ou com streaming em tempo real:

```python
stream = client.chat.completions.create(
    model="anthropic/claude-3-5-sonnet",
    messages=[{"role": "user", "content": "Escreva um poema curto sobre segurança."}],
    stream=True,
)

for chunk in stream:
    if chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="", flush=True)
```

### 3. Chamada via cURL

```bash
curl http://127.0.0.1:8090/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer kh-openrouter" \
  -d '{
    "model": "anthropic/claude-3-5-sonnet",
    "messages": [
      {"role": "user", "content": "Explique a arquitetura Zero-Knowledge do KeyHybrid."}
    ]
  }'
```

## Listagem de Modelos (`/v1/models`)

O endpoint `GET /v1/models` agrega automaticamente todos os modelos disponíveis nos provedores configurados e habilitados no seu cofre:

```bash
curl http://127.0.0.1:8090/v1/models \
  -H "Authorization: Bearer kh-default"
```

A resposta retorna a lista no padrão OpenAI contendo os IDs prefixados (ex.: `anthropic/claude-3-5-sonnet`, `gemini/gemini-2.0-flash`, `groq/llama-3.3-70b-versatile`) e os modelos do provedor default também sem prefixo.

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
- A chave de OpenAI (`OPENAI_API_KEY`) pode ser usada tanto diretamente pelo gateway `/v1` quanto como serviço via `kh run` e `/proxy`.
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
