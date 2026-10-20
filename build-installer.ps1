# 构建 WallpaperTool 安装包
# 用法: .\build-installer.ps1 [-Version 0.3.0]
param(
    [string]$Version = "0.3.0"
)
$ErrorActionPreference = "Stop"
$root = $PSScriptRoot

# 1. 构建主程序(裁剪体积)
Push-Location $root
try {
    # 1) 生成 exe 的 Windows 资源(图标+版本信息)。
    #    Go 默认不生成 .syso, 缺失时 exe 在任务栏/资源管理器里没有图标。
    #    版本号由 -Version 统一驱动, 避免 exe 版本信息与安装包版本不一致。
    Write-Host "[0/3] 生成 exe 图标资源..." -ForegroundColor Cyan
    $parts = $Version.Split('.')
    $num = '"Major": ' + $parts[0] + ', "Minor": ' + $parts[1] + ', "Patch": ' + $parts[2] + ', "Build": 0'
    $gvi = [System.IO.File]::ReadAllText((Join-Path $root "build\gvi.json"), (New-Object System.Text.UTF8Encoding($false)))
    $gvi = $gvi -replace '"FileVersion":\s*\{[^}]*\}', ('"FileVersion": { ' + $num + ' }')
    $gvi = $gvi -replace '"ProductVersion":\s*\{[^}]*\}', ('"ProductVersion": { ' + $num + ' }')
    $gvi = $gvi -replace '"FileVersion":\s*"\d+\.\d+\.\d+"', ('"FileVersion": "' + $Version + '"')
    $gvi = $gvi -replace '"ProductVersion":\s*"\d+\.\d+\.\d+"', ('"ProductVersion": "' + $Version + '"')
    $tmpGvi = Join-Path $env:TEMP "wallpaper-tool-gvi.json"
    [System.IO.File]::WriteAllText($tmpGvi, $gvi, (New-Object System.Text.UTF8Encoding($false)))
    $gviArgs = @(
        "run", "github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest",
        "-icon", "$root\internal\assets\icon.ico",
        "-o", "rsrc_wallpaper-tool.syso", $tmpGvi
    )
    & go @gviArgs
    if ($LASTEXITCODE -ne 0) { throw "生成 syso 失败" }

    go build -trimpath -ldflags "-s -w -H=windowsgui -X wallpaper-tool/internal/version.Version=$Version" -o wallpaper-tool.exe .
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
