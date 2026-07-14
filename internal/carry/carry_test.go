// Tests for env-file discovery and the copy engine. All on real temp
// directories — no git needed, because Discover takes the tracked set as
// plain data.
package carry

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

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

func TestDiscoverFindsEnvFilesAtAnyDepth(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".env", "A=1")
	write(t, root, "services/api/.env.local", "B=2")
	write(t, root, "main.go", "package main")
	got, err := Discover(root, DefaultPatterns(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".env", "services/api/.env.local"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDiscoverSkipsTrackedFiles(t *testing.T) {
	// .env.example is usually committed; carrying it would shadow the
	// checkout's own copy for no benefit.
	root := t.TempDir()
	write(t, root, ".env", "A=1")
	write(t, root, ".env.example", "A=fill-me-in")
	got, err := Discover(root, DefaultPatterns(), map[string]bool{".env.example": true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{".env"}) {
		t.Fatalf("tracked file leaked into carry set: %v", got)
	}
}

func TestDiscoverSkipsGitDirectory(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".git/info/.env", "SNEAKY=1")
	write(t, root, ".env", "A=1")
	got, err := Discover(root, DefaultPatterns(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{".env"}) {
		t.Fatalf(".git contents must never be carried: %v", got)
	}
}

func TestDiscoverSkipsNestedRepositoriesAndWorktrees(t *testing.T) {
	// A subdirectory holding its own .git (dir for submodules, file for
	// linked worktrees) belongs to a different checkout.
	root := t.TempDir()
	write(t, root, ".env", "A=1")
	write(t, root, "vendor/lib/.git/config", "[core]")
	write(t, root, "vendor/lib/.env", "NOT_OURS=1")
	write(t, root, "wt/task/.git", "gitdir: /elsewhere") // worktree-style .git file
	write(t, root, "wt/task/.env", "NOT_OURS=2")
	got, err := Discover(root, DefaultPatterns(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{".env"}) {
		t.Fatalf("nested checkouts must be skipped: %v", got)
	}
}

func TestDiscoverSkipsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation not portable on windows CI-less runs")
	}
	root := t.TempDir()
	write(t, root, "real.txt", "X=1")
	if err := os.Symlink(filepath.Join(root, "real.txt"), filepath.Join(root, ".env")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	got, err := Discover(root, DefaultPatterns(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("symlinks must never be carried: %v", got)
	}
}

func TestDiscoverCustomPatterns(t *testing.T) {
	root := t.TempDir()
	write(t, root, "secrets/dev.key", "k")
	write(t, root, ".env", "A=1")
	got, err := Discover(root, []string{"secrets/*.key"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"secrets/dev.key"}) {
		t.Fatalf("custom patterns replace defaults: %v", got)
	}
}

func TestDiscoverReturnsSortedPaths(t *testing.T) {
	root := t.TempDir()
	write(t, root, "z/.env", "1")
	write(t, root, "a/.env", "2")
	write(t, root, ".env", "3")
	got, err := Discover(root, DefaultPatterns(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".env", "a/.env", "z/.env"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("results must be sorted for deterministic output: %v", got)
	}
}

func TestSyncClassifiesCopyUpdateSame(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	write(t, src, ".env", "A=1")
	write(t, src, ".env.test", "B=2")
	write(t, dst, ".env.test", "B=OLD")

	results, err := Sync(src, dst, []string{".env", ".env.test"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Result{{".env", ActionCopy}, {".env.test", ActionUpdate}}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("got %+v, want %+v", results, want)
	}
	if read(t, dst, ".env.test") != "B=2" {
		t.Fatal("update must overwrite stale content")
	}

	// Second run: everything identical.
	results, err = Sync(src, dst, []string{".env", ".env.test"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Action != ActionSame {
			t.Fatalf("re-sync of identical files must be %q, got %+v", ActionSame, results)
		}
	}
}

func TestSyncCreatesParentDirectories(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	write(t, src, "services/api/.env.local", "PORT=3001")
	if _, err := Sync(src, dst, []string{"services/api/.env.local"}); err != nil {
		t.Fatal(err)
	}
	if read(t, dst, "services/api/.env.local") != "PORT=3001" {
		t.Fatal("nested carry must create parent directories")
	}
}

func TestSyncPreservesExecutableBit(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	write(t, src, ".env", "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(src, ".env"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(src, dst, []string{".env"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dst, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("executable bit lost: %v", info.Mode())
	}
}

func TestCheckReportsOKStaleMissing(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	write(t, src, ".env", "A=1")
	write(t, src, ".env.stale", "B=2")
	write(t, src, ".env.gone", "C=3")
	write(t, dst, ".env", "A=1")
	write(t, dst, ".env.stale", "B=OLD")

	results, drift, err := Check(src, dst, []string{".env", ".env.gone", ".env.stale"})
	if err != nil {
		t.Fatal(err)
	}
	want := []CheckResult{
		{".env", DriftOK},
		{".env.gone", DriftMissing},
		{".env.stale", DriftStale},
	}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("got %+v, want %+v", results, want)
	}
	if drift != 2 {
		t.Fatalf("drift count = %d, want 2", drift)
	}
	// Check mode must never create files, not even the missing one.
	if _, err := os.Stat(filepath.Join(dst, ".env.gone")); !os.IsNotExist(err) {
		t.Fatal("check mode must never create files")
	}
	// And an empty list is a successful no-op for both modes.
	if results, err := Sync(src, dst, nil); err != nil || len(results) != 0 {
		t.Fatalf("empty sync must succeed with no results: %v %v", results, err)
	}
}
