# 构建 WallpaperTool 安装包
# 用法: .\build-installer.ps1 [-Version 0.2.0]
param(
    [string]$Version = "0.2.0"
)
$ErrorActionPreference = "Stop"
$root = $PSScriptRoot

# 1. 构建主程序(裁剪体积)
Push-Location $root
try {
    go build -trimpath -ldflags "-s -w -H=windowsgui" -o wallpaper-tool.exe .
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    Write-Host "[1/2] exe 构建完成" -ForegroundColor Green

    # 2. 打安装包
    $iscc = Join-Path $env:LOCALAPPDATA "Programs\Inno Setup 7\ISCC.exe"
    if (-not (Test-Path $iscc)) { $iscc = "C:\Program Files (x86)\Inno Setup 7\ISCC.exe" }
    if (-not (Test-Path $iscc)) { throw "未找到 ISCC.exe, 请先安装 Inno Setup" }
    & $iscc "/DMyAppVersion=$Version" (Join-Path $root "installer\wallpaper-tool.iss")
    if ($LASTEXITCODE -ne 0) { throw "ISCC failed" }
    Write-Host "[2/2] 安装包构建完成: dist\WallpaperTool-setup-$Version.exe" -ForegroundColor Green
}
finally {
    Pop-Location
}
