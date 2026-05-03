#!/usr/bin/env bash
set -euo pipefail
files=$(git diff --cached --name-only)
if [ -z "$files" ]; then exit 0; fi
bad=$(echo "$files" | xargs grep -l '^<<<<<<< \|^=======$\|^>>>>>>> ' 2>/dev/null || true)
if [ -n "$bad" ]; then
  echo "merge markers found in: $bad" >&2
  exit 1
fi
