@echo off
set SCRIPT_PATH=%~dp0run.bat
set STARTUP_FOLDER=%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup
set SHORTCUT_NAME=necoconeco.lnk

:: Construct the full, quoted path to the shortcut link file
set "FULL_SHORTCUT_PATH=%STARTUP_FOLDER%\%SHORTCUT_NAME%"

echo Creating startup shortcut...
echo Target: %SCRIPT_PATH%
echo Link: %FULL_SHORTCUT_PATH%

:: Execute the PowerShell command, ensuring paths are correctly quoted
powershell -Command "$ws = New-Object -ComObject WScript.Shell; $s = $ws.CreateShortcut('"%FULL_SHORTCUT_PATH%"'); $s.TargetPath = '%SCRIPT_PATH%'; $s.Save()"

echo.
echo necoconeco has been set to run on startup.
pause