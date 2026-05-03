#!/usr/bin/env bash
set -euo pipefail
MAX_KB=500
while IFS= read -r file; do
  [ -f "$file" ] || continue
  size=$(du -k "$file" | cut -f1)
  if [ "$size" -gt "$MAX_KB" ]; then
    echo "large file ($size KB): $file" >&2
    exit 1
  fi
done < <(git diff --cached --name-only)
