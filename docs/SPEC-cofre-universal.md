# SPEC — Cofre universal: qualquer chave, sem vazar no terminal

> Branch `feat/cofre-universal` · base `origin/main` (`b6c21b9`) · 26/10/2026

## 1. A dor

O KeyHybrid deveria guardar **qualquer** segredo (Vercel, Supabase, GitHub, Cloudflare…)
e servir de intermediário, para que a chave nunca apareça no terminal, num `.env`, no
histórico do shell ou na conversa com um agente de IA. Hoje ele só sabe lidar com chave
de IA, e tem quatro furos:

| # | Furo | Onde | Efeito |
|---|---|---|---|
| F1 | CORS `*` e nenhuma autenticação | `main.go:227-236` | qualquer site aberto no navegador chama `127.0.0.1:8090`: gasta a OpenRouter, troca e apaga chaves |
| F2 | Sem checagem de `Host` | mesmo ponto | DNS rebinding contorna até um CORS correto |
| F3 | Toda chave é tratada como IA | `main.go:425-466`, `571-605`, `friday/assist` | chave do Supabase cadastrada vira "openrouter" e é enviada como `Bearer` para `openrouter.ai` |
| F4 | Nenhum jeito de usar chave fora de IA sem copiá-la | — | o usuário cola a chave no terminal, que é exatamente a dor |

## 2. Modelo de ameaça (o que protegemos e o que não)

- **Protegemos de**: site malicioso no navegador (F1/F2); chave indo para o destino errado
  (F3); chave impressa em terminal, log ou transcrição de agente (F4).
- **Não protegemos de**: processo malicioso rodando **como o mesmo usuário**. Ele já lê
  `~/.keyhybrid/.master.key` e o `pb_data`; nenhum token local muda isso. Fica registrado
  como limite conhecido (seção 8).

## 3. Tipos de chave

Campo novo `kind` na coleção `vault_keys`:

| kind | Uso | Rotas que a usam |
|---|---|---|
| `ai` (padrão; vazio = `ai`) | OpenRouter, Gemini, Anthropic — como hoje | `/v1/*`, `/api/ai/proxy/chat`, Sexta-Feira |
| `service` | qualquer API: Vercel, Supabase, GitHub, outra | `/proxy/{alias}/…` e `kh run` |

Campos novos: `kind`, `auth_header` (padrão `Authorization`), `auth_prefix` (padrão `Bearer `).
`base_url` já existe; para `service` ele é **obrigatório** e é o **único** destino da chave.

Regras de cadastro (`POST /api/keyhybrid/keys`) para `service`:
- `base_url`: `https://`, com host, sem usuário/senha, sem query, sem fragmento; barra final removida.
- `alias`: `^[A-Za-z0-9._-]{1,64}$` (vai na URL e no `kh run`).
- `auth_header`: `^[A-Za-z0-9-]{1,64}$`; vazio → `Authorization` + prefixo `Bearer `.
- Não mexe no `active` das outras chaves e fica `active=false` (o "ativo" é só de IA).

## 4. Correções no que existe

- **C1 (F1/F2) — guarda na entrada**, primeiro middleware, substitui o CORS atual:
  - `Host` precisa ser `127.0.0.1`, `localhost` ou `[::1]` (qualquer porta) → senão `403`.
  - Se houver `Origin`, ele precisa estar na lista: `tauri://localhost`,
    `http://tauri.localhost`, `https://tauri.localhost`, `http://localhost:1420`,
    `http://127.0.0.1:1420` ou a própria origem do servidor (`http://<Host>`, painel `/_/`)
    → senão `403`. Sem `Origin` (curl, Rust, Python, agentes) passa.
  - `Access-Control-Allow-Origin` devolve **a origem permitida**, nunca `*`; `Vary: Origin`;
    `Allow-Headers` inclui `X-KeyHybrid-Token`.
- **C2 (F3)** — toda busca de chave para IA (`proxyChatHandler`, `resolveVaultSecret`,
  `friday/assist`, auto-ativação do `GET /keys`) só enxerga `kind != "service"`.
  `POST /keys/{id}/activate` recusa chave de serviço (`400`).
- **C3** — `GET /keys` devolve também `kind`, `base_url` e `auth_header` (nada disso é
  segredo); `virtual_token` fica vazio para `service`.
- **C4** — `go.mod`: `go 1.27` → `go 1.27.0` (com `go 1.27` o download automático do
  toolchain falha: "toolchain not available").

## 5. Token local

`~/.keyhybrid/.cli.token`: 32 bytes aleatórios em hex, permissão `0600`, criado no boot se
não existir (falha do gerador aleatório = erro fatal, nunca semente fixa). Exigido no
cabeçalho `X-KeyHybrid-Token` pelas duas rotas novas, comparado em tempo constante.
Motivo: quem só consegue fazer uma requisição GET a `127.0.0.1` (SSRF de outro programa
local) não usa a chave.

## 6. Rotas novas

### 6.1 `ANY /proxy/{alias}/{path...}` — usar a chave sem vê-la
1. `X-KeyHybrid-Token` válido, senão `401`.
2. `alias` (sem diferenciar maiúsculas) precisa ser `kind=service`, senão `404`
   (mesma resposta para "não existe" e "é de IA").
3. Destino = `base_url` + resto do caminho (o caminho **escapado** da requisição) + query.
   Recusa (`400`) se o destino final tiver esquema, host ou porta diferentes do `base_url`,
   ou se o caminho sair do prefixo do `base_url` (`..`, `//evil.com`, `%2F%2F`, `@`).
