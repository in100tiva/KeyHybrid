# Política de Segurança

O KeyHybrid existe para **não** expor chaves de API. Por isso, falhas de segurança são tratadas com prioridade máxima e **nunca** em issue pública.

## Versões suportadas

| Versão | Suporte |
| --- | --- |
| `0.1.x` (última release) | ✅ Correções de segurança |
| Versões anteriores | ❌ Sem suporte — atualize |

## Como reportar uma vulnerabilidade

1. Abra um **Security Advisory privado**: <https://github.com/in100tiva/KeyHybrid/security/advisories/new>
2. Descreva o impacto, a versão afetada e um passo a passo mínimo para reprodução.
3. Não publique detalhes, provas de conceito ou capturas de tela em issues, discussões ou redes sociais até a correção ser liberada.

Se preferir canal alternativo, abra uma issue **vazia** apenas dizendo "canal privado de segurança solicitado" — o mantenedor entra em contato.

### O que ajuda no relatório

- Versão exata (`keyhybrid-server --version` ou tag da release).
- Sistema operacional e como o app foi instalado.
- Vetor de ataque (ex.: outro processo local, página web no navegador, arquivo malicioso no cofre).
- Prova de conceito mínima, **sem chaves reais**.

## Escopo

**Dentro do escopo**

- Vazamento da chave real do provedor para processos, logs, histórico de shell, arquivos ou rede.
- Quebra do isolamento por alias (`kh-<alias>`) ou do token virtual.
- Falhas na criptografia do cofre: AES-256-GCM, chave mestra, derivação/PIN, `Zeroize`.
- Bypass das proteções do gateway: verificação de `Origin`, `Host` (DNS rebinding), `Sec-Fetch-Site`, `Cross-Origin-Resource-Policy`.
- Escalonamento de privilégio entre app desktop, gateway e CLI `kh run`.
- Injeção de comando/SSRF no proxy multimodal ou nos endpoints internos.
- Exposição de segredos em artefatos de release, imagens de container ou no `pb_data`.

**Fora do escopo**

- Necessidade de acesso físico à máquina já desbloqueada ou de privilégios de administrador.
- Ataques que dependem de malware previamente executando com o mesmo usuário.
- Engenharia social, phishing e spam.
- Falhas em provedores upstream (OpenRouter, Google, Anthropic) — reporte diretamente a eles.
- Ausência de recursos que dependem de plano pago da plataforma.

## Dependências com advisories sem correção upstream

Dois alertas do Dependabot seguem abertos de forma **consciente**: em ambos não existe versão corrigida publicada e o código vulnerável não é alcançável pelo fluxo do KeyHybrid. Eles ficam registrados aqui em vez de serem dispensados em silêncio.

