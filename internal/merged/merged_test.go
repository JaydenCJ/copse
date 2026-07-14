// Tests for the prune verdict rules. Each case models a real merge shape
// — fast-forward, merge commit, rebase, squash, deleted upstream — since
// a wrong verdict either deletes live work or leaves dead worktrees
// around forever, the two failure modes copse exists to prevent.
package merged

import (
	"strings"
	"testing"
)

func TestAncestorBranchIsMerged(t *testing.T) {
	v := Evaluate("main", Signals{BranchExists: true, Ancestor: true}, false)
	if !v.Merged || !v.Prunable {
		t.Fatalf("ancestor branch must be merged+prunable: %+v", v)
	}
	if !strings.Contains(v.Reason, "ancestor") || !strings.Contains(v.Reason, "main") {
		t.Fatalf("reason must name the mechanism and base: %q", v.Reason)
	}
}

func TestFreshTaskIsNeverPruned(t *testing.T) {
	// Right after `copse new`, the branch tip IS an ancestor of base —
	// pruning on that alone would delete every just-created task.
	v := Evaluate("main", Signals{BranchExists: true, Fresh: true, Ancestor: true}, true)
	if v.Merged || v.Prunable {
		t.Fatalf("fresh task must be kept even though it is an ancestor: %+v", v)
	}
	if v.Reason != "no commits yet" {
		t.Fatalf("fresh reason wrong: %q", v.Reason)
	}
}

func TestDeletedBranchIsPrunableButNotMerged(t *testing.T) {
	v := Evaluate("main", Signals{BranchExists: false}, false)
	if v.Merged {
		t.Fatalf("a deleted branch proves nothing about merging: %+v", v)
	}
	if !v.Prunable || v.Reason != "branch deleted" {
		t.Fatalf("deleted branch must be prunable: %+v", v)
	}
}

func TestRebaseEquivalentCommitsAreMerged(t *testing.T) {
	// A rebase-merge leaves the branch non-ancestor but every commit
	// patch-equivalent ("-" in git cherry).
	v := Evaluate("main", Signals{BranchExists: true, Ahead: 3, CherryEquivalent: 3}, false)
	if !v.Merged || !strings.Contains(v.Reason, "patch-equivalent") {
		t.Fatalf("all-equivalent cherry must be merged: %+v", v)
	}
}

func TestMixedCherryIsNotMergedWithoutSquashProbe(t *testing.T) {
	// One commit landed via cherry-pick, one did not: pruning now would
	// destroy the unlanded commit.
	v := Evaluate("main", Signals{BranchExists: true, Ahead: 2, CherryEquivalent: 1, CherryNew: 1}, false)
	if v.Merged || v.Prunable {
		t.Fatalf("partially landed branch must be kept: %+v", v)
	}
}

func TestSquashProbeMakesBranchMerged(t *testing.T) {
	// Squash merges leave every individual commit "+" in cherry, but the
	// whole-branch probe proves the combined diff landed.
	v := Evaluate("main", Signals{BranchExists: true, Ahead: 4, CherryNew: 4, Squashed: true}, false)
	if !v.Merged || !strings.Contains(v.Reason, "squash") {
		t.Fatalf("squash-detected branch must be merged: %+v", v)
	}
}

func TestUpstreamGoneRequiresOptIn(t *testing.T) {
	s := Signals{BranchExists: true, Ahead: 2, CherryNew: 2, UpstreamGone: true}
	if v := Evaluate("main", s, false); v.Prunable {
		t.Fatalf("gone upstream must not prune without opt-in: %+v", v)
	}
	v := Evaluate("main", s, true)
	if !v.Prunable || v.Merged {
		t.Fatalf("with opt-in, gone is prunable but never claimed merged: %+v", v)
	}
	if v.Reason != "upstream branch gone" {
		t.Fatalf("reason must say why: %q", v.Reason)
	}
}

func TestAncestryOutranksGone(t *testing.T) {
	// When both hold, report the stronger, verifiable reason.
	v := Evaluate("main", Signals{BranchExists: true, Ancestor: true, UpstreamGone: true}, true)
	if !v.Merged || !strings.Contains(v.Reason, "ancestor") {
		t.Fatalf("ancestor evidence must win over gone: %+v", v)
	}
}

func TestActiveBranchReasonCountsCommits(t *testing.T) {
	v := Evaluate("main", Signals{BranchExists: true, Ahead: 1, CherryNew: 1}, false)
	if v.Prunable || v.Reason != "1 commit ahead of main" {
		t.Fatalf("singular reason wrong: %+v", v)
	}
	v = Evaluate("main", Signals{BranchExists: true, Ahead: 5, CherryNew: 5}, false)
	if v.Reason != "5 commits ahead of main" {
		t.Fatalf("plural reason wrong: %+v", v)
	}
}
