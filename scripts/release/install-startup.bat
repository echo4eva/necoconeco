@echo off
set SCRIPT_PATH=%~dp0run.bat
set SHORTCUT_NAME="necoconeco.lnk"
set STARTUP_PATH="%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup"

echo Creating startup shortcut...
powershell -Command "$ws = New-Object -ComObject WScript.Shell; $s = $ws.CreateShortcut(%STARTUP_PATH%\%SHORTCUT_NAME%); $s.TargetPath = '%SCRIPT_PATH%'; $s.Save()"

echo necoconeco has been set to run on startup.
pause