4. Repassa método, corpo e cabeçalhos, **menos**: `Authorization`, `Cookie`, `Host`,
   `Origin`, `Referer`, `X-KeyHybrid-Token`, `Proxy-*`, hop-by-hop e o `auth_header` da chave.
   Põe `auth_header: auth_prefix + segredo`.
5. **Não segue redirecionamento** (devolve o 3xx ao cliente): cabeçalho como `apikey` não
   é removido pelo Go num redirect para outro host.
6. Devolve status, cabeçalhos (menos `Set-Cookie` e hop-by-hop) e corpo em streaming.
   Timeout 120 s.
7. Log: alias, método, caminho, status. Nunca segredo, nunca query.

### 6.2 `POST /api/keyhybrid/reveal` — só para o `kh run`
- Corpo `{"aliases":["vercel","supabase"]}` → `{"secrets":{"vercel":"…","supabase":"…"}}`.
- Exige `X-KeyHybrid-Token`; **recusa qualquer requisição com `Origin`** (`403`), inclusive
  da própria interface: navegador nunca recebe segredo em claro.
- Alias inexistente → `404` com a lista dos que faltam. Vale para `ai` e `service`.
- `Cache-Control: no-store`. Log só com os nomes.

## 7. `kh run` — CLIs que não aceitam trocar a URL da API

```bash
kh run --env VERCEL_TOKEN=vercel --env SUPABASE_ACCESS_TOKEN=supabase -- vercel deploy
```

- `kh` = link em `~/.local/bin/kh` → `~/.keyhybrid/bin/keyhybrid-server` (criado pelo
  `install_linux.sh`). O `main()` trata `run` **antes** de criar o PocketBase, para não
  abrir um `pb_data` vazio ao lado do binário.
- `--env NOME=alias` (ou `-e`) repetível; `NOME` em `^[A-Za-z_][A-Za-z0-9_]*$`; `--` separa
  o comando. Gateway em `KH_GATEWAY` (padrão `http://127.0.0.1:8090`).
- Chama `/api/keyhybrid/reveal`, roda o comando com o ambiente atual + as variáveis,
  stdin herdado, devolve o código de saída do filho.
- **Máscara**: quando stdout/stderr **não** é terminal (agente, pipe, log), cada segredo
  com 8+ caracteres sai como `****`, inclusive quando chega partido entre duas escritas.
  Em terminal interativo a saída passa direto (prompts do `vercel login` funcionam).

## 8. Fora de escopo (limites conhecidos)

- Processo malicioso como o mesmo usuário (seção 2). Caminho: chave mestra no keyring do SO.
- PIN para liberar chave de produção no `kh run`/proxy.
- API que precisa de **dois** cabeçalhos com a chave (PostgREST do Supabase: `apikey` +
  `Authorization`). A Management API (`api.supabase.com`) funciona.
- Interface para cabeçalho customizado: a API aceita `auth_header`/`auth_prefix`; a tela
  usa o padrão `Authorization: Bearer`.
- Windows: `kh run` funciona pelo binário (`keyhybrid-server.exe run …`); o atalho `kh` é só Linux.

## 9. Interface (`apps/desktop`)

- Seletor de provedor ganha os serviços `vercel`, `supabase`, `github`, `outro`.
  Predefinidos: `https://api.vercel.com`, `https://api.supabase.com`, `https://api.github.com`;
  `outro` pede a URL. Campo "URL base" aparece só para serviço (editável).
- Salvar envia `kind` e `base_url`, nos dois caminhos (`fetch` e `invoke("save_vault_key")`).
  O comando Rust passa os dois adiante; sem isso ele regravaria a chave como IA.
- Lista: chave de serviço mostra o selo `serviço · <provedor>` e não vira "ativa".
- Passo 2 de uma chave de serviço: copiar `kh run --env <VAR>=<alias> -- <comando>`,
  a URL `http://127.0.0.1:8090/proxy/<alias>/` e um curl de exemplo com
  `-H "X-KeyHybrid-Token: $(cat ~/.keyhybrid/.cli.token)"`. `<VAR>` sugerida:
  `VERCEL_TOKEN`, `SUPABASE_ACCESS_TOKEN`, `GH_TOKEN`, ou `<ALIAS>_TOKEN`.

## 10. Critérios de aceite

| # | Critério | Como verificar |
|---|---|---|
| A1 | build, vet e testes do servidor passam | `cd apps/server && go build ./... && go vet ./... && go test ./... -count=1` |
| A2 | site de fora é barrado | `curl -H 'Origin: https://evil.example' :8090/api/keyhybrid/keys` → 403 |
| A3 | DNS rebinding é barrado | `curl -H 'Host: evil.example:8090' …/api/keyhybrid/keys` → 403 |
| A4 | interface continua funcionando | `Origin: http://localhost:1420` → 200 com `Access-Control-Allow-Origin` igual |
| A5 | chave de serviço nunca vai para IA | cadastrar `service` única + `POST /v1/chat/completions` com `Bearer kh-<alias>` → não usa a chave de serviço |
| A6 | proxy injeta a chave e trava o destino | teste Go com `httptest`: cabeçalho chega; `..`/`//host`/`%2F%2F` → 400; 3xx não é seguido |
| A7 | proxy e reveal sem token → 401; reveal com Origin → 403 | curl |
| A8 | máscara funciona com segredo partido entre escritas | teste Go do escritor de máscara |
| A9 | `kh run` entrega a variável e mascara | `kh run -e T=<alias> -- sh -c 'echo $T' \| cat` → `****` |
| A10 | interface compila | `cd apps/desktop && bun run check && bun run build`; `cargo check` em `src-tauri` |
