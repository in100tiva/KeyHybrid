#!/usr/bin/env bash
set -euo pipefail

ref="${1:-}"
conf="${2:-}"

if [[ -z "$ref" ]]; then
  echo "erro: ref vazio" >&2
  exit 1
fi

case "$ref" in
  refs/tags/v*)
    version="${ref#refs/tags/v}"
    ;;
  refs/tags/*)
    echo "erro: tag sem prefixo v: $ref" >&2
    exit 1
    ;;
  *)
    if [[ -z "$conf" || ! -f "$conf" ]]; then
      echo "erro: arquivo de configuração ausente: $conf" >&2
      exit 1
    fi
    version="$(node -e '
      const fs = require("fs");
      const data = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
      if (typeof data.version !== "string") process.exit(1);
      process.stdout.write(data.version);
    ' "$conf")"
    ;;
esac

num='(0|[1-9][0-9]{0,4})'
if [[ ! "$version" =~ ^${num}\.${num}\.${num}$ ]]; then
  echo "erro: versão inválida para MSI (esperado X.Y.Z numérico): $version" >&2
  exit 1
fi

IFS='.' read -r major minor patch <<< "$version"
if (( major > 255 || minor > 255 || patch > 65535 )); then
  echo "erro: versão fora dos limites do MSI (255.255.65535): $version" >&2
  exit 1
fi

printf '%s\n' "$version"
