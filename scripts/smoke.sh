#!/usr/bin/env bash
# End-to-end smoke test for copse: builds the binary, fabricates a
# deterministic git repository, and drives the full task lifecycle —
# new → ls → env drift → merge → prune — asserting on real CLI output.
# No network, idempotent, finishes in seconds.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

fail() {
  echo "SMOKE FAIL: $*" >&2
  exit 1
}

BIN="$WORKDIR/copse"
REPO="$WORKDIR/demo"

# Isolate git completely from the host user's configuration.
export GIT_CONFIG_GLOBAL=/dev/null
export GIT_CONFIG_SYSTEM=/dev/null
export GIT_AUTHOR_NAME="Dev Human"
export GIT_AUTHOR_EMAIL="dev@example.test"
export GIT_COMMITTER_NAME="Dev Human"
export GIT_COMMITTER_EMAIL="dev@example.test"
export GIT_AUTHOR_DATE="2026-07-12T09:00:00+00:00"
export GIT_COMMITTER_DATE="2026-07-12T09:00:00+00:00"

echo "1. build"
(cd "$ROOT" && go build -o "$BIN" ./cmd/copse) || fail "go build failed"

echo "2. version matches manifest"
# Capture output before grepping: `cmd | grep -q` races under pipefail,
# because grep exits at the first match and SIGPIPEs the producer.
OUT="$("$BIN" version)"
echo "$OUT" | grep -qx "copse 0.1.0" || fail "version mismatch"

echo "3. fabricate a repository with env files"
mkdir -p "$REPO"
git -C "$REPO" init -q -b main
printf '.env\n.env.*\n' > "$REPO/.gitignore"
printf 'package api\n' > "$REPO/api.go"
git -C "$REPO" add -A
git -C "$REPO" commit -q --no-gpg-sign -m "initial"
printf 'API_TOKEN=local-dev\n' > "$REPO/.env"
mkdir -p "$REPO/services/worker"
printf 'QUEUE_URL=amqp://127.0.0.1:5672\n' > "$REPO/services/worker/.env.local"

echo "4. new creates a worktree, branch, and carried env files"
OUT="$("$BIN" -C "$REPO" new rate-limit --note "429 retry")"
echo "$OUT" | grep -q "created task rate-limit" || fail "new output missing header"
echo "$OUT" | grep -q "copse/rate-limit" || fail "new output missing branch"
WT="$("$BIN" -C "$REPO" path rate-limit)"
[ -d "$WT" ] || fail "worktree directory missing"
grep -qx 'API_TOKEN=local-dev' "$WT/.env" || fail ".env not carried"
grep -qx 'QUEUE_URL=amqp://127.0.0.1:5672' "$WT/services/worker/.env.local" \
  || fail "nested .env.local not carried"

echo "5. ls shows the fresh task with its note"
OUT="$("$BIN" -C "$REPO" ls)"
echo "$OUT" | grep -q "rate-limit" || fail "ls missing task"
echo "$OUT" | grep -q "fresh" || fail "ls missing fresh state"
echo "$OUT" | grep -q "429 retry" || fail "ls missing note"

echo "6. env --check finds drift, env repairs it"
printf 'API_TOKEN=rotated\n' > "$REPO/.env"
set +e
"$BIN" -C "$REPO" env --check rate-limit > "$WORKDIR/check.out"
CODE=$?
set -e
[ "$CODE" -eq 1 ] || fail "env --check should exit 1 on drift (got $CODE)"
grep -q "stale" "$WORKDIR/check.out" || fail "drift not reported as stale"
OUT="$("$BIN" -C "$REPO" env rate-limit)"
echo "$OUT" | grep -q "update" || fail "env sync did not update"
grep -qx 'API_TOKEN=rotated' "$WT/.env" || fail "rotated token not synced"
"$BIN" -C "$REPO" env --check rate-limit >/dev/null || fail "post-sync check should pass"

echo "7. merged work is detected and pruned"
printf 'package api // retry\n' > "$WT/retry.go"
git -C "$WT" add -A
git -C "$WT" commit -q --no-gpg-sign -m "add retry backoff"
git -C "$REPO" merge -q --no-ff --no-gpg-sign -m "merge rate-limit" copse/rate-limit
OUT="$("$BIN" -C "$REPO" prune --dry-run)"
echo "$OUT" | grep -q "would prune  rate-limit" \
  || fail "dry-run did not preview the prune"
[ -d "$WT" ] || fail "dry-run must not remove the worktree"
OUT="$("$BIN" -C "$REPO" prune)"
echo "$OUT" | grep -q "merged into main (ancestor)" \
  || fail "prune did not state its evidence"
[ ! -d "$WT" ] || fail "worktree still present after prune"
OUT="$(git -C "$REPO" branch --list "copse/rate-limit")"
[ -z "$OUT" ] || fail "branch still present after prune"

echo "8. JSON output is machine-readable"
OUT="$("$BIN" -C "$REPO" ls --format json)"
echo "$OUT" | grep -q '"tool": "copse"' \
  || fail "ls JSON envelope missing"

echo "9. usage errors exit 2"
set +e
"$BIN" -C "$REPO" new "bad name" >/dev/null 2>&1
[ $? -eq 2 ] || fail "invalid task name should exit 2"
set -e

echo "SMOKE OK"
