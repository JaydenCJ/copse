// In-process CLI integration tests. Every test fabricates a real git
// repository in a temp dir (offline, pinned identity and dates) and
// drives cli.Run exactly as main() would — the same seam the smoke
// script exercises from the outside.
package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	// Isolate every git invocation — the tests' own and the ones copse
	// spawns — from the host user's configuration and identity.
	os.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	os.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	os.Setenv("GIT_AUTHOR_NAME", "Dev Human")
	os.Setenv("GIT_AUTHOR_EMAIL", "dev@example.test")
	os.Setenv("GIT_COMMITTER_NAME", "Dev Human")
	os.Setenv("GIT_COMMITTER_EMAIL", "dev@example.test")
	os.Setenv("GIT_AUTHOR_DATE", "2026-01-01T10:00:00+00:00")
	os.Setenv("GIT_COMMITTER_DATE", "2026-01-01T10:00:00+00:00")
	os.Exit(m.Run())
}

// git runs a git command in dir and fails the test on error.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// write creates a file (and parents) under root.
func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// newRepo builds the classic shape copse serves: a repo named "demo" on
// main, with tracked source, gitignored .env files, and a live .env.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "-b", "main")
	write(t, dir, ".gitignore", ".env\n.env.*\n")
	write(t, dir, "app.go", "package app\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "initial")
	write(t, dir, ".env", "API_URL=http://127.0.0.1:8080\nTOKEN=local-dev\n")
	return dir
}

// run drives cli.Run with -C so tests never chdir.
func run(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	var out, errB strings.Builder
	code := Run(append([]string{"-C", dir}, args...), &out, &errB)
	return code, out.String(), errB.String()
}

// mustRun asserts exit 0 and returns stdout.
func mustRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	code, out, errS := run(t, dir, args...)
	if code != 0 {
		t.Fatalf("copse %v: exit %d\nstdout: %s\nstderr: %s", args, code, out, errS)
	}
	return out
}

// taskPath returns the default worktree path for a task of the demo repo.
func taskPath(repo, name string) string {
	return filepath.Join(filepath.Dir(repo), "demo.copse", name)
}

// commitIn adds a file and commits it inside a worktree.
func commitIn(t *testing.T, wt, rel, content, msg string) {
	t.Helper()
	write(t, wt, rel, content)
	git(t, wt, "add", "-A")
	git(t, wt, "commit", "-q", "-m", msg)
}

