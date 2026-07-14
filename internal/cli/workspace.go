package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/JaydenCJ/copse/internal/carry"
	"github.com/JaydenCJ/copse/internal/gitio"
	"github.com/JaydenCJ/copse/internal/merged"
	"github.com/JaydenCJ/copse/internal/task"
)

// workspace bundles everything a command needs: a git handle rooted at
// the main worktree, the task state, the registered worktrees, and the
// effective configuration.
type workspace struct {
	git       gitio.Git
	top       string // main worktree root
	repo      string // base name of top, for display
	statePath string
	state     *task.State
	worktrees []gitio.Worktree
	cfg       config
}

// config is the effective copse configuration, resolved from git config
// with built-in defaults.
type config struct {
	root   string   // absolute directory that holds task worktrees
	prefix string   // branch name prefix
	base   string   // integration branch
	carry  []string // carry glob patterns
}

// open locates the repository around dir (or the process working
// directory when dir is empty) and loads state and configuration.
func open(dir string) (*workspace, error) {
	g := gitio.Git{Dir: dir}
	raw, err := g.WorktreesRaw()
	if err != nil {
		return nil, fmt.Errorf("not inside a git repository (%v)", err)
	}
	wts := gitio.ParseWorktrees(raw)
	if len(wts) == 0 {
		return nil, errors.New("git reported no worktrees")
	}
	if wts[0].Bare {
		return nil, errors.New("copse needs a checked-out main worktree; bare repositories are not supported")
	}
	top := wts[0].Path
	g = gitio.Git{Dir: top}
	common, err := g.CommonDir()
	if err != nil {
		return nil, err
	}
	statePath := filepath.Join(common, "copse", "tasks.json")
	st, err := task.Load(statePath)
	if err != nil {
		return nil, err
	}
	ws := &workspace{
		git:       g,
		top:       top,
		repo:      filepath.Base(top),
		statePath: statePath,
		state:     st,
		worktrees: wts,
	}
	ws.cfg = loadConfig(g, top)
	return ws, nil
}

func loadConfig(g gitio.Git, top string) config {
	c := config{prefix: "copse/", carry: carry.DefaultPatterns()}
	if v, ok := g.ConfigGet("copse.root"); ok && v != "" {
		if !filepath.IsAbs(v) {
			v = filepath.Join(top, v)
		}
		c.root = filepath.Clean(v)
	} else {
		c.root = filepath.Join(filepath.Dir(top), filepath.Base(top)+".copse")
	}
	if v, ok := g.ConfigGet("copse.branchprefix"); ok {
		c.prefix = v
	}
	if v, ok := g.ConfigGet("copse.base"); ok && v != "" {
		c.base = v
	} else {
		c.base = g.DefaultBranch()
	}
	if vs := g.ConfigAll("copse.carry"); len(vs) > 0 {
		c.carry = vs
	}
	return c
}

// registered reports whether path is a registered worktree.
func (ws *workspace) registered(path string) bool {
	for _, wt := range ws.worktrees {
		if filepath.Clean(wt.Path) == filepath.Clean(path) {
			return true
		}
	}
	return false
}

// unmanagedCount counts linked worktrees copse does not manage, so `ls`
// can point at them without pretending to own them.
func (ws *workspace) unmanagedCount() int {
	n := 0
	for _, wt := range ws.worktrees[1:] { // [0] is the main worktree
		managed := false
		for _, t := range ws.state.Tasks {
			if filepath.Clean(t.Path) == filepath.Clean(wt.Path) {
				managed = true
				break
			}
		}
		if !managed {
			n++
		}
	}
	return n
}

// taskInfo is everything ls/rm/prune need to know about one task.
type taskInfo struct {
	base    string   // resolved base branch used for the verdict
	present bool     // worktree registered and its directory exists
	branch  bool     // branch still exists
	fresh   bool     // branch tip still equals the recorded start hash
	ahead   int      // -1 when unknown
	behind  int      // -1 when unknown
	dirty   []string // paths that would block removal
	verdict merged.Verdict
}

// inspect gathers the merge/dirty signals for one task. includeGone
// controls whether a deleted upstream alone makes the task prunable.
func (ws *workspace) inspect(t task.Task, baseOverride string, includeGone bool) (taskInfo, error) {
	info := taskInfo{ahead: -1, behind: -1}
	base := baseOverride
	if base == "" {
		base = t.Base
	}
	if base == "" || !ws.git.HasRef(base) {
		base = ws.cfg.base
	}
	info.base = base
	_, statErr := os.Stat(t.Path)
	info.present = ws.registered(t.Path) && statErr == nil
	info.branch = ws.git.HasLocalBranch(t.Branch)
	sig := merged.Signals{BranchExists: info.branch}
	if info.branch && t.StartHash != "" {
		tip, err := ws.git.Hash(t.Branch)
		if err != nil {
			return info, err
		}
		info.fresh = tip == t.StartHash
		sig.Fresh = info.fresh
	}
	if info.branch && ws.git.HasRef(base) {
		anc, err := ws.git.IsAncestor(t.Branch, base)
		if err != nil {
			return info, err
		}
		sig.Ancestor = anc
		ahead, behind, err := ws.git.AheadBehind(base, t.Branch)
		if err != nil {
			return info, err
		}
		info.ahead, info.behind = ahead, behind
		sig.Ahead = ahead
		if !anc && ahead > 0 {
			raw, err := ws.git.CherryRaw(base, t.Branch)
			if err != nil {
				return info, err
			}
			ch := gitio.ParseCherry(raw)
			sig.CherryNew, sig.CherryEquivalent = ch.New, ch.Equivalent
			if sig.CherryNew > 0 {
				sq, err := ws.git.SquashedOnto(base, t.Branch)
				if err != nil {
					return info, err
				}
				sig.Squashed = sq
			}
		}
		gone, err := ws.git.UpstreamGone(t.Branch)
		if err != nil {
			return info, err
		}
		sig.UpstreamGone = gone
	}
	info.verdict = merged.Evaluate(base, sig, includeGone)
	if info.present {
		wtGit := gitio.Git{Dir: t.Path}
		rawStatus, err := wtGit.StatusRaw()
		if err != nil {
			return info, err
		}
		info.dirty = gitio.DirtyPaths(gitio.ParseStatus(rawStatus), t.Carried)
	}
	return info, nil
}

// stateLabel maps an inspection onto the STATE column of `copse ls`.
func (info taskInfo) stateLabel() string {
	switch {
	case !info.present:
		return "missing"
	case !info.branch:
		return "broken"
	case info.fresh:
		return "fresh"
	case info.verdict.Merged:
		return "merged"
	case info.verdict.Prunable:
		return "gone"
	default:
		return "active"
	}
}

// displayPath shortens an absolute worktree path relative to the repo
// root for text output; JSON always carries the absolute path.
func displayPath(top, p string) string {
	if rel, err := filepath.Rel(top, p); err == nil {
		return rel
	}
	return p
}

// plural formats "1 commit" / "3 commits" for simple -s words.
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
