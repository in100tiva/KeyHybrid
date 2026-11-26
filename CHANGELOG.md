# Changelog

Todas as mudanças relevantes deste projeto são documentadas neste arquivo.

O formato segue [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/) e o projeto adere ao [Versionamento Semântico](https://semver.org/lang/pt-BR/).

## [Não publicado]

### Adicionado

- **Instaladores desktop**: cada release `v*` passa a publicar `KeyHybrid_<versão>_amd64.deb`, `KeyHybrid_<versão>_x64_en-US.msi` e `KeyHybrid_<versão>_x64-setup.exe` com o `keyhybrid-server` embutido, cobertos por `SHA256SUMS.txt` e pela atestação de procedência.
- **CI do desktop em Rust**: job `Desktop Rust (Tauri)` no `.github/workflows/ci.yml`, com `cargo fmt --all -- --check` e `cargo clippy --all-targets --locked -- -D warnings` no `ubuntu-latest`, bibliotecas do Tauri v2 (WebKitGTK 4.1) e cache do Cargo.
- **Revisão de dependências**: `.github/workflows/dependency-review.yml` comenta no PR e bloqueia merges que introduzam dependências com severidade alta.
- **Política de segurança**: seção sobre os dois advisories sem correção upstream (`glib` e `disintegration/imaging`) e por que eles não são alcançáveis no KeyHybrid.
- **Procedência dos artefatos**: os binários e checksums de cada release recebem *build provenance* assinado pelo GitHub (`actions/attest-build-provenance`) e a imagem do GHCR passa a ser publicada com `provenance: mode=max` e SBOM.
- **Notas de release categorizadas**: `.github/release.yml` agrupa as mudanças por área (segurança, interface, gateway, dependências, CI e documentação).
- **OpenSSF Scorecard**: análise semanal da postura de segurança do repositório, publicada no code scanning e no painel público do Scorecard.
- **Bundle de procedência no release**: cada release passa a anexar `provenance.intoto.jsonl` com a atestação de build gerada pelo GitHub, permitindo conferir a origem dos binários com `gh attestation verify --bundle provenance.intoto.jsonl --repo in100tiva/KeyHybrid`.
- **Importar `.env` para o cofre** ([#18](https://github.com/in100tiva/KeyHybrid/issues/18)): **Importar .env** no dock (e no estado vazio da lista) abre uma área mascarada onde se cola o conteúdo ou se solta o arquivo. As chaves conhecidas (`OPENROUTER_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`/`GOOGLE_API_KEY`, `OPENAI_API_KEY`, `GITHUB_TOKEN`/`GH_TOKEN`, `SUPABASE_ACCESS_TOKEN`, `VERCEL_TOKEN`) aparecem numa prévia com provedor, apelido e os 4 últimos caracteres e são gravadas pelo mesmo `save_vault_key` do cadastro manual (AES-256-GCM no gateway). O prefixo da chave vence o nome da variável, a importação nunca sobrescreve um apelido existente e devolve a chave ativa de antes; no fim, uma checklist (apagar o `.env`, girar as chaves) e o comando `kh run` para rodar o projeto sem o arquivo.
- **Backup e restauração do cofre** ([#26](https://github.com/in100tiva/KeyHybrid/issues/26)): **Backup** no dock abre a etapa *Backup e restauração*, que salva o cofre inteiro num arquivo único `.khvault` (Argon2id `m=64 MiB, t=3, p=4` + AES-256-GCM com o cabeçalho como AAD, salt e nonce novos por arquivo) protegido por uma senha própria de pelo menos 12 caracteres, sem recuperação, e restaura com prévia sem segredos em dois modos: **Mesclar** (padrão; apelidos que já existem ficam intocados e aparecem como conflito) ou **Substituir tudo** (exige digitar `SUBSTITUIR` e grava antes um backup de segurança em `~/.keyhybrid/backups/`). O `keyhybrid-server` ganhou os subcomandos `vault-dump` e `vault-apply` (pipe, sem rota HTTP, validação pelas regras do cadastro e gravação numa única transação), os diálogos são abertos pelo Rust sem permissão de diálogo no webview, o status do último backup fica em `~/.keyhybrid/backup-status.json` e o CI passa a rodar `cargo test`.
- **Testes da interface**: `pnpm run test` em `apps/desktop` (`node --test`, sem dependência nova), também no CI e no `make test`.

### Alterado

- **PocketBase no novo endereço**: o módulo saiu do GitHub (repositório arquivado) para `ganigeorgiev.com/pocketbase/pocketbase`; o gateway sobe da v0.40.6 para a v0.42.3 (com `dbx` e `ozzo-validation` também nos caminhos novos) e passa a compilar com Go 1.27.4 (`toolchain` no `go.mod`). A v0.42 trouxe um middleware global que responde 401 a todo `Authorization` que não seja token do PocketBase: ele é desligado no gateway, porque o `/v1` e o `kh run` usam `Bearer kh-<alias>` e o token local, e um teste trava essa regra para as próximas atualizações do Dependabot.
- **`workflow_dispatch` do `release.yml`**: disparos manuais passam a só ensaiar o build dos binários e instaladores (gerando os artifacts da execução), sem publicar uma release fora de tags `v*`; tags de pré-release publicam só o gateway.
- **SvelteKit 3**: interface migrada de `@sveltejs/kit` 2.x para 3.4 e de `@sveltejs/adapter-static` 3.x para 4.x. A configuração deixou de ficar em `svelte.config.js` (arquivo removido) e passou para o plugin `sveltekit()` em `apps/desktop/vite.config.js`; o `tsconfig.json` agora estende o `$app/tsconfig` gerado pelo próprio kit.
- `make check` e `make fmt` passam a cobrir o código Rust (`cargo fmt`), e o `docs/INSTALL.md` documenta o requisito de **Node.js 22.17+** e `pnpm` 10 no lugar de `npm`.
- **Tokens de menor privilégio**: nenhum workflow declara permissão de escrita no nível do arquivo; `codeql.yml`, `container.yml`, `dependency-review.yml` e `release.yml` concentram a escrita apenas no job que realmente precisa dela, e os demais rodam com `contents: read`.
- **Actions fixadas por SHA**: todas as `uses:` dos workflows estão presas ao commit SHA da versão usada (com a tag ao lado, em comentário), e as imagens base do `Dockerfile` estão presas ao digest SHA-256 da imagem — o Dependabot continua atualizando as duas coisas.
- **Drag-and-drop da janela**: `dragDropEnabled: false` no `tauri.conf.json`, para o drop HTML5 do `.env` chegar à interface; soltar um arquivo fora da área de importação é barrado e não abre o arquivo na janela.

### Corrigido

- **Token local trocado por uma segunda instância do gateway** ([#37](https://github.com/in100tiva/KeyHybrid/issues/37)): o gateway agora abre a porta 8090 antes de rotacionar o `~/.keyhybrid/.cli.token`. Uma instância que encontra a porta ocupada sai com erro claro e não invalida o token de quem está atendendo, que antes deixava o `kh run` e o `/proxy` com "token recusado". No Linux, com o `keyhybrid.service` habilitado, o app e o `run_linux.sh` passam a pedir `systemctl --user start` em vez de subir um servidor próprio, que segurava a porta com o binário antigo durante atualizações. A ordem segura de atualização está em `docs/USAGE.md`.
- **Lints do Rust**: os avisos que o `cargo clippy -D warnings` acusava em `vault.rs`, `lib.rs` e `gateway.rs` foram resolvidos (`if` aninhados colapsados com *let chains* do Rust 2024 e uso de `is_multiple_of`); o comando `save_vault_key` mantém a assinatura plana exigida pelo `invoke` do Tauri.

### Segurança

- Os avisos do GitHub Advisory que atingiam a linha 2.x do `@sveltejs/kit` deixam de se aplicar, e o `cookie` transitivo passa a ser resolvido em `2.0.2`.
- **Chave da OpenAI não vai mais para a OpenRouter**: no cadastro manual, uma chave `sk-proj-`/`sk-svcacct-` colada ficava com o provedor padrão (`openrouter`) e o gateway a enviaria à OpenRouter. Agora o prefixo é reconhecido e a chave vira o serviço `openai`, com destino fixo `https://api.openai.com`.

### Planejado

- Instaladores `.deb` e `.msi` publicados como anexos de release (build verificado em ambiente desktop com WebKit/WebView2).
- Suporte a mais provedores no gateway, mantendo o contrato compatível com a API da OpenAI.

## [0.1.0] - 2026-11-23

Primeira versão pública do KeyHybrid: cofre *zero-knowledge* de chaves de IA com gateway local compatível com a API da OpenAI.

### Adicionado

- **Cofre desktop** em Tauri v2 + Svelte 5, com cofre criptografado, desbloqueio por PIN de 6 dígitos e sincronização local via SQLite.
- **Criptografia em repouso** com AES-256-GCM e chave mestra local (`~/.keyhybrid/.master.key`, permissão `0600`), protegida por Windows DPAPI / Linux Secret Service e limpeza de memória com `Zeroize`.
- **Gateway local** em Go (PocketBase) em `http://127.0.0.1:8090/v1`, compatível com clientes OpenAI, com endpoints de chat, áudio e vídeo.
- **Isolamento por alias**: clientes e agentes usam apenas tokens virtuais (`Bearer kh-openrouter`), e a chave real é injetada em RAM no momento da chamada HTTPS.
- **CLI `kh run`** para iniciar processos com as variáveis de ambiente resolvidas a partir do cofre, sem persistir segredos.
- **Proteções do gateway**: verificação de `Host` (anti DNS rebinding), `Origin`, `Sec-Fetch-Site` e `Cross-Origin-Resource-Policy`; bloqueio de requisições *state-changing* vindas de outros sites.
- **Fallback de guardrail** para a OpenRouter: em `HTTP 404` de modelo, o gateway reroteia para `openrouter/auto`.
- **Distribuição**: releases com binários do gateway (Linux, macOS, Windows), imagem de container pública no GHCR e documentação publicada no GitHub Pages.
- **Qualidade**: CI (Go + Svelte), CodeQL para `go` e `javascript-typescript`, Dependabot para Go, npm, Cargo, Actions e Docker, além de templates de issue/PR e política de segurança.

### Segurança

- `.gitignore` e `.dockerignore` endurecidos contra commit acidental de segredos, `pb_data/` e artefatos locais.

### Notas de migração

- Não há migração: esta é a primeira release pública. Use a seção *Abrir issue* para relatar incompatibilidades.

[0.1.0]: https://github.com/in100tiva/KeyHybrid/releases/tag/v0.1.0
[Não publicado]: https://github.com/in100tiva/KeyHybrid/compare/v0.1.0...HEAD
