# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-07-12

### Added

- `copse new <task>`: one command creates the branch (configurable
  prefix), the worktree (configurable root, default `../<repo>.copse/`),
  carries untracked env files in, and records the task — name, note,
  base branch, start hash — in `.git/copse/tasks.json` (atomic writes,
  versioned schema, shared by all worktrees).
- Env carrying: `.env` / `.env.*` at any depth by default, extensible
  per-repo (`git config copse.carry`) and per-invocation (`--carry`);
  only regular untracked files are copied, with permission bits kept and
  submodules/nested worktrees/symlinks skipped.
- `copse env`: re-sync carried files after secrets rotate (`--all` for
  every task), and `--check` mode that reports ok/stale/missing per file
  and exits 1 on any drift — scriptable freshness for five parallel
  checkouts.
- `copse ls` (default command): fresh/active/merged/gone/broken/missing
  state per task, dirty flag that excuses carried files, ahead/behind
  counts, notes, and a stable JSON envelope (`schema_version: 1`).
- `copse prune`: removes worktree + branch + state for every task whose
  work landed in the base, detecting merge commits (ancestry), rebase
  merges (patch-equivalence via `git cherry`), and squash merges (an
  unreferenced whole-branch probe commit) — each pruned task quotes its
  evidence; `--dry-run`, `--force`, `--keep-branch`, `--gone`, JSON output.
- Safety rails throughout: fresh tasks (no commits yet) are never
  pruned, dirty worktrees are skipped without `--force`, `copse rm`
  refuses to delete unmerged commits, and untracked files an agent left
  behind count as dirty while carried env files do not.
- `copse path` and `new --porcelain` for scripting; `-C dir` global
  flag; configuration via `git config` (`copse.root`,
  `copse.branchprefix`, `copse.base`, `copse.carry`).
- Runnable examples (`examples/make-demo-repo.sh`,
  `examples/agent-fanout.sh`) and a lifecycle-internals reference
  (`docs/lifecycle.md`).
- 87 deterministic offline tests (pure-unit + in-process CLI integration
  against fabricated git repositories) and `scripts/smoke.sh`.

[0.1.0]: https://github.com/JaydenCJ/copse/releases/tag/v0.1.0
