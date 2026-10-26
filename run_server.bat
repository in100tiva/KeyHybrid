@echo off
echo ========================================================
echo   Iniciando KeyHybrid AI Gateway & PocketBase em Go
echo ========================================================
echo.
cd /d "%~dp0apps\server"
keyhybrid-server.exe serve
pause
