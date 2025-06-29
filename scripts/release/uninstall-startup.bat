@echo off
set SHORTCUT_NAME="necoconeco.lnk"
set STARTUP_PATH="%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup"

echo Removing startup shortcut...
del %STARTUP_PATH%\%SHORTCUT_NAME%

echo necoconeco has been removed from startup.
pause