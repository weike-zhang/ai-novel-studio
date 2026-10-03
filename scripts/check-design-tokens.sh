#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HITS="$(grep -rEn '#[0-9a-fA-F]{3,8}\b' "$ROOT/apps/web/app" --include='*.css' | grep -v 'tokens.css' | grep -v 'var(--' || true)"

if [ -n "$HITS" ]; then
  echo "设计 token 守卫失败：业务 CSS 出现了裸色值（不在 var(--…, #fallback) 兜底中）。请改用 design-tokens.json 中的 token。"
  echo "$HITS"
  exit 1
fi

echo "设计 token 守卫通过"