func TestNewCreatesWorktreeBranchStateAndCarries(t *testing.T) {
	repo := newRepo(t)
	out := mustRun(t, repo, "new", "rate-limit")
	for _, want := range []string{"created task rate-limit", "copse/rate-limit", "from main @", "carried   .env"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	wt := taskPath(repo, "rate-limit")
	if got := read(t, wt, ".env"); got != read(t, repo, ".env") {
		t.Fatalf("carried .env content differs: %q", got)
	}
	if got := git(t, wt, "rev-parse", "--abbrev-ref", "HEAD"); got != "copse/rate-limit" {
		t.Fatalf("worktree on branch %q, want copse/rate-limit", got)
	}
	// State lands in the common git dir, shared across worktrees.
	state := read(t, repo, ".git/copse/tasks.json")
	if !strings.Contains(state, `"name": "rate-limit"`) || !strings.Contains(state, `"base": "main"`) {
		t.Fatalf("state file wrong:\n%s", state)
	}
}

func TestNewCarriesNestedButNotTrackedFiles(t *testing.T) {
	repo := newRepo(t)
	write(t, repo, "services/api/.env.local", "PORT=3001\n")
	write(t, repo, ".env.example", "API_URL=fill-me-in\n")
	git(t, repo, "add", "-f", ".env.example")
	git(t, repo, "commit", "-q", "-m", "add env example")

	out := mustRun(t, repo, "new", "auth")
	if !strings.Contains(out, "services/api/.env.local") {
		t.Fatalf("nested env file not carried:\n%s", out)
	}
	wt := taskPath(repo, "auth")
	if read(t, wt, "services/api/.env.local") != "PORT=3001\n" {
		t.Fatal("nested carry content wrong")
	}
	// .env.example is tracked: it arrives via checkout, not via carry.
	state := read(t, repo, ".git/copse/tasks.json")
	if strings.Contains(state, ".env.example") {
		t.Fatalf("tracked file must not be in the carried list:\n%s", state)
	}
}

func TestNewNoCarryAndExtraCarryFlags(t *testing.T) {
	repo := newRepo(t)
	write(t, repo, "notes.local", "scratch\n")

	mustRun(t, repo, "new", "bare", "--no-carry")
	if _, err := os.Stat(filepath.Join(taskPath(repo, "bare"), ".env")); !os.IsNotExist(err) {
		t.Fatal("--no-carry must not copy .env")
	}

	out := mustRun(t, repo, "new", "extra", "--carry", "*.local")
	if !strings.Contains(out, "notes.local") || !strings.Contains(out, ".env") {
		t.Fatalf("--carry must extend the defaults, not replace them:\n%s", out)
	}
	if read(t, taskPath(repo, "extra"), "notes.local") != "scratch\n" {
		t.Fatal("extra pattern file not carried")
	}
}

func TestNewRejectsBadInput(t *testing.T) {
	repo := newRepo(t)
	if code, _, errS := run(t, repo, "new", "bad name"); code != ExitUsage || !strings.Contains(errS, "not allowed") {
		t.Fatalf("invalid name: code=%d stderr=%q", code, errS)
	}
	mustRun(t, repo, "new", "taken")
	if code, _, errS := run(t, repo, "new", "taken"); code != ExitErr || !strings.Contains(errS, "already exists") {
		t.Fatalf("duplicate task: code=%d stderr=%q", code, errS)
	}
	git(t, repo, "branch", "copse/oops")
	if code, _, errS := run(t, repo, "new", "oops"); code != ExitErr || !strings.Contains(errS, "branch") {
		t.Fatalf("existing branch: code=%d stderr=%q", code, errS)
	}
}

func TestNewInEmptyRepoFailsCleanly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fresh")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "-b", "main")
	code, _, errS := run(t, dir, "new", "first")
	if code != ExitErr || !strings.Contains(errS, "does not resolve") {
		t.Fatalf("empty repo must fail with guidance: code=%d stderr=%q", code, errS)
	}
}

func TestNewPorcelainMatchesPathCommand(t *testing.T) {
	repo := newRepo(t)
	out := mustRun(t, repo, "new", "scripted", "--porcelain")
	if strings.Count(out, "\n") != 1 || !filepath.IsAbs(strings.TrimSpace(out)) {
		t.Fatalf("--porcelain must print exactly one absolute path: %q", out)
	}
	pathOut := mustRun(t, repo, "path", "scripted")
	if pathOut != out {
		t.Fatalf("path output %q != porcelain output %q", pathOut, out)
	}
	if code, _, errS := run(t, repo, "path", "ghost"); code != ExitErr || !strings.Contains(errS, "no task named") {
		t.Fatalf("unknown task path: code=%d stderr=%q", code, errS)
	}
}

func TestNewCustomBranchFromAndNote(t *testing.T) {
	repo := newRepo(t)
	git(t, repo, "branch", "release-1.x")
	// Flags after the positional name must parse too.
	out := mustRun(t, repo, "new", "hotfix", "--branch", "fix/oob-read", "--from", "release-1.x", "--note", "CVE backport")
	if !strings.Contains(out, "fix/oob-read") || !strings.Contains(out, "from release-1.x") {
		t.Fatalf("custom branch/from ignored:\n%s", out)
	}
	ls := mustRun(t, repo, "ls")
	if !strings.Contains(ls, "└─ CVE backport") {
		t.Fatalf("note not shown by ls:\n%s", ls)
	}
}

func TestNewHonorsGitConfig(t *testing.T) {
	repo := newRepo(t)
	git(t, repo, "config", "copse.root", ".worktrees")
	git(t, repo, "config", "copse.branchprefix", "wip/")
	git(t, repo, "config", "--add", "copse.carry", "secrets/*.key")
	write(t, repo, "secrets/dev.key", "k1\n")

	out := mustRun(t, repo, "new", "cfg")
	wt := filepath.Join(repo, ".worktrees", "cfg")
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("copse.root not honored: %v", err)
	}
	if !strings.Contains(out, "wip/cfg") {
		t.Fatalf("copse.branchprefix not honored:\n%s", out)
	}
	// Configured carry patterns replace the defaults entirely.
	if read(t, wt, "secrets/dev.key") != "k1\n" {
		t.Fatal("configured carry pattern not applied")
	}
	if _, err := os.Stat(filepath.Join(wt, ".env")); !os.IsNotExist(err) {
		t.Fatal("copse.carry must replace the default patterns")
	}
}

