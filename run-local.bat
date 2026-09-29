@echo off
setlocal
cd /d "%~dp0"
where pwsh >nul 2>nul
if %errorlevel%==0 (
    pwsh -ExecutionPolicy Bypass -NoProfile -File "%~dp0scripts\run-local.ps1" %*
) else (
    powershell -ExecutionPolicy Bypass -NoProfile -File "%~dp0scripts\run-local.ps1" %*
)
endlocal
