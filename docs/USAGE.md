# Uso — Gateway local

Como usar o KeyHybrid nos seus projetos, agentes e CLIs. Para instalação, veja [INSTALL.md](INSTALL.md).

## Configuração

Depois de salvar uma chave (ex.: `openrouter`) na interface, o KeyHybrid gera o token virtual **`kh-openrouter`**.

- **Base URL:** `http://127.0.0.1:8090/v1`
- **API key:** `kh-openrouter`

Funciona em qualquer SDK compatível com a API da OpenAI — basta trocar a base URL e a chave.

### Autenticação do `/v1/*`

Vale para todas as rotas `/v1` (chat, `/v1/models`, áudio e vídeo):

- **`Authorization: Bearer kh-<alias>`** de uma chave de IA cadastrada passa sem o token local (compatibilidade com clientes OpenAI). `kh-default` usa a chave ativa.
- **`kh-<alias>` que não existe** responde `404` com a lista dos aliases de IA cadastrados: `{"error": "alias <x> não cadastrado no KeyHybrid", "aliases": [...]}`. Nunca cai na chave ativa.
- **Qualquer outro caso** (outro Bearer, sem `Authorization`, só `X-Provider`) exige o token local em `X-KeyHybrid-Token` (ou como Bearer); sem ele, `401`. Com o token, vale a chave ativa ou o `X-Provider`.
- O campo `"alias"` no JSON do chat também precisa existir, senão `404` com a mesma lista.

## Chat / Completions

O endpoint `/v1/chat/completions` suporta **múltiplos provedores** nativamente (`anthropic`, `gemini`, `groq`, `openai`, `openrouter`).

### 1. Seleção de Provedor

Você pode selecionar o provedor de três formas, com a seguinte precedência:
1. **Pelo header `X-Provider`:** ex.: `-H "X-Provider: anthropic"`
2. **Pelo prefixo no model ID:** ex.: `"model": "anthropic/claude-3-5-sonnet"`, `"model": "groq/llama-3.3-70b-versatile"`, `"model": "gemini/gemini-2.0-flash"`
3. **Pelo token virtual de alias:** ex.: `Authorization: Bearer kh-anthropic`
4. **Fallback padrão (default):** Sem prefixo, header nem alias (ou com `kh-default`), o gateway envia para o provedor ativo (`active: true`) no cofre (ex.: OpenRouter). Alias explícito que não existe dá `404`, nunca o provedor ativo.

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

O agente usa só o `~/.keyhybrid/.cli.token` (proxy, `/v1`). O `kh run` lê sozinho o `~/.keyhybrid/.reveal.token`, o único aceito no `/api/keyhybrid/reveal`; nunca passe esse arquivo a um agente nem o use no proxy. Os dois mudam a cada boot do gateway: depois de atualizar o KeyHybrid, reinicie o gateway antes do primeiro `kh run`.

**`kh list`** mostra os aliases cadastrados, para o agente saber o que pode pedir ao `kh run` sem abrir o app:

```bash
kh list          # tabela: ALIAS, TIPO (IA/serviço), PROVEDOR, MODELO PADRÃO
kh list --json   # [{"alias":"openrouter","kind":"ai","provider":"openrouter","default_model":"openrouter/auto"}, …]
```

Usa o token local (`~/.keyhybrid/.cli.token`) e o mesmo `KH_GATEWAY` do `kh run`. Nunca mostra segredo, máscara, token virtual, URL base nem cabeçalho de autenticação; em chave de serviço, `default_model` vem vazio.

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

## Backup e restauração do cofre

