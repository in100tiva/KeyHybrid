---
title: "PR #31 — Importar .env para o cofre"
tipo: pr
data: 2026-11-23
autor: VictorNascimento14
projeto: KeyHybrid
pr: 31
url: https://github.com/in100tiva/KeyHybrid/pull/31
issue: https://github.com/in100tiva/KeyHybrid/issues/18
branch: feat/importar-env
tags: [pr, desktop, cofre, seguranca]
status: aberto
---

# PR #31 — Importar `.env` para o cofre

## Contexto

Issue [#18](https://github.com/in100tiva/KeyHybrid/issues/18): quem já tinha as chaves num `.env` precisava redigitar uma a uma, e é aí que elas costumam vazar (arquivo esquecido em pasta sincronizada, commit acidental). Este PR traz o `.env` para o cofre pelo mesmo caminho de cifra do cadastro manual.

## Mudanças

### Interface (`apps/desktop`)

- **Importar .env**, no dock e no estado vazio da lista, abre um estágio próprio: área mascarada (`-webkit-text-security`, sem corretor) onde se cola o conteúdo ou se solta o arquivo; prévia com apelido, provedor, variáveis e os 4 últimos caracteres, com opção de desmarcar; gravação; checklist final e o comando `kh run` pronto.
- `src/lib/envImport.ts` (novo, sem Svelte nem Tauri): `parseEnv` (lê como o dotenv), `planImport` (provedor, destino, apelido livre, máscara), `importRows` (grava em sequência pelo `save` e para no primeiro erro) e `khRunCommand`.
- `src/lib/providers.ts` (novo): `SERVICES`, os prefixos e `detectProvider` saíram da página, e o cadastro manual e a importação passam a usar a mesma tabela. Entram o serviço `openai` (`https://api.openai.com`) e os prefixos `sk-proj-`/`sk-svcacct-`.
- `tauri.conf.json`: `dragDropEnabled: false`, para o drop HTML5 chegar à página; a página barra arquivo solto fora da área.

### Gateway

- Nenhuma mudança. A importação usa `save_vault_key` → `POST /api/keyhybrid/keys` → `encryptSecretAtRest`.

### CI e ferramentas

- `pnpm run test` (`node --test` com o type stripping do Node 22, sem dependência), passo **Testes** no job da interface e no `make test`.

## Modelo de ameaça

| Pergunta | Resposta |
|---|---|
| Onde a chave fica antes de ir para o cofre? | Só na memória da tela (o texto colado), mascarada na tela; some ao concluir ou cancelar. A prévia não carrega a chave, só os 4 últimos caracteres. |
| Passa por disco, log ou armazenamento do navegador? | Não. Nada de `console`, `localStorage`, `sessionStorage` ou `fetch` no caminho: um teste espiona os quatro durante a importação, e uma guarda estática barra armazenamento do navegador e `console` no código da tela. A CSP já restringe a rede ao gateway local, e o teste trava isso. |
| Usa outro caminho de cifra? | Não. Cada chave vai pelo `save_vault_key`, o mesmo do cadastro manual: token local, PIN respeitado, AES-256-GCM no gateway. |
| A chave pode ir para o provedor errado? | O prefixo (quem emitiu a chave) vence o nome da variável. A OpenAI vira serviço com destino fixo, porque no `/v1` um provedor sem rota própria cai na OpenRouter ([#17](https://github.com/in100tiva/KeyHybrid/issues/17)). |
| Pode sobrescrever ou desativar algo do cofre? | Não sobrescreve: a gravação é upsert por apelido, então apelido em uso vira `-2`. Salvar chave de IA a torna ativa; a importação devolve a ativa de antes. |
| E se o arquivo solto não for um `.env`? | Acima de 64 KiB é recusado. Fora da área, o drop de arquivo é barrado: sem o handler nativo do Tauri, o WebView abriria o arquivo na janela. |

Limite que continua: string em JavaScript não se zera. O texto colado vive no heap do WebView até o coletor passar, como a chave do cadastro manual já vive hoje.

## Decisões técnicas

- **Parser e mapeamento na interface, não no Rust**: o texto colado já está no WebView, então levar o parser para o Rust não tiraria a chave do JS e ainda pediria um comando novo de leitura de arquivo, que aumenta a superfície. O módulo puro é testado com `node --test`.
- **OpenAI como serviço**: rota de IA para a OpenAI é a #17. Como serviço, `kh run -e OPENAI_API_KEY=openai` e o `/proxy/openai/` já funcionam, e a chave nunca passa pela OpenRouter. De quebra, o cadastro manual deixou de mandar para a OpenRouter uma chave `sk-proj-` colada com o provedor padrão.
- **`AIza` sozinho não vira Gemini**: é prefixo de qualquer API do Google (Maps, Firebase); só vale com nome de variável do Gemini.
- **Mesma chave em duas variáveis** (`GITHUB_TOKEN`/`GH_TOKEN`): uma entrada no cofre, as duas no `kh run`.
- **Para no primeiro erro** (cofre bloqueado, gateway fora): **Tentar de novo** retoma só o que faltou, com os mesmos apelidos, sem duplicar.
- **Testes sem dependência nova**: os `import "node:*"` do teste levam `@ts-expect-error`, como o `vite.config.js`, porque o projeto não tem `@types/node`.

## Armadilhas e aprendizados

- **O SvelteKit 3 removeu o `$lib`** (agora é `#lib`, pelo campo `imports` do `package.json`). A página importa `../lib/*.ts` por caminho relativo.
- **`dragDropEnabled`** vem `true` e deixa o drop de arquivo com o Tauri. Desligar sem uma guarda faz qualquer arquivo solto na janela abrir no lugar da interface.
- O gateway barra `POST` com `Sec-Fetch-Site: same-site` vindo do navegador: a gravação precisa da ponte Rust, como o cadastro manual já faz.
- O PocketBase usa WAL: para conferir o que chegou ao disco, olhe `data.db` **e** `data.db-wal`.

## Como testar

1. `cd apps/desktop && pnpm install --frozen-lockfile && pnpm run test && pnpm run check && pnpm run build`.
2. No app (`pnpm tauri dev`): **Importar .env** → cole um `.env` com três chaves ou solte o arquivo → confira a prévia → **Importar 3 chaves**.
3. Feche e reabra o app: as três continuam no cofre (critério de aceite da #18).
4. Com PIN configurado e o cofre bloqueado, a importação para com a mensagem e oferece **Desbloquear** e **Tentar de novo**.

### Verificação feita neste PR

- 10 testes de unidade, `svelte-check` (0 erros, 0 avisos) e build estático.
- Ponta a ponta com a interface real (`vite dev` no Chromium) e o gateway da release v0.1.0 num `KEYHYBRID_HOME` descartável; só a ponte Tauri foi trocada por uma função que faz o mesmo que o `gateway.rs`. 15 checagens: drop do arquivo, máscara, prévia, drop fora da área, arquivo grande, erro no meio com retomada, chave ativa preservada, apelido existente intocado, `kh run`, `data.db` e WAL sem chave em claro, nada em `localStorage`/`sessionStorage`/DOM/console, e as chaves de volta depois de reiniciar o gateway e reabrir a interface.
- **Não verificado localmente**: o app Tauri empacotado (a máquina não tem `webkit2gtk-4.1-dev`), o drop nativo no WebKitGTK/WebView2 e o `cargo clippy` com o `tauri.conf.json` novo — o job Rust do CI compila o `tauri-build`, que valida a configuração.

## Documentação afetada

- [`docs/USAGE.md` — Importar um `.env`](../USAGE.md#importar-um-env)
- [`CHANGELOG.md`](https://github.com/in100tiva/KeyHybrid/blob/main/CHANGELOG.md)
- [`CONTRIBUTING.md` — Testes](https://github.com/in100tiva/KeyHybrid/blob/main/CONTRIBUTING.md#testes)