func TestLsTableShowsFreshThenActiveTask(t *testing.T) {
	repo := newRepo(t)
	mustRun(t, repo, "new", "rate-limit")
	out := mustRun(t, repo, "ls")
	if !strings.Contains(out, "copse — 1 task in demo (base: main)") {
		t.Fatalf("header wrong:\n%s", out)
	}
	// No commits yet: the task is "fresh", never "merged" or "active".
	for _, want := range []string{"NAME", "STATE", "BRANCH", "PATH", "rate-limit", "fresh", "copse/rate-limit"} {
		if !strings.Contains(out, want) {
			t.Fatalf("ls output missing %q:\n%s", want, out)
		}
	}
	// Paths are shown relative to the repo root.
	if !strings.Contains(out, filepath.Join("..", "demo.copse", "rate-limit")) {
		t.Fatalf("relative path missing:\n%s", out)
	}
	// First commit flips it to active.
	commitIn(t, taskPath(repo, "rate-limit"), "mw.go", "package app\n", "start work")
	out = mustRun(t, repo, "ls")
	if !strings.Contains(out, "active") || strings.Contains(out, "fresh") {
		t.Fatalf("task with commits must be active:\n%s", out)
	}
}

func TestLsJSONEnvelope(t *testing.T) {
	repo := newRepo(t)
	mustRun(t, repo, "new", "rate-limit", "--note", "429 retry")
	out := mustRun(t, repo, "ls", "--format", "json")
	var doc struct {
		Tool   string `json:"tool"`
		Schema int    `json:"schema_version"`
		Base   string `json:"base"`
		Tasks  []struct {
			Name    string   `json:"name"`
			Branch  string   `json:"branch"`
			Path    string   `json:"path"`
			State   string   `json:"state"`
			Dirty   bool     `json:"dirty"`
			Ahead   int      `json:"ahead"`
			Note    string   `json:"note"`
			Carried []string `json:"carried"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if doc.Tool != "copse" || doc.Schema != 1 || doc.Base != "main" || len(doc.Tasks) != 1 {
		t.Fatalf("envelope wrong: %+v", doc)
	}
	tk := doc.Tasks[0]
	if tk.Name != "rate-limit" || tk.State != "fresh" || tk.Ahead != 0 ||
		tk.Note != "429 retry" || !filepath.IsAbs(tk.Path) || len(tk.Carried) != 1 {
		t.Fatalf("task fields wrong: %+v", tk)
	}
}

func TestLsDirtyDetection(t *testing.T) {
	repo := newRepo(t)
	// notes.local is carried but NOT gitignored: it shows up as untracked
	// in the worktree, and still must not count as dirty.
	write(t, repo, "notes.local", "scratch\n")
	mustRun(t, repo, "new", "clean-task", "--carry", "*.local")
	out := mustRun(t, repo, "ls")
	if !strings.Contains(out, "-") || strings.Contains(strings.Split(out, "clean-task")[1], "yes") {
		t.Fatalf("carried files must not mark the task dirty:\n%s", out)
	}
	// A real tracked modification does.
	write(t, taskPath(repo, "clean-task"), "app.go", "package app // changed\n")
	out = mustRun(t, repo, "ls")
	row := strings.SplitN(out, "clean-task", 2)[1]
	if !strings.Contains(strings.SplitN(row, "\n", 2)[0], "yes") {
		t.Fatalf("tracked modification must mark the task dirty:\n%s", out)
	}
}

func TestLsShowsMergedAndMissingStates(t *testing.T) {
	repo := newRepo(t)
	mustRun(t, repo, "new", "landed")
	commitIn(t, taskPath(repo, "landed"), "feature.go", "package app\n", "add feature")
	git(t, repo, "merge", "-q", "--no-ff", "-m", "merge landed", "copse/landed")

	mustRun(t, repo, "new", "lost")
	if err := os.RemoveAll(taskPath(repo, "lost")); err != nil {
		t.Fatal(err)
	}

	out := mustRun(t, repo, "ls")
	landedRow := strings.SplitN(strings.SplitN(out, "landed", 2)[1], "\n", 2)[0]
	if !strings.Contains(landedRow, "merged") {
		t.Fatalf("merged task not labelled:\n%s", out)
	}
	lostRow := strings.SplitN(strings.SplitN(out, "lost", 2)[1], "\n", 2)[0]
	if !strings.Contains(lostRow, "missing") {
		t.Fatalf("missing worktree not labelled:\n%s", out)
	}
}

func TestLsIsDefaultCommandAndCountsUnmanaged(t *testing.T) {
	repo := newRepo(t)
	mustRun(t, repo, "new", "mine")
	// A worktree created behind copse's back is reported, not hidden.
	git(t, repo, "worktree", "add", "-q", "-b", "handmade", filepath.Join(filepath.Dir(repo), "handmade-wt"), "main")
	out := mustRun(t, repo) // no subcommand at all
	if !strings.Contains(out, "mine") {
		t.Fatalf("bare copse must default to ls:\n%s", out)
	}
	if !strings.Contains(out, "1 unmanaged worktree") {
		t.Fatalf("unmanaged worktree not counted:\n%s", out)
	}
}

func TestEnvSyncAndCheckRoundTrip(t *testing.T) {
	repo := newRepo(t)
	mustRun(t, repo, "new", "svc")

	// Drift: the main .env changes after the task was created.
	write(t, repo, ".env", "API_URL=http://127.0.0.1:8080\nTOKEN=rotated\n")
	code, out, _ := run(t, repo, "env", "--check", "svc")
	if code != ExitDrift || !strings.Contains(out, "stale") {
		t.Fatalf("stale drift must exit %d with a stale line: code=%d\n%s", ExitDrift, code, out)
	}

	// Deleting the task copy is the other drift kind.
	if err := os.Remove(filepath.Join(taskPath(repo, "svc"), ".env")); err != nil {
		t.Fatal(err)
	}
	code, out, _ = run(t, repo, "env", "--check", "svc")
	if code != ExitDrift || !strings.Contains(out, "missing") {
		t.Fatalf("missing drift not reported: code=%d\n%s", code, out)
	}

	// Sync repairs both, and a fresh check is clean.
	out = mustRun(t, repo, "env", "svc")
	if !strings.Contains(out, "copy") {
		t.Fatalf("sync output wrong:\n%s", out)
	}
	if read(t, taskPath(repo, "svc"), ".env") != read(t, repo, ".env") {
		t.Fatal("sync must copy the rotated content")
	}
	code, out, _ = run(t, repo, "env", "--check", "svc")
	if code != ExitOK || !strings.Contains(out, "in sync") {
		t.Fatalf("clean check must exit 0: code=%d\n%s", code, out)
	}
}

func TestEnvAllAndErrors(t *testing.T) {
	repo := newRepo(t)
	mustRun(t, repo, "new", "a")
	mustRun(t, repo, "new", "b")
	write(t, repo, ".env", "TOKEN=rotated-again\n")
	out := mustRun(t, repo, "env", "--all")
	if !strings.Contains(out, "env a —") || !strings.Contains(out, "env b —") {
		t.Fatalf("--all must report every task:\n%s", out)
	}
	if read(t, taskPath(repo, "b"), ".env") != "TOKEN=rotated-again\n" {
		t.Fatal("--all must sync every task")
	}
	if code, _, _ := run(t, repo, "env", "ghost"); code != ExitErr {
		t.Fatalf("unknown task must exit %d, got %d", ExitErr, code)
	}
	if code, _, _ := run(t, repo, "env"); code != ExitUsage {
		t.Fatalf("env without target must exit %d, got %d", ExitUsage, code)
	}
	if code, _, _ := run(t, repo, "env", "--all", "a"); code != ExitUsage {
		t.Fatalf("env --all plus a name must exit %d, got %d", ExitUsage, code)
	}
}

func TestRmRemovesWorktreeBranchAndState(t *testing.T) {
	repo := newRepo(t)
	mustRun(t, repo, "new", "done")
	out := mustRun(t, repo, "rm", "done")
	if !strings.Contains(out, "removed task done") || !strings.Contains(out, "deleted") {
		t.Fatalf("rm output wrong:\n%s", out)
	}
	if _, err := os.Stat(taskPath(repo, "done")); !os.IsNotExist(err) {
		t.Fatal("worktree directory must be gone")
	}
	if strings.Contains(git(t, repo, "branch", "--list", "copse/done"), "copse/done") {
		t.Fatal("branch must be deleted")
	}
	if strings.Contains(read(t, repo, ".git/copse/tasks.json"), "done") {
		t.Fatal("state entry must be removed")
	}
	if code, _, _ := run(t, repo, "rm", "done"); code != ExitErr {
		t.Fatal("second rm must fail: task no longer exists")
	}
}

func TestRmSafetyRails(t *testing.T) {
	repo := newRepo(t)
	mustRun(t, repo, "new", "wip")
	wt := taskPath(repo, "wip")

	// Uncommitted changes block first.
	write(t, wt, "app.go", "package app // half-done\n")
	if code, _, errS := run(t, repo, "rm", "wip"); code != ExitErr || !strings.Contains(errS, "uncommitted") {
		t.Fatalf("dirty rm: code=%d stderr=%q", code, errS)
	}
	git(t, wt, "add", "-A")
	git(t, wt, "commit", "-q", "-m", "wip commit")

	// Then unmerged commits block.
	code, _, errS := run(t, repo, "rm", "wip")
	if code != ExitErr || !strings.Contains(errS, "1 commit not in main") {
		t.Fatalf("unmerged rm: code=%d stderr=%q", code, errS)
	}

	// --force overrides both rails.
	mustRun(t, repo, "rm", "wip", "--force")
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("--force rm must remove the worktree")
	}
}

func TestRmKeepBranch(t *testing.T) {
	repo := newRepo(t)
	mustRun(t, repo, "new", "parked")
	commitIn(t, taskPath(repo, "parked"), "wip.go", "package app\n", "parked work")
	out := mustRun(t, repo, "rm", "parked", "--keep-branch")
	if !strings.Contains(out, "kept") {
		t.Fatalf("rm --keep-branch output wrong:\n%s", out)
	}
	if !strings.Contains(git(t, repo, "branch", "--list", "copse/parked"), "copse/parked") {
		t.Fatal("--keep-branch must preserve the branch")
	}
}

func TestPruneDryRunPreviewsWithoutChanges(t *testing.T) {
	repo := newRepo(t)
	mustRun(t, repo, "new", "landed")
	commitIn(t, taskPath(repo, "landed"), "f.go", "package app\n", "feature")
	git(t, repo, "merge", "-q", "--no-ff", "-m", "merge", "copse/landed")

	out := mustRun(t, repo, "prune", "--dry-run")
	if !strings.Contains(out, "would prune") || !strings.Contains(out, "landed") {
		t.Fatalf("dry-run preview wrong:\n%s", out)
	}
	if _, err := os.Stat(taskPath(repo, "landed")); err != nil {
		t.Fatal("dry-run must not remove anything")
	}
	if !strings.Contains(git(t, repo, "branch", "--list", "copse/landed"), "copse/landed") {
		t.Fatal("dry-run must not delete branches")
	}
}

func TestPruneRemovesMergedTask(t *testing.T) {
	repo := newRepo(t)
	mustRun(t, repo, "new", "landed")
	commitIn(t, taskPath(repo, "landed"), "f.go", "package app\n", "feature")
	git(t, repo, "merge", "-q", "--no-ff", "-m", "merge", "copse/landed")
	mustRun(t, repo, "new", "flight") // fresh, stays

	out := mustRun(t, repo, "prune")
	if !strings.Contains(out, "merged into main (ancestor)") {
		t.Fatalf("prune must state its evidence:\n%s", out)
	}
	if !strings.Contains(out, "no commits yet") {
		t.Fatalf("fresh task must be kept with its reason:\n%s", out)
	}
	if !strings.Contains(out, "pruned 1, kept 1, skipped 0") {
		t.Fatalf("summary wrong:\n%s", out)
	}
	if _, err := os.Stat(taskPath(repo, "landed")); !os.IsNotExist(err) {
		t.Fatal("merged worktree must be removed")
	}
	if strings.Contains(git(t, repo, "branch", "--list", "copse/landed"), "copse/landed") {
		t.Fatal("merged branch must be deleted")
	}
	if _, err := os.Stat(taskPath(repo, "flight")); err != nil {
		t.Fatal("unmerged task must survive")
	}
}

func TestPruneDetectsSquashMerge(t *testing.T) {
	repo := newRepo(t)
	mustRun(t, repo, "new", "squashed")
	wt := taskPath(repo, "squashed")
	commitIn(t, wt, "a.go", "package app // a\n", "part 1")
	commitIn(t, wt, "b.go", "package app // b\n", "part 2")
	// GitHub-style squash merge: one new commit on main, branch untouched.
	git(t, repo, "merge", "-q", "--squash", "copse/squashed")
	git(t, repo, "commit", "-q", "-m", "squashed feature (#42)")

	out := mustRun(t, repo, "prune")
	if !strings.Contains(out, "merged into main (squash)") {
		t.Fatalf("squash merge not detected:\n%s", out)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("squash-merged worktree must be removed")
	}
}

func TestPruneDetectsRebasedBranch(t *testing.T) {
	repo := newRepo(t)
	mustRun(t, repo, "new", "picked")
	wt := taskPath(repo, "picked")
	commitIn(t, wt, "x.go", "package app // x\n", "pick me 1")
	commitIn(t, wt, "y.go", "package app // y\n", "pick me 2")
	// Diverge main first so the picked commits get new hashes, then land
	// each branch commit as a patch-equivalent copy (rebase-merge shape).
	commitIn(t, repo, "README.md", "# demo\n", "unrelated mainline work")
	git(t, repo, "cherry-pick", "copse/picked~1", "copse/picked")

	out := mustRun(t, repo, "prune")
	if !strings.Contains(out, "every commit patch-equivalent") {
		t.Fatalf("rebase merge not detected:\n%s", out)
	}
}

func TestPruneKeepsUnmergedAndSkipsDirty(t *testing.T) {
	repo := newRepo(t)
	mustRun(t, repo, "new", "active-work")
	commitIn(t, taskPath(repo, "active-work"), "w.go", "package app\n", "unfinished")

	mustRun(t, repo, "new", "merged-dirty")
	wtDirty := taskPath(repo, "merged-dirty")
	commitIn(t, wtDirty, "d.go", "package app\n", "landed")
	git(t, repo, "merge", "-q", "--no-ff", "-m", "merge", "copse/merged-dirty")
	write(t, wtDirty, "d.go", "package app // local tweak\n")

	out := mustRun(t, repo, "prune")
	if !strings.Contains(out, "keep") || !strings.Contains(out, "1 commit ahead of main") {
		t.Fatalf("unmerged task must be kept with a reason:\n%s", out)
	}
	if !strings.Contains(out, "skip") || !strings.Contains(out, "use --force") {
		t.Fatalf("dirty merged task must be skipped with guidance:\n%s", out)
	}
	if _, err := os.Stat(wtDirty); err != nil {
		t.Fatal("skipped worktree must still exist")
	}

	out = mustRun(t, repo, "prune", "--force")
	if !strings.Contains(out, "pruned 1") {
		t.Fatalf("--force must prune the dirty merged task:\n%s", out)
	}
	if _, err := os.Stat(wtDirty); !os.IsNotExist(err) {
		t.Fatal("--force prune must remove the dirty worktree")
	}
}

func TestPruneGoneRequiresOptIn(t *testing.T) {
	repo := newRepo(t)
	// A local bare "remote" keeps everything offline.
	remote := filepath.Join(t.TempDir(), "remote.git")
	git(t, repo, "init", "-q", "--bare", remote)
	git(t, repo, "remote", "add", "origin", remote)

	mustRun(t, repo, "new", "ship-it")
	commitIn(t, taskPath(repo, "ship-it"), "s.go", "package app\n", "shipped elsewhere")
	git(t, repo, "push", "-q", "origin", "copse/ship-it")
	git(t, repo, "branch", "--set-upstream-to=origin/copse/ship-it", "copse/ship-it")
	git(t, repo, "push", "-q", "origin", "--delete", "copse/ship-it")
	git(t, repo, "fetch", "-q", "--prune", "origin")

	out := mustRun(t, repo, "prune")
	if !strings.Contains(out, "keep") || strings.Contains(out, "pruned 1") {
		t.Fatalf("gone upstream must not prune by default:\n%s", out)
	}
	out = mustRun(t, repo, "prune", "--gone")
	if !strings.Contains(out, "upstream branch gone") || !strings.Contains(out, "pruned 1") {
		t.Fatalf("--gone must prune deleted-upstream tasks:\n%s", out)
	}
}

func TestPruneDropsMissingWorktreeEntry(t *testing.T) {
	repo := newRepo(t)
	mustRun(t, repo, "new", "landed")
	commitIn(t, taskPath(repo, "landed"), "f.go", "package app\n", "feature")
	git(t, repo, "merge", "-q", "--no-ff", "-m", "merge", "copse/landed")
	// Someone rm -rf'd the worktree by hand.
	if err := os.RemoveAll(taskPath(repo, "landed")); err != nil {
		t.Fatal(err)
	}
	out := mustRun(t, repo, "prune")
	if !strings.Contains(out, "drop") || !strings.Contains(out, "worktree already gone") {
		t.Fatalf("missing worktree must be dropped with a reason:\n%s", out)
	}
	if strings.Contains(read(t, repo, ".git/copse/tasks.json"), "landed") {
		t.Fatal("stale state entry must be dropped")
	}
	if strings.Contains(git(t, repo, "branch", "--list", "copse/landed"), "copse/landed") {
		t.Fatal("merged branch of a dropped task must be deleted")
	}
}

func TestPruneKeepBranchAndJSON(t *testing.T) {
	repo := newRepo(t)
	mustRun(t, repo, "new", "landed")
	commitIn(t, taskPath(repo, "landed"), "f.go", "package app\n", "feature")
	git(t, repo, "merge", "-q", "--no-ff", "-m", "merge", "copse/landed")

	out := mustRun(t, repo, "prune", "--keep-branch", "--format", "json")
	var doc struct {
		Tool    string `json:"tool"`
		DryRun  bool   `json:"dry_run"`
		Pruned  int    `json:"pruned"`
		Actions []struct {
			Action string `json:"action"`
			Name   string `json:"name"`
			Detail string `json:"detail"`
		} `json:"actions"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if doc.Tool != "copse" || doc.DryRun || doc.Pruned != 1 || doc.Actions[0].Action != "prune" {
		t.Fatalf("prune JSON wrong: %+v", doc)
	}
	if !strings.Contains(doc.Actions[0].Detail, "branch kept") {
		t.Fatalf("detail must mention the kept branch: %+v", doc.Actions[0])
	}
	if !strings.Contains(git(t, repo, "branch", "--list", "copse/landed"), "copse/landed") {
		t.Fatal("--keep-branch must preserve the branch")
	}
}

func TestTopLevelDispatch(t *testing.T) {
	repo := newRepo(t)
	if out := mustRun(t, repo, "version"); out != "copse 0.1.0\n" {
		t.Fatalf("version output %q", out)
	}
	help := mustRun(t, repo, "--help")
	for _, cmd := range []string{"new", "ls", "env", "rm", "prune", "path"} {
		if !strings.Contains(help, cmd) {
			t.Fatalf("help missing %q:\n%s", cmd, help)
		}
	}
	if code, _, _ := run(t, repo, "bogus"); code != ExitUsage {
		t.Fatalf("unknown command must exit %d", ExitUsage)
	}
	if code, _, _ := run(t, repo, "ls", "--bogus"); code != ExitUsage {
		t.Fatalf("unknown flag must exit %d", ExitUsage)
	}
	// Extra positionals must be rejected loudly, not with a silent exit 2.
	if code, _, errS := run(t, repo, "ls", "extra"); code != ExitUsage || !strings.Contains(errS, `unexpected argument "extra"`) {
		t.Fatalf("extra argument: code=%d stderr=%q", code, errS)
	}
	outside := t.TempDir()
	if code, _, errS := run(t, outside, "ls"); code != ExitErr || !strings.Contains(errS, "not inside a git repository") {
		t.Fatalf("outside a repo: code=%d stderr=%q", code, errS)
	}
}
