# 修复 NuGet/MSBuild：补全 ProgramFiles 系环境变量
# 用法: . .\fix-nuget-env.ps1
$env:ProgramFiles = "C:\Program Files"
Set-Item -Path "Env:ProgramFiles(x86)" -Value "C:\Program Files (x86)" -Force
$env:ProgramW6432 = "C:\Program Files"
$env:CommonProgramFiles = "C:\Program Files\Common Files"
Set-Item -Path "Env:CommonProgramFiles(x86)" -Value "C:\Program Files (x86)\Common Files" -Force
$env:ProgramData = "C:\ProgramData"
$env:NUGET_PACKAGES = "C:\Users\wsc76\.nuget\packages"
Write-Host "[fix-nuget-env] ProgramFiles=$env:ProgramFiles"
