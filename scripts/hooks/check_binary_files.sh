#!/usr/bin/env bash
set -euo pipefail
while IFS= read -r file; do
  [ -f "$file" ] || continue
  if file "$file" | grep -q 'binary'; then
    echo "binary file staged: $file" >&2
    exit 1
  fi
done < <(git diff --cached --name-only)
