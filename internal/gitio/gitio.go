// Package gitio talks to the local git binary and parses its plumbing
// output. Parsers are pure functions over bytes so they can be tested
// against captured fixtures; only the thin Git runner shells out.
package gitio

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Git runs git commands inside Dir. The zero value runs in the process
// working directory.
type Git struct {
	Dir string
}

// probeIdentity pins a synthetic identity for plumbing commands that
// create unreferenced probe objects (see SquashedOnto), so they work in
// repositories where user.name is not configured and always produce the
// same object for the same tree.
var probeIdentity = []string{
	"GIT_AUTHOR_NAME=copse",
	"GIT_AUTHOR_EMAIL=copse@example.test",
	"GIT_COMMITTER_NAME=copse",
	"GIT_COMMITTER_EMAIL=copse@example.test",
	"GIT_AUTHOR_DATE=2026-01-01T00:00:00+00:00",
	"GIT_COMMITTER_DATE=2026-01-01T00:00:00+00:00",
}

// run executes git with hardened flags so user configuration (pagers,
// signature display) cannot change the output shape. It returns stdout,
// the exit code, and an error carrying the first stderr line.
func (g Git) run(extraEnv []string, args ...string) ([]byte, int, error) {
	full := append([]string{
		"-c", "core.pager=cat",
		"-c", "log.showSignature=false",
	}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = g.Dir
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		code := -1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.Bytes(), code, fmt.Errorf("git %s: %s", args[0], firstLine(msg))
	}
	return out.Bytes(), 0, nil
}

func (g Git) out(args ...string) ([]byte, error) {
	b, _, err := g.run(nil, args...)
	return b, err
}

func (g Git) text(args ...string) (string, error) {
	b, err := g.out(args...)
	return strings.TrimSpace(string(b)), err
}

// CommonDir returns the absolute path of the repository's common git
// directory (shared by every worktree), where copse keeps its state file.
func (g Git) CommonDir() (string, error) {
	out, err := g.text("rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(out) {
		base := g.Dir
		if base == "" {
			base = "."
		}
		abs, absErr := filepath.Abs(filepath.Join(base, out))
		if absErr != nil {
			return "", absErr
		}
		out = abs
	}
	return filepath.Clean(out), nil
}

// CurrentBranch returns the short name of the checked-out branch, or
// "HEAD" when detached.
func (g Git) CurrentBranch() (string, error) {
	return g.text("rev-parse", "--abbrev-ref", "HEAD")
}

// ShortHash abbreviates a ref to git's short hash form.
func (g Git) ShortHash(ref string) (string, error) {
	return g.text("rev-parse", "--short", ref)
}

// Hash resolves a ref to its full commit hash.
func (g Git) Hash(ref string) (string, error) {
	return g.text("rev-parse", ref+"^{commit}")
}

