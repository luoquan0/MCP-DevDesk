@echo off
cd /d "%~dp0"
if not exist "%~dp0dist\MCP-DevDesk-amd64.exe" (
  echo MCP DevDesk 尚未编译，正在构建...
  powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0build.ps1" -RunTests
  if errorlevel 1 (
    pause
    exit /b 1
  )
)
start "" "%~dp0dist\MCP-DevDesk-amd64.exe"

