# Contribuindo com o KeyHybrid

Obrigado pelo interesse! Este projeto tem uma regra que está acima de todas as outras: **nenhuma chave real pode sair da máquina do usuário**. Contribuições que enfraqueçam isso são recusadas.

## Antes de abrir uma issue

- Bug? Use o template **🐞 Bug** e remova segredos dos logs.
- Ideia ou dúvida? Prefira as [Discussões](https://github.com/in100tiva/KeyHybrid/discussions).
- Vulnerabilidade? **Siga o [SECURITY.md](SECURITY.md)** — nunca em canal público.

## Ambiente de desenvolvimento

Pré-requisitos completos em [docs/INSTALL.md](docs/INSTALL.md). Resumo:

| Ferramenta | Versão |
| --- | --- |
| Go | 1.27+ |
| Node.js | 22.17+ |
| pnpm | 10+ |
| Rust + Tauri CLI | stable (só para o app desktop) |

```bash
git clone https://github.com/in100tiva/KeyHybrid.git
cd KeyHybrid

# gateway
cd apps/server && go build ./... && go test ./...

# interface
cd apps/desktop && pnpm install --frozen-lockfile && pnpm run check && pnpm run build
```

Comandos úteis:

```bash
make help        # lista os alvos
make check       # go vet + svelte-check + cargo fmt --check
make test        # testes Go com -race e da interface
make build       # gateway + interface
make docker      # imagem local do gateway
```

## Padrões de código

- **Go**: `gofmt` obrigatório, `go vet` limpo, zero dependências novas sem justificativa.
- **Svelte**: Svelte 5 com *runes* (`$state`, `$derived`, `$effect`), TypeScript sem `any` novo.
- **SvelteKit**: a configuração fica no plugin `sveltekit()` de `apps/desktop/vite.config.js` — o kit 3 não usa mais `svelte.config.js`, e o `tsconfig.json` estende `$app/tsconfig`.
- **Rust**: `cargo fmt` e `cargo clippy` sem warnings novos.
- **Texto e UI**: português do Brasil, sem expor detalhes internos de segurança na interface.
- **Commits**: [Conventional Commits](https://www.conventionalcommits.org/pt-br/) — `feat:`, `fix:`, `docs:`, `test:`, `chore:`, `refactor:`, `ci:`.
- **Segurança**: qualquer PR que toque criptografia, PIN, chave mestra, CORS/Origin ou o proxy deve explicar o modelo de ameaça no corpo do PR.

## Testes

- `apps/server`: testes Go (`go test -race ./...`). Para fluxos que tocam criptografia, prefira testes de unidade com chaves efêmeras.
- `apps/desktop`: `pnpm run test` (`node --test`, sem dependência). Regra pura da interface, como a importação de `.env`, fica em `src/lib/` para ser testada assim, com valores falsos.
- Nunca versione `pb_data/`, `*.db`, `~/.keyhybrid`, `.master.key`, `.cli.token` ou qualquer token real, nem em fixtures.

## Processo de PR

1. Abra uma issue antes de mudanças grandes, para alinhar a abordagem.
2. Faça *fork*, crie a branch a partir de `main` (`feat/minha-feature`).
3. Mantenha o PR pequeno e com um único assunto.
4. Preencha o template: o que muda, como foi testado, checklist.
5. A CI precisa ficar verde (Go, svelte-check, build, Rust/clippy, revisão de dependências e CodeQL).
6. Um mantenedor revisa; pelo menos uma aprovação é necessária para o merge.

## O que ganha merge rápido

- Correções com teste de regressão.
- Melhorias de compatibilidade com SDKs OpenAI (sempre sem quebrar o fluxo atual).
- Documentação clara, em português, com exemplos executáveis.
- Redução de superfície de ataque: menos permissões, menos dados em disco, menos logs.

## Código de conduta

Ao participar, você concorda com o [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
