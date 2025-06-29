@echo off
echo Uninstalling Necoconeco from Windows startup...
echo.

set "REMOVED_SOMETHING=0"

REM Remove from Task Scheduler
echo Checking Task Scheduler...
schtasks /query /tn "NecoconecoStartup" >nul 2>&1
if %ERRORLEVEL% EQU 0 (
    echo Removing scheduled task...
    schtasks /delete /tn "NecoconecoStartup" /f >nul 2>&1
    if %ERRORLEVEL% EQU 0 (
        echo ✓ Removed scheduled task
        set "REMOVED_SOMETHING=1"
    ) else (
        echo ✗ Failed to remove scheduled task
    )
) else (
    echo - No scheduled task found
)

REM Remove from Registry
echo Checking Registry startup...
reg query "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v "NecoconecoStartup" >nul 2>&1
if %ERRORLEVEL% EQU 0 (
    echo Removing registry entry...
    reg delete "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v "NecoconecoStartup" /f >nul 2>&1
    if %ERRORLEVEL% EQU 0 (
        echo ✓ Removed registry entry
        set "REMOVED_SOMETHING=1"
    ) else (
        echo ✗ Failed to remove registry entry
    )
) else (
    echo - No registry entry found
)

REM Remove from Startup folder
set "STARTUP_DIR=%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup"
echo Checking Startup folder...

REM Check for original run.bat
if exist "%STARTUP_DIR%\run.bat" (
    echo Removing run.bat from startup folder...
    del "%STARTUP_DIR%\run.bat" >nul 2>&1
    if %ERRORLEVEL% EQU 0 (
        echo ✓ Removed run.bat from startup folder
        set "REMOVED_SOMETHING=1"
    ) else (
        echo ✗ Failed to remove run.bat from startup folder
    )
) else (
    echo - No run.bat found in startup folder
)

REM Check for launcher script
if exist "%STARTUP_DIR%\necoconeco_launcher.bat" (
    echo Removing launcher script from startup folder...
    del "%STARTUP_DIR%\necoconeco_launcher.bat" >nul 2>&1
    if %ERRORLEVEL% EQU 0 (
        echo ✓ Removed launcher script from startup folder
        set "REMOVED_SOMETHING=1"
    ) else (
        echo ✗ Failed to remove launcher script from startup folder
    )
) else (
    echo - No launcher script found in startup folder
)

REM Check for alternate launcher name
if exist "%STARTUP_DIR%\necoconeco_startup.bat" (
    echo Removing startup script from startup folder...
    del "%STARTUP_DIR%\necoconeco_startup.bat" >nul 2>&1
    if %ERRORLEVEL% EQU 0 (
        echo ✓ Removed startup script from startup folder
        set "REMOVED_SOMETHING=1"
    ) else (
        echo ✗ Failed to remove startup script from startup folder
    )
) else (
    echo - No startup script found in startup folder
)

echo.
if %REMOVED_SOMETHING% EQU 1 (
    echo ✓ Uninstallation complete! Necoconeco will no longer start automatically.
) else (
    echo ℹ No startup entries found. Necoconeco was not configured to start automatically.
)

echo.
echo Press any key to exit...
pause >nul