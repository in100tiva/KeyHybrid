# Interface desktop (Tauri v2 + SvelteKit 3)

Camada de interface do KeyHybrid: cofre de chaves, desbloqueio por PIN e configuração do
gateway local. O gateway em Go fica em [`../server`](../server) — o app precisa dele rodando
para resolver as chaves.

## Stack

| Camada | Tecnologia |
| --- | --- |
| Runtime desktop | Tauri v2 (WebView do sistema; Rust em `src-tauri/`) |
| Framework | SvelteKit 3 + Svelte 5 (runes) |
| Build | Vite 8 com `@sveltejs/adapter-static` em modo SPA (`fallback: index.html`) |
| Verificação | `svelte-check` + TypeScript em modo `strict` |

Desde o SvelteKit 3 a configuração do framework vive no plugin `sveltekit()` de
[`vite.config.js`](vite.config.js) — não existe mais `svelte.config.js`. O
[`tsconfig.json`](tsconfig.json) estende o `$app/tsconfig` gerado pelo próprio kit durante o
`svelte-kit sync`.

## Comandos

```bash
pnpm install
pnpm run check    # svelte-check (TypeScript strict)
pnpm run build    # gera o site estático em ./build
pnpm tauri dev    # app desktop com HMR em http://localhost:1420
pnpm tauri build  # instaladores (requer toolchain Rust)
```

Para rodar o app completo:

```bash
# terminal 1 — gateway
cd ../server && go run .

# terminal 2 — interface
pnpm tauri dev
```

## IDE recomendado

[VS Code](https://code.visualstudio.com/) com as extensões
[Svelte](https://marketplace.visualstudio.com/items?itemName=svelte.svelte-vscode),
[Tauri](https://marketplace.visualstudio.com/items?itemName=tauri-apps.tauri-vscode) e
[rust-analyzer](https://marketplace.visualstudio.com/items?itemName=rust-lang.rust-analyzer).

## Documentação

- Instalação e pré-requisitos: [`docs/INSTALL.md`](../../docs/INSTALL.md)
- Uso do cofre e do gateway: [`docs/USAGE.md`](../../docs/USAGE.md)
- Especificação do cofre: [`docs/SPEC-cofre-universal.md`](../../docs/SPEC-cofre-universal.md)
