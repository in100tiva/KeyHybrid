"""Testes canônicos da issue #16 (instaladores .deb/.msi na release). Não editar: o gate confere o hash.

Uso (na raiz do repositório): python3 -I .github/scripts/test_release_desktop.py
Só usa a stdlib + PyYAML.
"""

import json
import re
import sys
from pathlib import Path

import yaml

ROOT = Path.cwd()
FALHAS = []


def check(cond, msg):
    print(("  ok    " if cond else "  FALHA ") + msg)
    if not cond:
        FALHAS.append(msg)


def steps_text(job):
    """Concatena run/uses/with de todos os steps (para buscas de texto)."""
    out = []
    for s in job.get("steps", []):
        out.append(json.dumps(s, ensure_ascii=False))
    return "\n".join(out)


def runs(job):
    return [s.get("run", "") for s in job.get("steps", []) if "run" in s]


# ---------------------------------------------------------------- release.yml
wf_path = ROOT / ".github/workflows/release.yml"
wf_raw = wf_path.read_text(encoding="utf-8")
wf = yaml.safe_load(wf_raw)
jobs = wf.get("jobs", {})
# PyYAML lê a chave `on` como True.
gatilhos = wf.get("on", wf.get(True, {}))

check("push" in gatilhos and gatilhos["push"].get("tags") == ["v*"], "gatilho push tags v* mantido")
check("workflow_dispatch" in gatilhos, "workflow_dispatch mantido")
check(wf.get("permissions") == {"contents": "read"}, "permissions do workflow continua contents: read")

# Toda action fixada por SHA de 40 hex (convenção do PR #27).
uses = re.findall(r"^\s*-?\s*uses:\s*(\S+)", wf_raw, flags=re.M)
nao_fixadas = [u for u in uses if not re.fullmatch(r"[\w.-]+/[\w./-]+@[0-9a-f]{40}", u)]
check(uses and not nao_fixadas, f"todas as actions fixadas por SHA (fora: {nao_fixadas})")
check("tauri-apps/tauri-action" not in wf_raw, "sem tauri-action (build pelo CLI do próprio projeto)")

# Job binaries intocado no essencial.
check("binaries" in jobs and len(jobs["binaries"]["strategy"]["matrix"]["include"]) == 5, "job binaries com 5 alvos")

# Job desktop.
desk = jobs.get("desktop")
check(desk is not None, "job desktop existe")
if desk:
    check(desk.get("permissions") == {"contents": "read"}, "desktop: permissions contents: read")
    check(desk.get("runs-on") == "${{ matrix.os }}", "desktop: runs-on vem da matrix")
    inc = desk.get("strategy", {}).get("matrix", {}).get("include", [])
    oss = sorted(i.get("os") for i in inc)
    check(oss == ["ubuntu-22.04", "windows-latest"], f"desktop: matrix ubuntu-22.04 + windows-latest (veio {oss})")
    check(desk.get("strategy", {}).get("fail-fast") is False, "desktop: fail-fast false")
    check(isinstance(desk.get("timeout-minutes"), int) and desk["timeout-minutes"] <= 90, "desktop: timeout-minutes <= 90")
    txt = steps_text(desk)
    rr = "\n".join(runs(desk))
    for action in ("actions/checkout@", "actions/setup-go@", "pnpm/action-setup@", "actions/setup-node@",
                   "dtolnay/rust-toolchain@", "actions/upload-artifact@"):
        check(action in txt, f"desktop: usa {action}")
    check("go-version-file" in txt and "apps/server/go.mod" in txt, "desktop: Go pelo apps/server/go.mod")
    check("libwebkit2gtk-4.1-dev" in rr and "runner.os == 'Linux'" in txt, "desktop: deps WebKitGTK só no Linux")
    check("pnpm install --frozen-lockfile" in rr, "desktop: pnpm install --frozen-lockfile")
    check(".github/scripts/desktop-version.sh" in rr and "github.ref" in txt, "desktop: versão via desktop-version.sh com github.ref")
    builds = [r for r in runs(desk) if "tauri build" in r]
    check(any(re.search(r"--bundles deb --config src-tauri/tauri\.deb\.conf\.json --config \"\$RUNNER_TEMP/tauri\.version\.json\"", r) for r in builds),
          "desktop: tauri build --bundles deb com tauri.deb.conf.json + versão")
    check(any(re.search(r"--bundles (msi,nsis|nsis,msi)\b", r) for r in builds), "desktop: tauri build --bundles msi,nsis")
    check(all("--config" in r for r in builds) and builds, "desktop: build recebe --config com a versão")
    # Verificação do .deb dentro do CI: servidor empacotado e executável.
    check("dpkg-deb -c" in rr and "server/keyhybrid-server" in rr, "desktop: CI confere server/keyhybrid-server dentro do .deb")
    check(re.search(r"-rwx", rr) is not None, "desktop: CI confere bit de execução do servidor no .deb")
    check('test "$(dpkg-deb -f "$deb" Package)" = key-hybrid' in rr, "desktop: CI confere o nome do pacote (key-hybrid)")
    check("|| true" not in rr and "continue-on-error" not in json.dumps(desk), "desktop: nenhuma checagem engolida (|| true / continue-on-error)")
    check(all("if" not in s or s["if"] == "runner.os == 'Linux'" for s in desk.get("steps", [])),
          "desktop: único if permitido nos steps é runner.os == 'Linux'")
    junta = [r for r in runs(desk) if "instaladores" in r and "find src-tauri/target/release/bundle" in r]
    check(len(junta) == 1 and all(n in junta[0] for n in ("KeyHybrid_*.deb", "KeyHybrid_*.msi", "KeyHybrid_*-setup.exe")),
          "desktop: step junta .deb/.msi/-setup.exe numa pasta plana $RUNNER_TEMP/instaladores")
    ups = [s for s in desk.get("steps", []) if str(s.get("uses", "")).startswith("actions/upload-artifact@")]
    check(len(ups) == 1 and ups[0].get("with", {}).get("if-no-files-found") == "error", "desktop: 1 upload com if-no-files-found: error")
    # Um único padrão, plano: com vários, o artifact preserva deb/ msi/ nsis/ e o sha256sum da raiz falha.
    check(ups and ups[0].get("with", {}).get("path") == "${{ runner.temp }}/instaladores/*",
          "desktop: upload de ${{ runner.temp }}/instaladores/* (artifact plano)")
    nomes = [s.get("name", "") for s in desk.get("steps", [])]
    i_junta = next((i for i, s in enumerate(desk.get("steps", [])) if "instaladores" in s.get("run", "")), -1)
    i_up = next((i for i, s in enumerate(desk.get("steps", [])) if str(s.get("uses", "")).startswith("actions/upload-artifact@")), -1)
    check(0 <= i_junta < i_up, "desktop: juntar vem antes do upload")

