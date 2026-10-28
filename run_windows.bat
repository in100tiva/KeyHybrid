@echo off
setlocal
cd /d "%~dp0"

echo [KeyHybrid] Verificando servidor Gateway Go (127.0.0.1:8090)...
if not exist "apps\server\keyhybrid-server.exe" (
    echo [KeyHybrid] Compilando keyhybrid-server.exe...
    pushd apps\server
    go build -trimpath -buildvcs=false -ldflags="-s -w" -o keyhybrid-server.exe .
    popd
)

start "" /B "apps\server\keyhybrid-server.exe" serve --http=127.0.0.1:8090

echo [KeyHybrid] Iniciando aplicativo Desktop...
cd apps\desktop
npm run tauri dev
