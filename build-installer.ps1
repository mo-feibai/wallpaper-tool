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
    # 1) 确保 exe 的 Windows 资源(图标+版本信息)已嵌入。
    #    Go 默认不生成 .syso, 缺失时 exe 在任务栏/资源管理器里没有图标。
    if (-not (Test-Path "rsrc_wallpaper-tool.syso")) {
        Write-Host "[0/3] 生成 exe 图标资源..." -ForegroundColor Cyan
        $gviArgs = @(
            "run", "github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest",
            "-icon", "$root\internal\assets\icon.ico",
            "-o", "rsrc_wallpaper-tool.syso", "build\gvi.json"
        )
        & go @gviArgs
        if ($LASTEXITCODE -ne 0) { throw "生成 syso 失败" }
    }

    go build -trimpath -ldflags "-s -w -H=windowsgui" -o wallpaper-tool.exe .
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    Write-Host "[1/3] exe 构建完成(含图标资源)" -ForegroundColor Green

    # 2. 打安装包
    $iscc = Join-Path $env:LOCALAPPDATA "Programs\Inno Setup 7\ISCC.exe"
    if (-not (Test-Path $iscc)) { $iscc = "C:\Program Files (x86)\Inno Setup 7\ISCC.exe" }
    if (-not (Test-Path $iscc)) { throw "未找到 ISCC.exe, 请先安装 Inno Setup" }
    & $iscc "/DMyAppVersion=$Version" (Join-Path $root "installer\wallpaper-tool.iss")
    if ($LASTEXITCODE -ne 0) { throw "ISCC failed" }
    Write-Host "[2/3] 安装包构建完成" -ForegroundColor Green
    Write-Host "[3/3] 产物: dist\WallpaperTool-setup-$Version.exe" -ForegroundColor Green
}
finally {
    Pop-Location
}
