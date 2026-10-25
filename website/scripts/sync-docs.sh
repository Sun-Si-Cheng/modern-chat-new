#!/usr/bin/env bash
# ============================================================
# 同步仓库文档 → website/public/docs/（中文副本，Vite 会原样拷进 dist）
# 用法：在仓库根目录执行  bash website/scripts/sync-docs.sh
# - 中文副本直接覆盖拷贝（仓库文档是唯一真源）
# - 英文译本无法自动生成：若中文源比 en/ 译本新，仅提示需要人工修订
# ============================================================
set -e
cd "$(dirname "$0")/../.."   # 仓库根

declare -A MAP=(
  ["README.md"]="getting-started.md"
  ["CHANGELOG.md"]="changelog.md"
  ["docs/设计文档.md"]="design.md"
  ["docs/后端需求规格.md"]="api-spec.md"
  ["docs/前端技术方案.md"]="frontend-guide.md"
  ["docs/运维手册.md"]="ops-guide.md"
  ["server/README.md"]="server-readme.md"
  ["web/README.md"]="web-readme.md"
)

DST="website/public/docs"
EN="$DST/en"
mkdir -p "$EN"

stale=()
for src in "${!MAP[@]}"; do
  dst="${MAP[$src]}"
  cp "$src" "$DST/$dst"
  echo "copied  $src -> $DST/$dst"
  # 英文译本过期检测
  if [ ! -f "$EN/$dst" ]; then
    stale+=("$dst (missing EN)")
  elif [ "$src" -nt "$EN/$dst" ]; then
    stale+=("$dst (older than $src)")
  fi
done

echo ""
if [ ${#stale[@]} -eq 0 ]; then
  echo "OK: all EN translations are up to date."
else
  echo "ATTENTION: ${#stale[@]} EN translation(s) need manual revision:"
  for s in "${stale[@]}"; do echo "  - $EN/$s"; done
  echo "Edit the files under $EN/ to match the updated Chinese docs, then commit."
fi
