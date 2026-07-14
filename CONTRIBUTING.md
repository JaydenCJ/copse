# Contributing to copse

Issues, discussions and pull requests are all welcome.

## Getting started

You need Go ≥1.22 and git ≥2.31; nothing else.

```bash
git clone https://github.com/JaydenCJ/copse && cd copse
go build ./...
go test ./...
bash scripts/smoke.sh
```

`scripts/smoke.sh` builds the binary, fabricates a deterministic
repository with env files in a temp dir, and drives the full lifecycle —
new → ls → env drift → merge → prune — asserting on real CLI output; it
must finish by printing `SMOKE OK`.

## Before you open a pull request

1. `gofmt -l .` reports nothing (formatting is enforced).
2. `go vet ./...` passes with no findings.
3. `go test ./...` passes (87 deterministic tests, no network).
4. `bash scripts/smoke.sh` prints `SMOKE OK`.
5. Add tests for behavior changes; keep logic in pure, unit-testable
   modules (parsers, glob matching, and the merge verdict never shell
   out — only `gitio.Git` does).

## Ground rules

- Keep dependencies at zero; adding one needs strong justification in
  the PR. copse's only external interface is the local `git` binary.
- No network calls, ever. No telemetry.
- Never destroy work silently: any code path that deletes a worktree or
  branch must pass the dirty check and the merge verdict first, and
  `--force` must remain an explicit, per-invocation decision.
- Merge-detection rules are ordered by confidence in
  `internal/merged/merged.go`; a new rule needs a unit test per merge
  shape and a row in `docs/lifecycle.md`.
- Code comments and doc comments are written in English.
- Determinism first: identical repository state must produce
  byte-identical reports, including all orderings.

## Reporting bugs

Include the output of `copse version`, the full command you ran, the
output of `copse ls --format json` (redact paths if needed), and — for
wrong merge verdicts — the shape of the history involved
(`git log --graph --oneline base branch`), since that is exactly what
the verdict sees.

## Security

Please do not open public issues for security problems; use GitHub's
private vulnerability reporting on this repository instead.
