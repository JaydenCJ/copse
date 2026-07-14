// Tests for the pure parsers over git plumbing output. Each fixture
// reproduces the exact byte shape git emits — a parser that drifts from
// git's format silently misclassifies worktrees, which is the worst
// failure mode a lifecycle tool can have.
package gitio

import (
	"reflect"
	"testing"
)

func TestParseWorktreesMainAndLinked(t *testing.T) {
	raw := []byte("worktree /work/demo\nHEAD 1111111111111111111111111111111111111111\nbranch refs/heads/main\n\n" +
		"worktree /work/demo.copse/rate-limit\nHEAD 2222222222222222222222222222222222222222\nbranch refs/heads/copse/rate-limit\n\n")
	wts := ParseWorktrees(raw)
	if len(wts) != 2 {
		t.Fatalf("got %d worktrees, want 2: %+v", len(wts), wts)
	}
	if wts[0].Path != "/work/demo" || wts[0].Branch != "main" {
		t.Fatalf("main worktree parsed wrong: %+v", wts[0])
	}
	if wts[1].Branch != "copse/rate-limit" || wts[1].Head == "" {
		t.Fatalf("linked worktree parsed wrong: %+v", wts[1])
	}
}

func TestParseWorktreesDetachedAndLocked(t *testing.T) {
	raw := []byte("worktree /work/demo\nHEAD abc\nbranch refs/heads/main\n\n" +
		"worktree /work/wt\nHEAD def\ndetached\nlocked being used by CI\n\n")
	wts := ParseWorktrees(raw)
	if len(wts) != 2 {
		t.Fatalf("got %d worktrees, want 2", len(wts))
	}
	wt := wts[1]
	if !wt.Detached || wt.Branch != "" {
		t.Fatalf("detached not recognized: %+v", wt)
	}
	if !wt.Locked || wt.LockReason != "being used by CI" {
		t.Fatalf("lock reason lost: %+v", wt)
	}
}

func TestParseWorktreesBareRepository(t *testing.T) {
	// A bare main entry has no HEAD/branch lines at all.
	raw := []byte("worktree /srv/git/demo.git\nbare\n\n")
	wts := ParseWorktrees(raw)
	if len(wts) != 1 || !wts[0].Bare {
		t.Fatalf("bare flag not recognized: %+v", wts)
	}
}

func TestParseWorktreesPrunableAndEmptyInput(t *testing.T) {
	raw := []byte("worktree /work/gone\nHEAD abc\nbranch refs/heads/x\nprunable gitdir file points to non-existent location\n\n")
	wts := ParseWorktrees(raw)
	if len(wts) != 1 || !wts[0].Prunable {
		t.Fatalf("prunable flag not recognized: %+v", wts)
	}
	if got := ParseWorktrees(nil); len(got) != 0 {
		t.Fatalf("empty input should parse to no worktrees, got %+v", got)
	}
}

func TestParseWorktreesMissingTrailingBlankLine(t *testing.T) {
	// Defensive: the final block must be flushed even without the
	// trailing blank line some git versions omit under -z conversions.
	raw := []byte("worktree /work/demo\nHEAD abc\nbranch refs/heads/main")
	wts := ParseWorktrees(raw)
	if len(wts) != 1 || wts[0].Branch != "main" {
		t.Fatalf("final block lost: %+v", wts)
	}
}

func TestParseCherryCounts(t *testing.T) {
	raw := []byte("- 1111111111111111111111111111111111111111\n+ 2222222222222222222222222222222222222222\n- 3333333333333333333333333333333333333333\n")
	c := ParseCherry(raw)
	if c.Equivalent != 2 || c.New != 1 {
		t.Fatalf("got %+v, want 2 equivalent / 1 new", c)
	}
}

func TestParseCherryEmptyAndGarbage(t *testing.T) {
	if c := ParseCherry(nil); c.Equivalent != 0 || c.New != 0 {
		t.Fatalf("empty cherry should count zero: %+v", c)
	}
	// Lines that are not marker lines (defensive) are ignored.
	if c := ParseCherry([]byte("warning: something\n")); c.Equivalent != 0 || c.New != 0 {
		t.Fatalf("non-marker lines must not count: %+v", c)
	}
}

func TestParseStatusSimpleEntries(t *testing.T) {
	raw := []byte(" M app.go\x00?? notes.txt\x00A  new.go\x00")
	entries := ParseStatus(raw)
	want := []StatusEntry{{" M", "app.go"}, {"??", "notes.txt"}, {"A ", "new.go"}}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("got %+v, want %+v", entries, want)
	}
}

func TestParseStatusRenameConsumesSourcePath(t *testing.T) {
	// Porcelain -z rename records are "R  new\0old\0"; the old path must
	// not be misread as a separate entry.
	raw := []byte("R  renamed.go\x00original.go\x00 M other.go\x00")
	entries := ParseStatus(raw)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(entries), entries)
	}
	if entries[0].Path != "renamed.go" || entries[1].Path != "other.go" {
		t.Fatalf("rename source leaked into entries: %+v", entries)
	}
}

func TestDirtyPathsExcludesCarriedUntracked(t *testing.T) {
	entries := []StatusEntry{
		{"??", ".env"},
		{"??", "notes.local"},
		{"??", "real-work.go"},
	}
	dirty := DirtyPaths(entries, []string{".env", "notes.local"})
	if len(dirty) != 1 || dirty[0] != "real-work.go" {
		t.Fatalf("carried files must not count as dirty: %v", dirty)
	}
}

func TestDirtyPathsCountsTrackedModifications(t *testing.T) {
	// A carried file that later gets *tracked and modified* still counts:
	// only the untracked "??" state is excused.
	entries := []StatusEntry{{" M", ".env"}}
	dirty := DirtyPaths(entries, []string{".env"})
	if len(dirty) != 1 {
		t.Fatalf("tracked modification must count as dirty: %v", dirty)
	}
}

func TestDirtyPathsIgnoredEntriesNeverBlock(t *testing.T) {
	entries := []StatusEntry{{"!!", "build.log"}}
	if dirty := DirtyPaths(entries, nil); len(dirty) != 0 {
		t.Fatalf("ignored entries must never block removal: %v", dirty)
	}
}

func TestParseUpstreamGone(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"origin/copse/x\x1f[gone]", true},
		{"origin/copse/x\x1f[ahead 1]", false},
		{"origin/copse/x\x1f", false}, // upstream exists, in sync
		{"\x1f", false},               // no upstream configured
		{"", false},                   // branch missing entirely
	}
	for _, c := range cases {
		if got := ParseUpstreamGone(c.line); got != c.want {
			t.Fatalf("ParseUpstreamGone(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}
