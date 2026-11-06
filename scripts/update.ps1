# ModernChat 一键升级（Windows / PowerShell）：从 Gitee 拉取 → 重建前后端 → 提示重启
#
# 用法（在仓库根目录）：
#   .\scripts\update.ps1                    # 拉 main
#   .\scripts\update.ps1 -Branch dev        # 拉指定分支
#   .\scripts\update.ps1 -NoRestart         # 只构建，稍后手动重启
param(
    [string]$Branch = "main",
    [switch]$NoRestart
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$module = "gitee.com/Sun-Si-Cheng/modern-chat-new/server"
$before = (git rev-parse --short HEAD).Trim()
Write-Host "==> 当前: $before ($(git rev-parse --abbrev-ref HEAD))" -ForegroundColor Cyan

# 1) 拉取（--ff-only：本地有分叉直接报错，绝不自动合并）
git fetch --all --prune
git checkout $Branch
git pull --ff-only origin $Branch

$after = (git rev-parse --short HEAD).Trim()
if ($before -eq $after) {
    Write-Host "==> 已是最新（$after），无需重建" -ForegroundColor Yellow
    exit 0
}

$version = (git describe --tags --always 2>$null)
if (-not $version) { $version = $after }
$buildTime = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

# 2) 后端
Write-Host "==> 构建后端 $version ($after)" -ForegroundColor Cyan
New-Item -ItemType Directory -Force -Path "$root\bin" | Out-Null
Push-Location "$root\server"
try {
    go build -trimpath -ldflags `
        "-s -w -X $module/internal/version.Version=$version -X $module/internal/version.Commit=$after -X $module/internal/version.BuildTime=$buildTime" `
        -o "$root\bin\modernchat-server.exe" ./cmd/server
} finally {
    Pop-Location
}

# 3) 前端
Write-Host "==> 构建前端" -ForegroundColor Cyan
Push-Location "$root\web"
try {
    npm ci --no-audit --no-fund
    npm run build
} finally {
    Pop-Location
}

Write-Host "==> 完成：$version ($after)" -ForegroundColor Green
if ($NoRestart) {
    Write-Host "    已跳过重启（产物：bin\modernchat-server.exe、web\dist）" -ForegroundColor Yellow
} else {
    Write-Host "    请重启后端进程使新版本生效；验证：curl http://127.0.0.1:8080/api/version" -ForegroundColor Yellow
}
