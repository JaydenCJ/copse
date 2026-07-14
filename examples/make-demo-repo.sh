#!/usr/bin/env bash
# Fabricates a small repository with env files and two copse tasks — one
# merged, one in flight — so every subcommand has something to show.
# Usage: bash examples/make-demo-repo.sh [dir]   (default: /tmp/copse-demo)
set -euo pipefail

DEST="${1:-/tmp/copse-demo}"
COPSE="${COPSE:-copse}" # set COPSE=/path/to/binary to use a local build

export GIT_CONFIG_GLOBAL=/dev/null
export GIT_CONFIG_SYSTEM=/dev/null
export GIT_AUTHOR_NAME="Dev Human"
export GIT_AUTHOR_EMAIL="dev@example.test"
export GIT_COMMITTER_NAME="Dev Human"
export GIT_COMMITTER_EMAIL="dev@example.test"
export GIT_AUTHOR_DATE="2026-07-12T09:00:00+00:00"
export GIT_COMMITTER_DATE="2026-07-12T09:00:00+00:00"

rm -rf "$DEST"
mkdir -p "$DEST/acme-api"
cd "$DEST/acme-api"

git init -q -b main
printf '.env\n.env.*\n' > .gitignore
printf 'package api\n' > api.go
mkdir -p services/worker
printf 'package worker\n' > services/worker/worker.go
git add -A
git commit -q -m "initial"

# The untracked env files copse will carry into every task worktree.
printf 'DATABASE_URL=postgres://127.0.0.1:5432/acme\nAPI_TOKEN=local-dev\n' > .env
printf 'QUEUE_URL=amqp://127.0.0.1:5672\n' > services/worker/.env.local

# Task 1: created, worked on, and merged — prune fodder.
"$COPSE" new auth-retry --note "retry with backoff"
WT="$("$COPSE" path auth-retry)"
printf 'package api // retry\n' > "$WT/retry.go"
git -C "$WT" add -A
git -C "$WT" commit -q -m "add retry backoff"
git merge -q --no-ff -m "Merge auth-retry (#12)" copse/auth-retry

# Task 2: fresh, still waiting for its first commit.
"$COPSE" new rate-limit --note "429 retry middleware"

echo
echo "demo ready in $DEST/acme-api — try:"
echo "  $COPSE -C $DEST/acme-api ls"
echo "  $COPSE -C $DEST/acme-api prune --dry-run"
