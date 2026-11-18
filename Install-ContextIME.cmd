@echo off
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0native-tsf\Install.ps1" %*
pause