| Pacote | Severidade | Situação |
| --- | --- | --- |
| `glib` 0.18.5 (`apps/desktop/src-tauri/Cargo.lock`) — [GHSA-wrw7-89jp-8q8g](https://github.com/advisories/GHSA-wrw7-89jp-8q8g) | média | Afeta só os impls de `Iterator`/`DoubleEndedIterator` de `glib::VariantStrIter` (iteração de strings em `GVariant`); o KeyHybrid não usa `VariantStrIter`. A correção exige `glib >= 0.20`, que só entra quando a cadeia GTK usada pelo Tauri (`gtk` 0.18) subir de major — fora do nosso controle. |
| `github.com/disintegration/imaging` 1.6.2 (`apps/server/go.mod`) — [GHSA-q7pp-wcgr-pffx](https://github.com/advisories/GHSA-q7pp-wcgr-pffx) | baixa | Dependência **indireta** do PocketBase (`apis` → `tools/filesystem`), usada apenas para gerar thumbnails de arquivos de imagem. O app não define campos de imagem e não expõe upload de arquivos, então esse caminho não é alcançável. `v1.6.2` é a última versão publicada. |

Se você encontrar um caminho de exploração real para qualquer um dos dois, trate como vulnerabilidade e siga o [canal privado](#como-reportar-uma-vulnerabilidade).

O CI roda `govulncheck` (Go) e `cargo audit` (Rust) em todo pull request: vulnerabilidade nova reprova o PR; os avisos de manutenção abaixo aparecem só no log. O `glib` acima entra no `cargo audit` como aviso `unsound` (RUSTSEC-2024-0429), não como falha.

### Avisos de crate sem manutenção (informativos)

O RUSTSEC também sinaliza crates **sem mantenedor ativo** que entram de forma transitiva pelo stack GTK/WebKit do Tauri: `chrono`, `proc-macro-error`, `zbus_names`, `zvariant` e `zvariant_derive`. Não existe versão corrigida publicada — são avisos de manutenção, não falhas exploráveis descritas. Eles continuam visíveis no `cargo audit`/OSV e serão resolvidos quando o Tauri migrar para a próxima geração do `gtk-rs`; até lá o Dependabot mantém o restante do lockfile atualizado.

## Verificação de artefatos

Os binários em [Releases](https://github.com/in100tiva/KeyHybrid/releases) e as imagens no GHCR são produzidos pelos workflows deste repositório e cada release publica `SHA256SUMS.txt`.

A partir da próxima release, os binários e o checksum recebem uma *attestation* de procedência (build provenance) assinada pelo GitHub Actions. Para verificar:

```bash
gh attestation verify keyhybrid-server-linux-amd64 --repo in100tiva/KeyHybrid
gh attestation verify oci://ghcr.io/in100tiva/keyhybrid:latest --repo in100tiva/KeyHybrid
```

O resultado mostra commit, workflow e runner que geraram o artefato. Cada release também publica o bundle `provenance.intoto.jsonl`, que pode ser verificado offline ou sem consultar o histórico do repositório:

```bash
gh attestation verify keyhybrid-server-linux-amd64 \
  --bundle provenance.intoto.jsonl --repo in100tiva/KeyHybrid
```

Para conferir a integridade do download:

```bash
sha256sum -c SHA256SUMS.txt
```

## Modelo de ameaça do backup do cofre

O backup (`.khvault`, [#26](https://github.com/in100tiva/KeyHybrid/issues/26)) existe para levar o cofre para outra máquina sem copiar pastas internas. Formato e fluxo em [docs/USAGE.md](docs/USAGE.md#backup-e-restauração-do-cofre).

**Contra o que protege**

- **Arquivo roubado ou perdido** (pendrive, Drive, HD externo): sem a senha, o único caminho é força bruta offline contra o Argon2id (`m=64 MiB`, `t=3`, `p=4`, versão `0x13`), com senha de no mínimo 12 caracteres.
- **Arquivo adulterado ou truncado**: AES-256-GCM com os 38 bytes do cabeçalho (magic, versão, flags, salt, nonce) autenticados como AAD. Salt, nonce ou corpo alterados falham com a mesma mensagem genérica de senha errada; magic, versão ou flags inválidos são recusados como arquivo corrompido. Nos dois casos, antes de qualquer escrita no cofre.
- **Reuso de nonce**: salt e nonce novos, aleatórios, em cada arquivo, inclusive no backup de segurança feito antes de um *Substituir tudo*.
- **Restauração parcial**: todas as chaves são validadas pelas regras do cadastro antes da primeira escrita e a gravação roda numa única transação do banco; o *Substituir tudo* grava antes um backup de segurança e, se ele falhar, não apaga nada.

**Contra o que não protege**

- **Malware rodando com o seu usuário.** Ele já lê o `pb_data` e a chave mestra (selada pelo SO para esse mesmo usuário) e pode rodar `keyhybrid-server vault-dump` como o app faz. O backup não piora nem melhora isso.
- **Senha fraca.** Só o mínimo de 12 caracteres é obrigatório; o medidor de força é indicativo. Uma senha previsível cai na força bruta offline.
- **Senha perdida.** Não há recuperação, cópia da senha nem porta dos fundos.
- **Backup antigo.** Um arquivo continua válido para sempre com a senha dele. Se ele vazar junto com a senha, gire as chaves no provedor; apagar a chave do cofre não invalida o backup.

**Sem rede e sem rota HTTP**

- O backup não abre conexão: o app cifra e decifra o arquivo em Rust e conversa com o `keyhybrid-server` do mesmo usuário só por pipe (`vault-dump` / `vault-apply`).
- O gateway não tem rota de backup, export ou import. O teste `TestVaultSemRotaHTTP` (`apps/server/vault_cli_test.go`) varre os registros de rota do servidor e falha se algum caminho contiver `backup`, `export`, `import`, `dump`, `restore` ou `vault-apply`. O `TestVaultSemRede` falha se o dump ou a aplicação usarem o `http.DefaultTransport`.

**Onde o texto em claro existe**

- Só em memória: no processo do app, no `keyhybrid-server` e no pipe entre os dois, que são processos do mesmo usuário. Nenhum arquivo temporário em claro é gravado. O `.khvault`, o backup de segurança e o `backup-status.json` (data, caminho e contagem, sem segredos) são os únicos arquivos escritos; a pasta `~/.keyhybrid/backups` nasce com `0700` no Linux/macOS.
- **Rust:** senha, chave derivada, texto decifrado, envelope serializado e a saída/entrada do pipe ficam em `Zeroizing` e são zerados ao sair de escopo. O documento do cofre já lido (`PlainVault`) zera todas as suas strings no `Drop`, e o JSON é lido e escrito em buffers com capacidade fixa de 16 MiB, para não deixar cópias de realocação para trás. É uma tentativa de higiene de memória: o alocador e o parser ainda podem ter cópias curtas fora desse controle.
- **Go:** os buffers de entrada, saída e de cada segredo decifrado são zerados com `wipeBytes`, mas o segredo também vira `string` no documento JSON, e `string` em Go é imutável: essa cópia só some quando o coletor de lixo libera a memória. O processo é curto (termina logo depois do dump ou da aplicação).
- **Interface:** a senha digitada vive no estado da tela até concluir, cancelar ou falhar, e então é apagada; strings de JavaScript também não são zeráveis.

**Diálogos e superfície do webview**

- Os diálogos de salvar e abrir arquivo são abertos pelo Rust (`tauri-plugin-dialog`). O `capabilities/default.json` não concede nenhuma permissão `dialog:` ao webview; ele só recebe o caminho escolhido.
- Todos os comandos de backup conferem a origem local da janela e, com PIN configurado, exigem o cofre desbloqueado.

**O comando `vault-dump` existe**

- `keyhybrid-server vault-dump` despeja o cofre em claro no stdout e recusa apenas o stdout de terminal, para não pintar segredo na tela por engano. Qualquer processo do seu usuário pode rodá-lo com o stdout num pipe ou num arquivo. Isso não dá poder novo: o mesmo processo já poderia ler o `pb_data` e a chave mestra. Não redirecione o dump para arquivo fora do app.

## Nosso compromisso

| Etapa | Prazo alvo |
| --- | --- |
| Confirmação de recebimento | 72 horas |
| Avaliação inicial e severidade | 7 dias |
| Correção de falhas críticas/altas | 30 dias |
| Divulgação coordenada | Após correção e release |

Você será creditado no advisory e no `CHANGELOG.md`, se desejar.

## Práticas recomendadas para usuários

- Mantenha o gateway escutando apenas em loopback (`127.0.0.1:8090`).
- Ao usar Docker, publique a porta como `-p 127.0.0.1:8090:8090`.
- Nunca faça commit de `~/.keyhybrid`, `pb_data/` ou de arquivos `.env`.
- Rotacione a chave real no provedor se suspeitar de exposição — o cofre não impede o uso indevido de uma chave já vazada.