# Job release.
rel = jobs.get("release", {})
needs = rel.get("needs")
needs = [needs] if isinstance(needs, str) else (needs or [])
check(sorted(needs) == ["binaries", "desktop"], f"release: needs binaries + desktop (veio {needs})")
check(re.search(r"startsWith\(\s*github\.ref\s*,\s*'refs/tags/v'\s*\)", str(rel.get("if", ""))) is not None,
      "release: só publica em tag v* (workflow_dispatch vira ensaio)")
check(rel.get("permissions") == {"contents": "write", "id-token": "write", "attestations": "write"}, "release: permissions inalteradas")
rr = "\n".join(runs(rel))
sums = [r for r in runs(rel) if "sha256sum" in r]
check(len(sums) == 1 and re.search(r"^\s*sha256sum keyhybrid-server-\* KeyHybrid_\* > SHA256SUMS\.txt\s*$", sums[0], flags=re.M) is not None,
      "release: SHA256SUMS.txt cobre servidor e instaladores")
txt = steps_text(rel)
check("subject-path" in txt and "dist/*" in txt, "release: atestação continua em dist/*")
gh = [s for s in rel.get("steps", []) if str(s.get("uses", "")).startswith("softprops/action-gh-release@")]
check(gh and gh[0].get("with", {}).get("fail_on_unmatched_files") is True and gh[0]["with"].get("files") == "dist/*",
      "release: action-gh-release publica dist/* com fail_on_unmatched_files")

# ------------------------------------------------------------ configs Tauri
tauri_dir = ROOT / "apps/desktop/src-tauri"
base = json.loads((tauri_dir / "tauri.conf.json").read_text(encoding="utf-8"))
check(base.get("productName") == "KeyHybrid", "tauri.conf.json: productName KeyHybrid (nome dos assets)")
check(base.get("bundle", {}).get("active") is True, "tauri.conf.json: bundle ativo")

# tauri.linux.conf.json seria mesclado em todo build Linux (inclusive o clippy do ci.yml), e o
# build.rs exige que os resources existam: o servidor só existe no job de release.
check(not (tauri_dir / "tauri.linux.conf.json").exists(), "tauri.linux.conf.json NÃO existe (quebraria o clippy do CI)")
lin_path = tauri_dir / "tauri.deb.conf.json"
check(lin_path.exists(), "tauri.deb.conf.json existe")
if lin_path.exists():
    lin = json.loads(lin_path.read_text(encoding="utf-8"))
    res = lin.get("bundle", {}).get("resources", {})
    check(res == {"../../server/keyhybrid-server": "server/keyhybrid-server"},
          f"linux: resources leva ../../server/keyhybrid-server -> server/keyhybrid-server (veio {res})")
    bbc = lin.get("build", {}).get("beforeBuildCommand", "")
    check(bbc == "pnpm run build:server:unix && pnpm run build", f"linux: beforeBuildCommand (veio {bbc!r})")

win = json.loads((tauri_dir / "tauri.windows.conf.json").read_text(encoding="utf-8"))
check(win.get("bundle", {}).get("resources") == {"../../server/keyhybrid-server.exe": "server/keyhybrid-server.exe"},
      "windows: resources do servidor inalterado")
check(win.get("bundle", {}).get("windows", {}).get("nsis", {}).get("installerHooks") == "./windows/hooks.nsh",
      "windows: hook NSIS mantido")

