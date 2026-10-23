# 一键构建 WinUI 3 短剧库桌面端
# 用法: powershell -File build-winui.ps1
$ErrorActionPreference = "Stop"

# 1) 修复 NuGet（本机 ProgramFiles 环境变量缺失）
$env:ProgramFiles = "C:\Program Files"
Set-Item -Path "Env:ProgramFiles(x86)" -Value "C:\Program Files (x86)" -Force
$env:ProgramW6432 = "C:\Program Files"
$env:CommonProgramFiles = "C:\Program Files\Common Files"
Set-Item -Path "Env:CommonProgramFiles(x86)" -Value "C:\Program Files (x86)\Common Files" -Force

$proj = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $proj

Write-Host "[1/3] build ..."
dotnet build JukuWinUI.csproj -c Debug -p:Platform=x64
if ($LASTEXITCODE -ne 0) { throw "build failed" }

$bin = Join-Path $proj "bin\x64\Debug\net9.0-windows10.0.19041.0"
# 2) 拷贝 1.7 Bootstrap 原生库
$boot = Get-ChildItem "$env:USERPROFILE\.nuget\packages\microsoft.windowsappsdk\1.7*" -Recurse -Filter "Microsoft.WindowsAppRuntime.Bootstrap.dll" |
  Where-Object { $_.FullName -match "win-x64" } | Select-Object -First 1
if ($boot) { Copy-Item $boot.FullName $bin -Force }

Write-Host "[2/3] output: $bin\JukuWinUI.exe"
Write-Host "[3/3] launching ..."
Start-Process (Join-Path $bin "JukuWinUI.exe") -WorkingDirectory $bin