// HasLocalBranch reports whether refs/heads/<name> exists.
func (g Git) HasLocalBranch(name string) bool {
	_, code, err := g.run(nil, "rev-parse", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil && code == 0
}

// HasRef reports whether ref resolves to a commit (local branch, remote
// branch, tag, or raw hash alike).
func (g Git) HasRef(ref string) bool {
	_, code, err := g.run(nil, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return err == nil && code == 0
}

// ConfigGet returns the value of a config key and whether it is set.
func (g Git) ConfigGet(key string) (string, bool) {
	out, code, err := g.run(nil, "config", "--get", key)
	if err != nil || code != 0 {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// ConfigAll returns every value of a multi-valued config key.
func (g Git) ConfigAll(key string) []string {
	out, _, err := g.run(nil, "config", "--get-all", key)
	if err != nil {
		return nil
	}
	var vals []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			vals = append(vals, l)
		}
	}
	return vals
}

// WorktreesRaw returns `git worktree list --porcelain` output, ready for
// ParseWorktrees.
func (g Git) WorktreesRaw() ([]byte, error) {
	return g.out("worktree", "list", "--porcelain")
}

// AddWorktree creates a worktree at path on a new branch cut from start.
func (g Git) AddWorktree(path, newBranch, start string) error {
	_, _, err := g.run(nil, "worktree", "add", "-b", newBranch, path, start)
	return err
}

// RemoveWorktree unregisters and deletes a worktree directory. copse
// always performs its own dirty check first, then forces removal so that
// carried (untracked) env files cannot block it.
func (g Git) RemoveWorktree(path string) error {
	_, _, err := g.run(nil, "worktree", "remove", "--force", path)
	return err
}

// PruneWorktrees drops worktree registrations whose directories are gone.
func (g Git) PruneWorktrees() error {
	_, _, err := g.run(nil, "worktree", "prune")
	return err
}

// DeleteBranch force-deletes a local branch. Forcing is deliberate: git
// considers a squash-merged branch unmerged, while copse has already
// verified patch containment before calling this.
func (g Git) DeleteBranch(name string) error {
	_, _, err := g.run(nil, "branch", "-D", name)
	return err
}

// IsAncestor reports whether ancestor is reachable from descendant.
func (g Git) IsAncestor(ancestor, descendant string) (bool, error) {
	_, code, err := g.run(nil, "merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	if code == 1 {
		return false, nil
	}
	return false, err
}

// CherryRaw returns `git cherry <upstream> <head>` output, ready for
// ParseCherry.
func (g Git) CherryRaw(upstream, head string) ([]byte, error) {
	return g.out("cherry", upstream, head)
}

// SquashedOnto reports whether the entire diff of branch since it forked
// from base is already contained in base — the shape a squash merge
// leaves behind. It builds an unreferenced probe commit that squashes the
// branch into a single patch, then asks `git cherry` whether that patch
// is equivalent to something in base. The probe object is eventually
// garbage-collected by git; nothing in the repository is modified.
func (g Git) SquashedOnto(base, branch string) (bool, error) {
	mbRaw, code, err := g.run(nil, "merge-base", base, branch)
	if err != nil {
		if code == 1 {
			return false, nil // no common ancestor: unrelated histories
		}
		return false, err
	}
	mb := strings.TrimSpace(string(mbRaw))
	tree, err := g.text("rev-parse", branch+"^{tree}")
	if err != nil {
		return false, err
	}
	probeRaw, _, err := g.run(probeIdentity, "commit-tree", tree, "-p", mb, "-m", "copse squash probe")
	if err != nil {
		return false, err
	}
	probe := strings.TrimSpace(string(probeRaw))
	out, err := g.CherryRaw(base, probe)
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(strings.TrimSpace(string(out)), "- "), nil
}

// StatusRaw returns NUL-separated porcelain status for the worktree at
// Dir, with untracked files expanded one per entry, ready for ParseStatus.
func (g Git) StatusRaw() ([]byte, error) {
	return g.out("status", "--porcelain", "-z", "--untracked-files=all")
}

// AheadBehind counts how many commits branch is ahead of and behind base.
func (g Git) AheadBehind(base, branch string) (ahead, behind int, err error) {
	out, err := g.text("rev-list", "--left-right", "--count", base+"..."+branch)
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("git rev-list: unexpected output %q", out)
	}
	behind, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, fmt.Errorf("git rev-list: unexpected output %q", out)
	}
	ahead, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("git rev-list: unexpected output %q", out)
	}
	return ahead, behind, nil
}

// UpstreamGone reports whether branch has a configured upstream that no
// longer exists (the state `git branch -vv` renders as "[gone]").
func (g Git) UpstreamGone(branch string) (bool, error) {
	out, err := g.text("for-each-ref",
		"--format=%(upstream:short)\x1f%(upstream:track)",
		"refs/heads/"+branch)
	if err != nil {
		return false, err
	}
	return ParseUpstreamGone(out), nil
}

// DefaultBranch guesses the integration branch: origin/HEAD if it points
// at an existing local branch, then main, then master, then whatever is
// currently checked out.
func (g Git) DefaultBranch() string {
	if out, err := g.text("symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil && out != "" {
		if name := strings.TrimPrefix(out, "origin/"); g.HasLocalBranch(name) {
			return name
		}
	}
	for _, name := range []string{"main", "master"} {
		if g.HasLocalBranch(name) {
			return name
		}
	}
	if br, err := g.CurrentBranch(); err == nil && br != "" && br != "HEAD" {
		return br
	}
	return "main"
}

// LsFiles returns the set of tracked paths, relative to the worktree root.
func (g Git) LsFiles() (map[string]bool, error) {
	out, err := g.out("ls-files", "-z")
	if err != nil {
		return nil, err
	}
	tracked := make(map[string]bool)
	for _, p := range bytes.Split(out, []byte{0}) {
		if len(p) > 0 {
			tracked[string(p)] = true
		}
	}
	return tracked, nil
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}
