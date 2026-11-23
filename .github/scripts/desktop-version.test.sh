#!/usr/bin/env bash
# Testes canônicos de .github/scripts/desktop-version.sh (issue #16). Não editar: o gate confere o hash.
# Uso: bash .github/scripts/desktop-version.test.sh   (na raiz do repositório)
set -uo pipefail
SCRIPT=".github/scripts/desktop-version.sh"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
CONF="$TMP/tauri.conf.json"
printf '{\n  "productName": "KeyHybrid",\n  "version": "9.8.7"\n}\n' > "$CONF"
FALHAS=0

ok() { # ok <ref> <esperado>
  local out rc
  out=$(bash "$SCRIPT" "$1" "$CONF" 2>/dev/null); rc=$?
  if [[ $rc -eq 0 && "$out" == "$2" ]]; then echo "  ok   $1 -> $out"
  else echo "  FALHA $1: esperado '$2' rc=0, veio '$out' rc=$rc"; FALHAS=1; fi
}
recusa() { # recusa <ref>
  local out rc
  out=$(bash "$SCRIPT" "$1" "$CONF" 2>/dev/null); rc=$?
  if [[ $rc -ne 0 && -z "$out" ]]; then echo "  ok   $1 recusado"
  else echo "  FALHA $1: devia sair != 0 sem stdout, veio '$out' rc=$rc"; FALHAS=1; fi
}

[[ -f "$SCRIPT" ]] || { echo "FALHA: $SCRIPT não existe"; exit 1; }

# Tag v* -> versão da tag, sem o v.
ok "refs/tags/v0.2.0" "0.2.0"
ok "refs/tags/v1.10.3" "1.10.3"
ok "refs/tags/v255.255.65535" "255.255.65535"
ok "refs/tags/v0.0.0" "0.0.0"
# Fora de tag (workflow_dispatch numa branch) -> versão do tauri.conf.json.
ok "refs/heads/main" "9.8.7"
ok "refs/heads/feat/instaladores-desktop" "9.8.7"
# O MSI só aceita X.Y.Z numérico (X,Y <= 255, Z <= 65535).
recusa "refs/tags/v0.2.0-beta.1"
recusa "refs/tags/v0.2"
recusa "refs/tags/v0.2.0.1"
recusa "refs/tags/v256.0.0"
recusa "refs/tags/v0.256.0"
recusa "refs/tags/v0.0.65536"
recusa "refs/tags/v01.2.3"
recusa "refs/tags/v1.02.3"
recusa "refs/tags/0.2.0"
recusa "refs/tags/release"
recusa "refs/tags/v"
recusa ""
# Sem ref nenhum e conf inválido também recusa.
out=$(bash "$SCRIPT" "refs/heads/main" "$TMP/nao-existe.json" 2>/dev/null); rc=$?
if [[ $rc -ne 0 && -z "$out" ]]; then echo "  ok   conf ausente recusado"; else echo "  FALHA conf ausente: '$out' rc=$rc"; FALHAS=1; fi
printf '{ "version": "1.2.3-rc.1" }\n' > "$TMP/pre.json"
out=$(bash "$SCRIPT" "refs/heads/main" "$TMP/pre.json" 2>/dev/null); rc=$?
if [[ $rc -ne 0 && -z "$out" ]]; then echo "  ok   conf com pre-release recusado"; else echo "  FALHA conf pre-release: '$out' rc=$rc"; FALHAS=1; fi

exit $FALHAS