pkg = json.loads((ROOT / "apps/desktop/package.json").read_text(encoding="utf-8"))
scr = pkg.get("scripts", {})
check("build:server" in scr and "keyhybrid-server.exe" in scr["build:server"], "package.json: build:server (Windows) mantido")
unix = scr.get("build:server:unix", "")
check("cd ../server" in unix and "go build" in unix and re.search(r"-o keyhybrid-server(\s|$)", unix) is not None
      and ".exe" not in unix, f"package.json: build:server:unix gera keyhybrid-server sem .exe (veio {unix!r})")

# ------------------------------------------------------------ Rust: fiação
lib_raw = (tauri_dir / "src/lib.rs").read_text(encoding="utf-8")
# Comentários não contam como código.
lib = re.sub(r"//[^\n]*", "", lib_raw)
check(re.search(r"^\s*mod server_bin;", lib, flags=re.M) is not None, "lib.rs: declara mod server_bin")
corpo = re.search(r"pub fn ensure_gateway_running\(\)\s*\{(.*?)\n\}", lib, flags=re.S)
check(corpo is not None, "lib.rs: ensure_gateway_running() sem parâmetros (gateway.rs chama assim)")
if corpo:
    c = corpo.group(1)
    check("server_bin::candidates(" in c and "server_bin::first_existing(" in c,
          "lib.rs: ensure_gateway_running usa server_bin::candidates + first_existing")
    check("let candidates = [" not in c and ".keyhybrid" not in c, "lib.rs: lista antiga de candidatos removida")
    # Sem WebKit local nada compila o lib.rs: pegar aqui o que só o clippy do CI veria.
    check(re.search(r"\bbreak\b", c) is None and re.search(r"\bfor\b", c) is None, "lib.rs: sem for/break no corpo (não há mais loop)")
    check("let bin_name = server_bin::bin_name();" in c, "lib.rs: let bin_name = server_bin::bin_name();")
    for trecho in ('.arg("serve")', '.arg("--http=127.0.0.1:8090")', ".current_dir(parent)", "CREATE_NO_WINDOW", "cmd.spawn()",
                   "TcpStream::connect_timeout(&addr, Duration::from_millis(250))"):
        check(trecho in c, f"lib.rs: corpo mantém {trecho}")
    check(re.search(r"server_bin::candidates\((?:[^;]*?)RESOURCE_DIR\.get\(\)\.map\(PathBuf::as_path\)\s*,?\s*\)", c, flags=re.S) is not None,
          "lib.rs: candidates recebe RESOURCE_DIR.get().map(PathBuf::as_path)")
check(re.search(r"static RESOURCE_DIR:\s*(std::sync::)?OnceLock<PathBuf>", lib) is not None, "lib.rs: static RESOURCE_DIR: OnceLock<PathBuf>")
setup = re.search(r"\.setup\(\|app\|\s*\{(.*?)\n        \}\)", lib, flags=re.S)
check(setup is not None and re.search(r"if let Ok\((\w+)\) = app\.path\(\)\.resource_dir\(\)\s*\{\s*let _ = RESOURCE_DIR\.set\(\1\);\s*\}",
                                       setup.group(1)) is not None,
      "lib.rs: setup grava app.path().resource_dir() em RESOURCE_DIR")
if setup:
    s = setup.group(1)
    check(s.find("RESOURCE_DIR.set(") < s.find("ensure_gateway_running"), "lib.rs: RESOURCE_DIR é gravado antes de subir o gateway")
sb = (tauri_dir / "src/server_bin.rs").read_text(encoding="utf-8") if (tauri_dir / "src/server_bin.rs").exists() else ""
check(re.search(r'#\[cfg\(test\)\]\s*#\[path = "server_bin_tests.rs"\]\s*mod tests;', sb) is not None,
      "server_bin.rs: inclui os testes canônicos")
check("tauri" not in sb, "server_bin.rs: só std (roda sem Tauri)")

# ------------------------------------------------------------ docs
inst = (ROOT / "docs/INSTALL.md").read_text(encoding="utf-8")
for termo in ("sudo apt remove key-hybrid", "Get-FileHash", "KeyHybrid_", "_amd64.deb", "_x64_en-US.msi", "_x64-setup.exe", "sudo apt install ./",
              "SmartScreen", "Executar assim mesmo", "sha256sum -c", "taskkill /F /IM keyhybrid-server.exe",
              "gh attestation verify"):
    check(termo in inst, f"INSTALL.md menciona {termo!r}")
i_smart, i_linux = inst.find("SmartScreen"), inst.find("## 🐧 Linux")
check(0 <= i_smart < i_linux, "INSTALL.md: seção do instalador vem antes do build do zero (## 🐧 Linux)")
readme = (ROOT / "README.md").read_text(encoding="utf-8")
linha = next((l for l in readme.splitlines() if l.startswith("| **App desktop**")), "")
check(linha and "por enquanto" not in linha and "Releases" in linha and "INSTALL.md" in linha,
      f"README: linha App desktop aponta para Releases e INSTALL.md (veio {linha!r})")

print()
if FALHAS:
    print(f"{len(FALHAS)} falha(s)")
    sys.exit(1)
print("tudo ok")
