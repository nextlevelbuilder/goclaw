#!/bin/sh
# Watch Phase 5 Stage 1 PR CI (PR #15 + #16). Emits a line when state changes,
# exits when both PRs have no pending checks.
prev15=""
prev16=""
while true; do
  c15=$(gh pr checks 15 --repo qkhalk/goclaw 2>/dev/null | awk '{print $1"="$2}' | sort | tr '\n' ' ')
  c16=$(gh pr checks 16 --repo qkhalk/goclaw 2>/dev/null | awk '{print $1"="$2}' | sort | tr '\n' ' ')
  n15=$(printf '%s' "$c15" | grep -o 'pending' | wc -l)
  n16=$(printf '%s' "$c16" | grep -o 'pending' | wc -l)
  if [ "$n15" -eq 0 ] && [ "$n16" -eq 0 ]; then
    echo "DONE PR15: $c15"
    echo "DONE PR16: $c16"
    exit 0
  fi
  if [ "$c15" != "$prev15" ] || [ "$c16" != "$prev16" ]; then
    echo "PR15: $c15 PR16: $c16"
    prev15="$c15"
    prev16="$c16"
  fi
  sleep 30
done
