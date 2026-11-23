## O que muda

<!-- Descreva a mudança em 1-3 frases. Link para a issue: "Closes #123" -->

## Tipo de mudança

- [ ] Correção de bug
- [ ] Nova funcionalidade
- [ ] Melhoria de segurança
- [ ] Documentação
- [ ] Refatoração / performance
- [ ] CI, build ou empacotamento

## Como foi testado

<!-- Comandos executados e o que foi observado. -->

```bash
cd apps/server && go vet ./... && go test ./...
cd apps/desktop && pnpm run check && pnpm run build
```

## Checklist

- [ ] O código segue o estilo do projeto (gofmt limpo, Svelte 5 com runes)
- [ ] Adicionei ou atualizei testes quando fazia sentido
- [ ] Atualizei a documentação afetada (`README`, `docs/` ou `CHANGELOG.md`)
- [ ] **Não** incluí chaves reais, tokens `kh-*`, PINs, `pb_data/` ou `~/.keyhybrid` no diff
- [ ] Mudanças de segurança foram descritas o suficiente para revisão sem expor detalhes exploráveis

## Notas para revisão

<!-- Decisões de design, trade-offs, migrações necessárias. -->
