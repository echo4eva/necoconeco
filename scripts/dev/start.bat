:: --- scripts/start.bat ---
@echo off
title necoconeco

:: The root directory is the parent of this script's directory (%~dp0)
set "PROJECT_ROOT=%~dp0.."

echo Loading configuration from %PROJECT_ROOT%\.env
:: Load environment variables from the project root
if exist "%PROJECT_ROOT%\.env" (
    for /f "usebackq delims=" %%a in ("%PROJECT_ROOT%\.env") do (
        set "%%a"
    )
)

echo Starting initial sync...
:: Run clientsync.exe from the project root and wait for it
call "%PROJECT_ROOT%\clientsync.exe"

echo Initial sync complete.
echo.
echo Starting real-time file watcher...
:: Run client.exe from the project root
call "%PROJECT_ROOT%\client.exe"