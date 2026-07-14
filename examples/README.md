# copse examples

Two runnable scripts, both offline and self-contained. Set `COPSE` to a
local build (`go build -o copse ./cmd/copse`) if copse is not on PATH.

## make-demo-repo.sh

Fabricates a repository with gitignored env files and two tasks: one
merged (so `prune` has something to remove) and one fresh.

```bash
bash examples/make-demo-repo.sh /tmp/copse-demo
copse -C /tmp/copse-demo/acme-api ls
copse -C /tmp/copse-demo/acme-api prune --dry-run
```

## agent-fanout.sh

The parallel-agent workflow this tool was built for: one named worktree
per task, env files carried into each, paths printed one per line for
whatever runs your agents.

```bash
bash examples/agent-fanout.sh /tmp/copse-demo/acme-api fix-429 dark-mode audit-log
```

`make-demo-repo.sh` pins git identity, dates, and configuration, so the
repository it fabricates — commit hashes included — is identical on
every machine. `agent-fanout.sh` deliberately does not: it runs against
your real repository with your own git settings.
