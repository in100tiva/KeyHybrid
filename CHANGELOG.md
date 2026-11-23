# Changelog

Todas as mudanças relevantes deste projeto são documentadas neste arquivo.

O formato segue [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/) e o projeto adere ao [Versionamento Semântico](https://semver.org/lang/pt-BR/).

## [Não publicado]

### Adicionado

- **CI do desktop em Rust**: job `Desktop Rust (Tauri)` no `.github/workflows/ci.yml`, com `cargo fmt --all -- --check` e `cargo clippy --all-targets --locked -- -D warnings` no `ubuntu-latest`, bibliotecas do Tauri v2 (WebKitGTK 4.1) e cache do Cargo.
- **Revisão de dependências**: `.github/workflows/dependency-review.yml` comenta no PR e bloqueia merges que introduzam dependências com severidade alta.
- **Política de segurança**: seção sobre os dois advisories sem correção upstream (`glib` e `disintegration/imaging`) e por que eles não são alcançáveis no KeyHybrid.

### Alterado

- **SvelteKit 3**: interface migrada de `@sveltejs/kit` 2.x para 3.4 e de `@sveltejs/adapter-static` 3.x para 4.x. A configuração deixou de ficar em `svelte.config.js` (arquivo removido) e passou para o plugin `sveltekit()` em `apps/desktop/vite.config.js`; o `tsconfig.json` agora estende o `$app/tsconfig` gerado pelo próprio kit.
- `make check` e `make fmt` passam a cobrir o código Rust (`cargo fmt`), e o `docs/INSTALL.md` documenta o requisito de **Node.js 22.17+** e `pnpm` 10 no lugar de `npm`.

### Segurança

- Os avisos do GitHub Advisory que atingiam a linha 2.x do `@sveltejs/kit` deixam de se aplicar, e o `cookie` transitivo passa a ser resolvido em `2.0.2`.

### Planejado

- Instaladores `.deb` e `.msi` publicados como anexos de release (build verificado em ambiente desktop com WebKit/WebView2).
- Suporte a mais provedores no gateway, mantendo o contrato compatível com a API da OpenAI.
- Assistente de migração de segredos a partir de `.env` existentes.

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
