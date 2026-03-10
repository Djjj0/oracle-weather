@echo off
REM Start trading bot with clean output
cd /d "%~dp0"
bin\bot.exe 2>nul
