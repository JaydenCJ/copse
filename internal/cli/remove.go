package cli

import (
	"flag"
	"fmt"
	"io"
)

// runRemove deletes one task deliberately: worktree, branch, and state.
// Unlike prune it does not require the branch to be merged, but it
// refuses to destroy unmerged commits or uncommitted changes without
// --force.
func runRemove(dir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("rm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	force := fs.Bool("force", false, "remove even when dirty or unmerged")
	keepBranch := fs.Bool("keep-branch", false, "keep the branch; remove only worktree and state")
	pos, err := parseArgs(fs, args, 1)
	if err != nil {
		return ExitUsage
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, "copse rm: exactly one task name required")
		return ExitUsage
	}
	ws, err := open(dir)
	if err != nil {
		fmt.Fprintf(stderr, "copse: %v\n", err)
		return ExitErr
	}
	t := ws.state.Find(pos[0])
	if t == nil {
		fmt.Fprintf(stderr, "copse: no task named %q (see copse ls)\n", pos[0])
		return ExitErr
	}
	info, err := ws.inspect(*t, "", true)
	if err != nil {
		fmt.Fprintf(stderr, "copse rm: %v\n", err)
		return ExitErr
	}
	if len(info.dirty) > 0 && !*force {
		fmt.Fprintf(stderr, "copse rm: %s has uncommitted changes (%s) — commit them or pass --force\n",
			t.Name, plural(len(info.dirty), "path"))
		return ExitErr
	}
	if info.branch && !info.fresh && !info.verdict.Merged && info.ahead > 0 && !*force && !*keepBranch {
		fmt.Fprintf(stderr, "copse rm: branch %s has %s not in %s — pass --keep-branch to keep it or --force to delete anyway\n",
			t.Branch, plural(info.ahead, "commit"), info.base)
		return ExitErr
	}
	if info.present {
		if err := ws.git.RemoveWorktree(t.Path); err != nil {
			fmt.Fprintf(stderr, "copse rm: %v\n", err)
			return ExitErr
		}
	}
	// Drop stale registrations (hand-deleted directories) before touching
	// the branch: git refuses to delete a branch a worktree still claims.
	_ = ws.git.PruneWorktrees()
	branchNote := "kept"
	if info.branch && !*keepBranch {
		if err := ws.git.DeleteBranch(t.Branch); err != nil {
			fmt.Fprintf(stderr, "copse rm: %v\n", err)
			return ExitErr
		}
		branchNote = "deleted"
	}
	name, branch, path := t.Name, t.Branch, t.Path
	ws.state.Remove(name)
	if err := ws.state.Save(ws.statePath); err != nil {
		fmt.Fprintf(stderr, "copse rm: %v\n", err)
		return ExitErr
	}
	fmt.Fprintf(stdout, "removed task %s\n", name)
	fmt.Fprintf(stdout, "  worktree  %s (removed)\n", displayPath(ws.top, path))
	fmt.Fprintf(stdout, "  branch    %s (%s)\n", branch, branchNote)
	return ExitOK
}