O backup é **um arquivo único e cifrado** (`.khvault`) com o cofre inteiro: apelidos, provedores, segredos, modelos padrão, URLs base e cabeçalhos de autenticação. Você guarda onde quiser (pendrive, HD externo, Drive pessoal). Nada passa pela internet e o KeyHybrid não registra para onde o arquivo foi, a não ser o caminho no histórico local (veja [Status](#status-do-último-backup)).

Se o cofre tem PIN, ele precisa estar desbloqueado; senão aparece *"Cofre bloqueado. Digite seu PIN de 6 dígitos para autorizar o backup ou a restauração."*

### Fazer backup

1. No dock, clique em **Backup**. Abre a etapa **Backup e restauração**.
2. Clique em **Fazer backup**.
3. Digite a **senha do backup** duas vezes. O medidor mostra a força (Curta demais, Fraca, Razoável, Boa, Forte).
4. Clique em **Escolher onde salvar**. O diálogo nativo sugere `keyhybrid-backup-AAAAMMDD-HHMMSS.khvault` (horário local); se você tirar a extensão, o app põe `.khvault` de volta.
5. A tela confirma: *"Backup salvo em <caminho> com N chaves."*

### Senha do backup

- Pelo menos **12 caracteres** (contados como caracteres, não bytes: `á` conta 1). Abaixo disso, o app recusa.
- Pelo menos **6 caracteres diferentes** (`aaaaaaaaaaaa` é recusada).
- Com **12 a 15 caracteres**, misture pelo menos **3 tipos**: minúsculas, maiúsculas, números e símbolos (espaço conta como símbolo). `senha123senha` é recusada; `Cavalo-Bateria-Grampo` ou uma frase longa só com minúsculas passam.
- Essas regras valem só para **fazer** backup. Restaurar aceita a senha que o arquivo tiver, então backups antigos continuam abrindo.
- É **outra senha**, separada do PIN de 6 dígitos do cofre. O PIN não abre o backup.
- O PIN só tranca a interface do app: o servidor não o conhece, e o gateway e o `kh run` seguem funcionando com o app bloqueado. Quem tem acesso ao seu usuário do sistema passa por ele. Detalhes em [SECURITY.md](../SECURITY.md#modelo-de-ameaça-do-backup-do-cofre).
- **Não existe recuperação.** Perdeu a senha, perdeu o backup. O KeyHybrid não guarda cópia dela nem tem como quebrar o arquivo.
- As senhas ficam só na memória da tela e são apagadas ao concluir, cancelar, voltar ou falhar.

### Restaurar

1. No dock, **Backup** → **Restaurar backup**. Escolha o arquivo `.khvault` no diálogo nativo.
2. Digite a senha do backup e clique em **Ler backup**. Nada é gravado nesta etapa.
3. A **prévia** mostra a data do backup, a versão do app que o gerou, o total de chaves (de IA e de serviço), os provedores, os apelidos e os apelidos que **já existem** neste cofre. Nenhum segredo aparece.
4. Escolha o modo e clique em **Restaurar**:
   - **Mesclar** (padrão): acrescenta ao cofre atual só as chaves cujo apelido ainda não existe. Um apelido que já existe (sem diferenciar maiúscula de minúscula) é um **conflito**: a chave do cofre fica intocada e a do backup é ignorada. Se o cofre já tem uma chave de IA ativa, ela continua a mesma e as importadas entram inativas; se não tem, a primeira chave ativa do backup fica ativa.
   - **Substituir tudo**: apaga o cofre atual e grava o conteúdo do backup. Para liberar o botão, digite `SUBSTITUIR` (espaços nas pontas não contam). Antes de apagar qualquer coisa, o app grava um **backup de segurança** do cofre atual, cifrado com **a mesma senha** que você acabou de digitar, em `~/.keyhybrid/backups/keyhybrid-antes-de-restaurar-AAAAMMDD-HHMMSS.khvault` (horário UTC; no Windows, `%USERPROFILE%\.keyhybrid\backups\`). Se esse backup de segurança falhar, nada é apagado. Para voltar atrás, restaure esse arquivo.
5. O resultado mostra quantas chaves entraram, quantas há no cofre agora, as removidas (no Substituir), os conflitos e o caminho do backup de segurança.

Em qualquer modo, todas as chaves do arquivo são validadas pelas mesmas regras do cadastro manual antes da primeira escrita, e a gravação roda numa única transação: uma chave inválida, um apelido repetido dentro do arquivo ou um erro no meio deixam o cofre exatamente como estava. No máximo uma chave de IA fica ativa e chave de serviço nunca fica ativa.

### Status do último backup

A etapa mostra *"Último backup: <data>, com N chaves, em <caminho>."* (ou *"Nenhum backup feito ainda."*) e *"Última restauração: <data>, N chaves (Mesclar ou Substituir), de <caminho>."*. O histórico fica em `~/.keyhybrid/backup-status.json`, sem segredos:

```json
{
  "last_backup": { "at": "2026-11-26T12:00:00Z", "path": "/media/pendrive/keyhybrid-backup-20261126-090000.khvault", "items": 3 },
  "last_restore": { "at": "2026-11-26T12:05:00Z", "path": "/media/pendrive/keyhybrid-backup-20261126-090000.khvault", "items": 3, "mode": "merge" }
}
```

O backup de segurança do Substituir não entra nesse histórico.

### Formato do arquivo `.khvault`

| Offset | Tamanho | Campo |
| --- | --- | --- |
| 0 | 8 | Magic `KHVAULT\0` (`4B 48 56 41 55 4C 54 00`) |
| 8 | 1 | Versão do arquivo (`1`) |
| 9 | 1 | Flags (`0`; qualquer outro valor é rejeitado) |
| 10 | 16 | Salt do Argon2id (aleatório por arquivo) |
| 26 | 12 | Nonce do AES-256-GCM (aleatório por arquivo) |
| 38 | N + 16 | Texto cifrado seguido da tag GCM de 16 bytes |

- **Chave:** Argon2id versão `0x13`, `m=65536` KiB (64 MiB), `t=3`, `p=4`, saída de 32 bytes, sobre a senha (UTF-8) e o salt. São os mesmos parâmetros do PIN do cofre.
- **Cifra:** AES-256-GCM. Os 38 bytes do cabeçalho entram como AAD: mexer em qualquer byte do cabeçalho ou do corpo derruba a autenticação.
- **Limite:** arquivos acima de 16 MiB são recusados antes de ler.

O texto decifrado é este envelope JSON (segredos de exemplo, falsos):

```json
{
  "format": "keyhybrid-backup",
  "version": 1,
  "created_at": "2026-11-26T12:00:00Z",
  "app_version": "0.1.0",
  "vault": {
    "format": "keyhybrid-vault",
    "version": 1,
    "entries": [
      { "alias": "openrouter", "provider": "openrouter", "kind": "ai", "secret": "sk-or-v1-teste-0000000000000000",
        "base_url": "", "default_model": "openrouter/auto", "active": true, "auth_header": "", "auth_prefix": "" },
      { "alias": "supabase", "provider": "supabase", "kind": "service", "secret": "sbp_teste_0000000000000000",
        "base_url": "https://api.supabase.com", "default_model": "", "active": false, "auth_header": "", "auth_prefix": "" }
    ]
  }
}
```

### Subcomandos `vault-dump` e `vault-apply`

O app desktop não lê o banco diretamente: ele chama o `keyhybrid-server` com o mesmo `KEYHYBRID_HOME` e troca o JSON do campo `vault` por pipe. Não existe rota HTTP de backup, export ou import no gateway.

Os dois comandos exigem, na **primeira linha do stdin**, o conteúdo de `~/.keyhybrid/.reveal.token` (gerado no boot do gateway). Sem ele, ou com o `.cli.token` no lugar, saem com código `2` sem tocar no cofre. O app envia o token sozinho; se o arquivo não existe, abra ou reinicie o gateway.

- `keyhybrid-server vault-dump` escreve o cofre **em claro** no stdout, no formato `{"format":"keyhybrid-vault","version":1,"entries":[…]}`. Não aceita argumentos e se recusa a rodar com o stdout num terminal. Se o cofre não existe, sai com código 1 (*"cofre não encontrado em <pasta>"*) sem criar pasta, banco nem chave mestra. Redirecionar para arquivo funciona, mas grava os segredos em claro no disco: não faça isso fora do app.
- `keyhybrid-server vault-apply --mode merge|replace [--dry-run]` lê, depois da linha do token, esse mesmo JSON do stdin (até 16 MiB), valida e grava numa transação. Recusa stdin de terminal. Com `--dry-run`, só calcula o resultado (é o que a prévia usa) e, sem cofre, calcula contra um cofre vazio sem criar nada; só a aplicação de verdade cria o cofre numa instalação limpa. No stdout devolve o relatório, sem segredos:

```json
{"mode":"merge","dry_run":false,"added":["openrouter"],"conflicts":["supabase"],"removed":0,"total_after":2}
```

| Código de saída | Significado |
| --- | --- |
| `0` | Sucesso |
| `1` | Falha: cofre não abriu, chave não decifrou, entrada ilegível, JSON inválido, entrada maior que 16 MiB, chave inválida, apelido repetido ou erro na transação |
| `2` | Uso errado: argumento desconhecido, `--mode` ausente ou inválido, stdout (dump) ou stdin (apply) é um terminal, token de revelação ausente ou inválido na 1ª linha do stdin |
| `3` | `format`/`version` do dump não suportado |

As mensagens vão para o stderr e nunca contêm segredo; o app mostra a última linha delas.

### Mensagens de erro

| Mensagem | O que significa |
| --- | --- |
| A senha do backup precisa de pelo menos 12 caracteres. | Senha curta ao fazer backup. |
| A senha do backup precisa de pelo menos 6 caracteres diferentes. | Senha repetitiva ao fazer backup (ex.: `aaaaaaaaaaaa`). |
| Com menos de 16 caracteres, a senha do backup precisa misturar 3 tipos: minúsculas, maiúsculas, números e símbolos (ou espaço). | Senha de 12 a 15 caracteres com só 1 ou 2 tipos ao fazer backup. |
| As duas senhas não são iguais. | A confirmação não bate com a senha. |
| Senha incorreta ou arquivo alterado. | A senha está errada, ou o arquivo foi modificado ou truncado depois do cabeçalho. O app não distingue os dois casos de propósito. |
| Arquivo corrompido ou não é um backup do KeyHybrid. | Magic, versão `0`, flags ou JSON interno inválidos, ou arquivo menor que o mínimo. |
| Backup de versão mais nova: atualize o KeyHybrid para restaurar. | O arquivo foi gerado por uma versão do formato que este app ainda não conhece. |
| Arquivo maior que 16 MiB: não é um backup do KeyHybrid. | Arquivo grande demais; recusado sem decifrar. |
| Para substituir tudo, digite SUBSTITUIR. | A confirmação do Substituir está faltando ou diferente. |
| Cofre bloqueado. Digite seu PIN de 6 dígitos para autorizar o backup ou a restauração. | O cofre tem PIN e está bloqueado. |
| Atualize o KeyHybrid: o servidor instalado não tem o comando de backup. | O `keyhybrid-server` ao lado do app é de uma versão sem `vault-dump`/`vault-apply`. |
| Servidor do KeyHybrid não encontrado ao lado do app. | O binário do gateway não foi achado. |
| Token de revelação ausente: abra ou reinicie o gateway do KeyHybrid e tente de novo. | Não existe `~/.keyhybrid/.reveal.token`: o gateway desta versão ainda não subiu nenhuma vez. |
| O servidor recusou a operação (`<código>`): `<motivo>` | O `keyhybrid-server` saiu com erro; o motivo é a última linha do stderr (ex.: `vault-apply: chave "x" inválida: …`). |
| Não foi possível salvar o backup / a cópia de segurança: … | Falha de disco ou permissão no destino. |
| Histórico de backup ilegível (backup-status.json). | O arquivo de status foi editado à mão ou corrompeu; apague-o para recomeçar o histórico. |

## Atualizar o KeyHybrid no Linux

O `install_linux.sh` deixa o gateway como serviço do usuário (`keyhybrid.service`). Com o serviço habilitado, o app e o `run_linux.sh` pedem ao systemd para subir o gateway, em vez de rodar um `keyhybrid-server` próprio. Para trocar o binário sem perder o token local nem o cofre:

1. Faça um backup em **Backup → Fazer backup** (veja a seção acima).
2. Feche o app do KeyHybrid.
3. `systemctl --user stop keyhybrid.service`
4. Troque o binário: rode `./install_linux.sh` de novo, ou copie o `keyhybrid-server` novo para `~/.keyhybrid/bin/`.
5. `systemctl --user start keyhybrid.service` e confira com `systemctl --user status keyhybrid.service`.
6. Abra o app. A lista de chaves deve ser a mesma de antes.

O token local (`~/.keyhybrid/.cli.token`) é trocado a cada início do gateway, mas só depois que ele conseguiu a porta 8090. Uma segunda instância que encontra a porta ocupada sai com `porta 127.0.0.1:8090 ocupada (outro KeyHybrid já está rodando?); token local mantido` e não mexe no token de quem está atendendo.

## Limitações conhecidas

- Um processo malicioso rodando com o seu usuário ainda pode ler a chave.
- Não há PIN para liberar chave de produção.
- APIs que exigem dois cabeçalhos com a chave (ex.: PostgREST do Supabase, `apikey` + `Authorization`) não funcionam; a Management API do Supabase funciona.
- A interface usa apenas `Authorization: Bearer`; cabeçalho customizado só pela API.
- No Windows, `kh run` funciona via binário (`keyhybrid-server.exe run …`); o atalho `kh` é só Linux.
- O cofre protege a chave em repouso e no uso por ferramentas externas — ele não isola o seu próprio usuário do sistema.
