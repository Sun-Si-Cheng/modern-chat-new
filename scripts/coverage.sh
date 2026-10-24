#!/usr/bin/env bash
# 后端覆盖率（单测 + 集成测试合并）
#
# 用法（在 server/ 目录执行）：
#   ../scripts/coverage.sh
#   DB_DSN='root:密码@tcp(127.0.0.1:3306)/modernchat_test?...' ../scripts/coverage.sh
#
# 说明：
#   - 集成测试需要 MySQL；连不上时用例会 SKIP（go test 仍返回 ok，是"假通过"），
#     脚本检测到 SKIP 会告警。
#   - 必须用 -coverpkg 指定 internal 各包，否则只统计到"包自己的单测"覆盖。

set -uo pipefail

DSN="${DB_DSN:-root:root@tcp(127.0.0.1:3306)/modernchat_test?charset=utf8mb4&parseTime=True&loc=Local}"
if [ -z "${DB_DSN:-}" ]; then
  echo "[warn] 未设置 DB_DSN，使用默认 $DSN" >&2
fi
export DB_DSN="$DSN"

OUT=cover.out
echo "==> 运行单测 + 集成测试（-tags integration）"

LOG=$(go test -count=1 -tags integration -cover -coverpkg=./internal/... -coverprofile="$OUT" ./... 2>&1)
echo "$LOG"

SKIPPED=$(echo "$LOG" | grep -c -- '--- SKIP' || true)
if [ "${SKIPPED:-0}" -gt 0 ]; then
  echo ""
  echo "[warn] 有 $SKIPPED 个用例被 SKIP（多半是连不上 MySQL，请检查 DB_DSN 与数据库）"
  echo "       跳过的用例不产生覆盖率，统计结果会偏低。"
fi

if [ -f "$OUT" ]; then
  echo ""
  echo "==> 总覆盖率："
  go tool cover -func="$OUT" | grep '^total'

  echo ""
  echo "==> 各包覆盖率："
  go tool cover -func="$OUT" \
    | grep -o 'internal/[a-z]*/.*[0-9.]*%$' \
    | awk -F'/' '{print $2}' \
    | awk '{print $1, $NF}' \
    | sed 's/%//' \
    | awk '{s[$1]+=$2; n[$1]++} END {for (p in s) printf "  %-12s %.1f%%\n", p, s[p]/n[p]}' \
    | sort

  echo ""
  echo "提示：生成 HTML 报告 → go tool cover -html=$OUT -o coverage.html"
else
  echo "[error] 未生成 $OUT" >&2
  exit 1
fi
