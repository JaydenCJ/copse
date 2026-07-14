// Package merged decides whether a task branch is safely disposable. The
// rules are pure functions over signals gathered from git, so every
// combination — including the awkward squash-merge shapes — is unit
// tested without a repository.
package merged

import "fmt"

// Signals is everything the verdict depends on, gathered by the CLI from
// one round of git plumbing calls.
type Signals struct {
	BranchExists     bool
	Fresh            bool // branch tip still equals its recorded start hash
	Ancestor         bool // branch tip reachable from base
	Ahead            int  // commits on branch that base lacks
	CherryNew        int  // `git cherry` "+" lines: no equivalent patch in base
	CherryEquivalent int  // `git cherry` "-" lines: patch already in base
	Squashed         bool // whole-branch diff already applied to base
	UpstreamGone     bool // configured upstream deleted on the remote
}

// Verdict says whether the task can be pruned, and why, in words meant
// for direct display.
type Verdict struct {
	Merged   bool // the branch's work is contained in base
	Prunable bool // prune may remove it (merged, or gone when opted in)
	Reason   string
}

// Evaluate applies the pruning rules in order of confidence: real
// ancestry first, then patch equivalence (rebase merges), then the
// squash probe. Upstream deletion comes last and only with includeGone,
// because a deleted remote branch does not prove the work ever landed.
func Evaluate(base string, s Signals, includeGone bool) Verdict {
	if !s.BranchExists {
		return Verdict{Prunable: true, Reason: "branch deleted"}
	}
	// A task nobody has committed to yet is technically an ancestor of
	// base, but pruning it would silently undo a `copse new` someone is
	// about to use. Fresh tasks are always kept.
	if s.Fresh {
		return Verdict{Reason: "no commits yet"}
	}
	if s.Ancestor {
		return Verdict{Merged: true, Prunable: true, Reason: fmt.Sprintf("merged into %s (ancestor)", base)}
	}
	if s.Ahead > 0 && s.CherryNew == 0 && s.CherryEquivalent > 0 {
		return Verdict{Merged: true, Prunable: true, Reason: fmt.Sprintf("merged into %s (every commit patch-equivalent)", base)}
	}
	if s.Squashed {
		return Verdict{Merged: true, Prunable: true, Reason: fmt.Sprintf("merged into %s (squash)", base)}
	}
	if s.UpstreamGone && includeGone {
		return Verdict{Prunable: true, Reason: "upstream branch gone"}
	}
	if s.Ahead == 1 {
		return Verdict{Reason: fmt.Sprintf("1 commit ahead of %s", base)}
	}
	return Verdict{Reason: fmt.Sprintf("%d commits ahead of %s", s.Ahead, base)}
}
