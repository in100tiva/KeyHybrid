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
