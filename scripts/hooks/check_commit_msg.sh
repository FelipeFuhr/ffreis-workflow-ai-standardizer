#!/usr/bin/env bash
set -euo pipefail
msg=$(cat "$1")
pattern='^(feat|fix|docs|chore|refactor|test|ci|perf|style|build|revert)(\([a-z0-9-]+\))?: .{1,100}$'
if ! echo "$msg" | grep -qE "$pattern"; then
  echo "commit message does not follow conventional commits: $msg" >&2
  exit 1
fi
