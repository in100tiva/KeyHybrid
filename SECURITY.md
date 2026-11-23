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
