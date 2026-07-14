#!/usr/bin/env bash
# Fan out one worktree per task for parallel coding agents: each agent
# gets an isolated checkout with the repo's .env files already in place,
# and `copse prune` cleans up behind whichever branches merge first.
# Usage: bash examples/agent-fanout.sh <repo-dir> <task>...
set -euo pipefail

REPO="${1:?usage: agent-fanout.sh <repo-dir> <task>...}"
shift
[ "$#" -gt 0 ] || { echo "name at least one task" >&2; exit 2; }
COPSE="${COPSE:-copse}"

for task in "$@"; do
  # --porcelain prints only the worktree path, ready to hand to an agent
  # (or to `cd`, or to a tmux pane, or to a container bind mount).
  path="$("$COPSE" -C "$REPO" new "$task" --porcelain)"
  echo "$task -> $path"
  # Launch your agent of choice here, e.g.:
  #   (cd "$path" && my-coding-agent --task "$task") &
done

echo
echo "when the dust settles:"
echo "  $COPSE -C $REPO ls              # who is ahead, who is dirty"
echo "  $COPSE -C $REPO env --all       # re-sync rotated env files"
echo "  $COPSE -C $REPO prune --dry-run # what merged while you slept"
