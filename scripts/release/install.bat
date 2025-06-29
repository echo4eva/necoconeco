@echo off
echo Installing Necoconeco startup...

REM Get current directory
set "CURRENT_DIR=%~dp0"
set "BAT_FILE=%CURRENT_DIR%run.bat"

REM Try to create scheduled task first
echo Creating scheduled task...
schtasks /create /tn "NecoconecoStartup" /tr "\"%BAT_FILE%\"" /sc onstart /ru "%USERNAME%" /f >nul 2>&1

if %ERRORLEVEL% EQU 0 (
    echo ✓ Successfully installed via Task Scheduler!
    goto :success
)

REM Fallback to startup folder - create launcher script
echo Task Scheduler failed, trying startup folder...
set "STARTUP_DIR=%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup"
set "LAUNCHER_FILE=%STARTUP_DIR%\necoconeco_launcher.bat"

REM Create launcher script that points to original directory
echo @echo off > "%LAUNCHER_FILE%"
echo cd /d "%CURRENT_DIR%" >> "%LAUNCHER_FILE%"
echo call "%BAT_FILE%" >> "%LAUNCHER_FILE%"

if %ERRORLEVEL% EQU 0 (
    echo ✓ Successfully installed via Startup folder!
    goto :success
) else (
    echo ✗ Installation failed. Please run as Administrator.
    goto :end
)

:success
echo.
echo Installation complete! The program will start automatically on next boot.

:end
echo Press any key to exit...
pause >nul