@echo off
chcp 65001 >nul
title 短剧库
cd /d "%~dp0.."
if not exist "dist\juku_windows_amd64.exe" (
  echo 找不到下载器，请先编译或获取包含 dist 的完整版本。
  pause
  exit /b 1
)
"dist\juku_windows_amd64.exe" %*
if errorlevel 1 pause
