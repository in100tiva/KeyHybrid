@echo off
echo ========================================================
echo   Iniciando KeyHybrid Desktop (Tauri v2 + Svelte 5 + Rust)
echo ========================================================
echo.
cd /d "%~dp0apps\desktop"
pnpm tauri dev
pause
