# copse lifecycle internals

How copse tracks tasks, decides what "merged" means, and chooses which
files to carry. Everything here is implementation detail that v0.x may
evolve — the CLI surface and the JSON envelopes are the stable contract.

## Task state

copse stores its state in one JSON file inside the repository's *common*
git directory, so every worktree sees the same list and nothing is ever
committed:

```
<repo>/.git/copse/tasks.json
```

```json
{
  "schema_version": 1,
  "tasks": [
    {
      "name": "rate-limit",
      "branch": "copse/rate-limit",
      "path": "/work/acme-api.copse/rate-limit",
      "base": "main",
      "start_hash": "fccab37…",
      "created_at": "2026-07-12T09:00:00Z",
      "note": "429 retry middleware",
      "carried": [".env", "services/worker/.env.local"]
    }
  ]
}
```

Writes are atomic (temp file + rename). A `schema_version` newer than the
binary understands is refused loudly instead of being rewritten.

`start_hash` pins the commit the branch was cut at. It is what lets copse
tell a **fresh** task (tip still equals `start_hash` — nothing to lose,
but also nothing to prune) apart from a branch whose commits genuinely
landed in the base.

## Merge detection

`copse prune` and the `STATE` column of `copse ls` apply these rules in
order of confidence; the first hit wins and is quoted as the reason:

| # | Rule | Evidence | Catches |
|---|---|---|---|
| 0 | branch deleted | `refs/heads/<branch>` missing | manual cleanup |
| 1 | fresh | branch tip == `start_hash` | just-created tasks (kept, never pruned) |
| 2 | ancestor | `git merge-base --is-ancestor` | fast-forward and merge-commit merges |
| 3 | patch-equivalent | every `git cherry <base> <branch>` line is `-` | rebase merges |
| 4 | squash | whole-branch probe commit is `-` in `git cherry` | squash merges |
| 5 | upstream gone | `%(upstream:track)` is `[gone]` | deleted remote branches (opt-in via `--gone`) |

Rule 4 is the interesting one. A squash merge leaves every individual
branch commit looking unmerged, so copse builds an **unreferenced probe
commit**: the branch's final tree parented directly on the merge-base,
i.e. the whole branch squashed into one patch. If `git cherry` says that
patch already exists in the base, the branch's combined diff has landed.
The probe object is never referenced by any ref and is garbage-collected
by git in due course; the repository is not modified.

Rule 5 never claims "merged" — a deleted remote branch proves nothing
about where the work went — which is why it is opt-in and reported as
`upstream branch gone`, not as a merge.

## Dirty checks

Before removing anything, copse runs `git status --porcelain -z` in the
task worktree and blocks on:

- any change to a tracked file, and
- any untracked file **except** the ones copse itself carried in.

Ignored files never block. This is why carried `.env` files (untracked by
design) do not make every task permanently "dirty", while a stray
`notes.md` an agent left behind does — until you decide with `--force`.

## Carry semantics

- Patterns match the path **relative to the main worktree** with `/`
  separators; `*` and `?` stay within a segment, `**` spans segments.
- A pattern without `/` matches the basename, so the defaults `.env` and
  `.env.*` find env files at any depth.
- Only **regular, untracked** files are carried: tracked files travel
  with the branch already, symlinks and specials are skipped, and any
  directory containing a `.git` entry (submodule, nested worktree) is
  skipped wholesale.
- `copse env` re-discovers from the main worktree on every run, so files
  added or rotated after `copse new` are picked up; `--check` compares
  byte-for-byte and exits 1 on any drift.
