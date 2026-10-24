# 后端覆盖率（单测 + 集成测试合并）
#
# 用法（在 server/ 目录执行）：
#   .\..\scripts\coverage.ps1
#   $env:DB_DSN = 'root:密码@tcp(127.0.0.1:3306)/modernchat_test?...'; ..\scripts\coverage.ps1
#
# 说明：
#   - 集成测试需要 MySQL。连不上时用例会 SKIP（go test 仍返回 ok，是"假通过"），
#     本脚本检测到 SKIP 会明确告警，避免把跳过的用例当成通过。
#   - 覆盖率统计必须用 -coverpkg 指定 internal 各包，否则只统计到"包自己的单测"。
#   - 参数要用引号包裹（-coverpkg/-coverprofile），否则 PowerShell 解析异常。

$ErrorActionPreference = 'Continue'

$dsn = $env:DB_DSN
if (-not $dsn) {
  $dsn = 'root:root@tcp(127.0.0.1:3306)/modernchat_test?charset=utf8mb4&parseTime=True&loc=Local'
  Write-Host "[warn] 未设置 DB_DSN，使用默认 $dsn" -ForegroundColor Yellow
}
$env:DB_DSN = $dsn

$outFile = 'cover.out'
Write-Host "==> 运行单测 + 集成测试（-tags integration）" -ForegroundColor Cyan

$log = & go test -count=1 -tags integration -cover "-coverpkg=./internal/..." "-coverprofile=$outFile" ./... 2>&1
$log | ForEach-Object { $_ }

$skipped = ($log | Select-String -Pattern '^\s*---\s+SKIP').Count
if ($skipped -gt 0) {
  Write-Host ""
  Write-Host "[warn] 有 $skipped 个用例被 SKIP（多半是连不上 MySQL，请检查 DB_DSN 与数据库是否存在）" -ForegroundColor Yellow
  Write-Host "       跳过的用例不产生覆盖率，统计结果会偏低。" -ForegroundColor Yellow
}

if (Test-Path $outFile) {
  Write-Host ""
  Write-Host "==> 总覆盖率：" -ForegroundColor Cyan
  & go tool cover "-func=$outFile" | Select-String -Pattern '^total'

  Write-Host ""
  Write-Host "==> 各包覆盖率：" -ForegroundColor Cyan
  & go tool cover "-func=$outFile" |
    ForEach-Object {
      if ($_ -match 'internal/([^/]+)/.*\s+([\d.]+)%$') {
        [PSCustomObject]@{ Pkg = $Matches[1]; Pct = [double]$Matches[2] }
      }
    } |
    Group-Object Pkg |
    ForEach-Object {
      $avg = [math]::Round((($_.Group.Pct | Measure-Object -Average).Average), 1)
      '  {0,-12} {1}%' -f $_.Name, $avg
    }

  Write-Host ""
  Write-Host "提示：生成 HTML 报告 → go tool cover -html=$outFile -o coverage.html" -ForegroundColor DarkGray
}
else {
  Write-Host "[error] 未生成 $outFile" -ForegroundColor Red
  exit 1
}
