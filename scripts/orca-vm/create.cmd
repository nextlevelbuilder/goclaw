@echo off
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0create.ps1"
exit /b %ERRORLEVEL%
