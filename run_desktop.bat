@echo off
echo ========================================================
echo   Iniciando KeyHybrid Desktop (Tauri v2 + Svelte 5 + Rust)
echo ========================================================
echo.
cd /d "%~dp0"
rem O Tauri empacota o servidor como recurso; sem o binario o build falha.
if not exist "apps\server\keyhybrid-server.exe" (
    echo [KeyHybrid] Compilando keyhybrid-server.exe...
    pushd apps\server
    go build -trimpath -buildvcs=false -ldflags="-s -w" -o keyhybrid-server.exe .
    popd
)
cd apps\desktop
pnpm tauri dev
pause
