# ============================================================
# 同步仓库文档 → website/public/content/docs/（中文副本，Vite 会原样拷进 dist）
# 注意：不放 public/docs/ —— 那会与前端路由 /docs/:file 撞路径，
# 静态服务器会优先返回 .md 原文，刷新文档页就变成"裸 Markdown"。
# 用法：在仓库根目录执行  .\website\scripts\sync-docs.ps1
# - 中文副本直接覆盖拷贝（仓库文档是唯一真源）
# - 英文译本无法自动生成：若中文源比 en/ 译本新，仅提示需要人工修订
# ============================================================
$ErrorActionPreference = 'Stop'
$root = (Get-Item $PSCommandPath).Directory.Parent.Parent.FullName   # scripts → website → 仓库根
Set-Location $root

$map = [ordered]@{
  'README.md'            = 'getting-started.md'
  'CHANGELOG.md'         = 'changelog.md'
  'docs/设计文档.md'      = 'design.md'
  'docs/后端需求规格.md'  = 'api-spec.md'
  'docs/前端技术方案.md'  = 'frontend-guide.md'
  'docs/运维手册.md'      = 'ops-guide.md'
  'docs/安全与迁移指南.md' = 'security-migration-guide.md'
  'server/README.md'     = 'server-readme.md'
  'web/README.md'        = 'web-readme.md'
}

$dst = 'website/public/content/docs'
$en  = Join-Path $dst 'en'
New-Item -ItemType Directory -Force -Path $en | Out-Null

$stale = @()
foreach ($src in $map.Keys) {
  $name = $map[$src]
  Copy-Item $src (Join-Path $dst $name) -Force
  Write-Host "copied  $src -> $dst/$name"

  $srcFile    = Get-Item $src
  $enFile     = Join-Path $en $name
  if (-not (Test-Path $enFile)) {
    $stale += "$name (missing EN)"
  } elseif ((Get-Item $enFile).LastWriteTime -lt $srcFile.LastWriteTime) {
    $stale += "$name (older than $src)"
  }
}

Write-Host ''
if ($stale.Count -eq 0) {
  Write-Host 'OK: all EN translations are up to date.' -ForegroundColor Green
} else {
  Write-Host ("ATTENTION: {0} EN translation(s) need manual revision:" -f $stale.Count) -ForegroundColor Yellow
  $stale | ForEach-Object { Write-Host "  - $en/$_" }
  Write-Host "Edit the files under $en/ to match the updated Chinese docs, then commit."
}
