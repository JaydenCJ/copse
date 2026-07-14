package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/JaydenCJ/copse/internal/render"
	"github.com/JaydenCJ/copse/internal/task"
)

// runPrune removes every task whose branch has landed in the base:
// regular merges (ancestor), rebase merges (patch-equivalent commits),
// and squash merges (whole-branch diff contained). --gone extends it to
// tasks whose upstream branch was deleted on the remote.
func runPrune(dir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "show what would happen, change nothing")
	force := fs.Bool("force", false, "prune merged tasks even when their worktree is dirty")
	gone := fs.Bool("gone", false, "also prune tasks whose upstream branch was deleted")
	keepBranch := fs.Bool("keep-branch", false, "remove worktrees but keep the branches")
	baseFlag := fs.String("base", "", "override the base branch")
	format := fs.String("format", "text", "output format: text or json")
	if _, err := parseArgs(fs, args, 0); err != nil {
		return ExitUsage
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintf(stderr, "copse prune: unknown --format %q (want text or json)\n", *format)
		return ExitUsage
	}
	ws, err := open(dir)
	if err != nil {
		fmt.Fprintf(stderr, "copse: %v\n", err)
		return ExitErr
	}
	baseShown := *baseFlag
	if baseShown == "" {
		baseShown = ws.cfg.base
	}
	if !*dryRun {
		// Drop stale registrations (hand-deleted directories) up front:
		// git refuses to delete a branch a worktree still claims.
		_ = ws.git.PruneWorktrees()
	}
	rep := render.PruneReport{DryRun: *dryRun, Base: baseShown}
	changed := false
	tasks := append([]task.Task{}, ws.state.Tasks...)
	for _, t := range tasks {
		info, err := ws.inspect(t, *baseFlag, *gone)
		if err != nil {
			fmt.Fprintf(stderr, "copse prune: %v\n", err)
			return ExitErr
		}
		v := info.verdict
		switch {
		case v.Prunable && !info.present:
			// Nothing on disk: forget the task, and drop the branch too
			// when the work is verifiably merged.
			if !*dryRun {
				if info.branch && v.Merged && !*keepBranch {
					if err := ws.git.DeleteBranch(t.Branch); err != nil {
						fmt.Fprintf(stderr, "copse prune: %v\n", err)
						return ExitErr
					}
				}
				ws.state.Remove(t.Name)
				changed = true
			}
			rep.Actions = append(rep.Actions, render.PruneAction{
				Verb: "drop", Name: t.Name, Branch: t.Branch,
				Detail: v.Reason + " — worktree already gone",
			})
			rep.Pruned++
		case v.Prunable && len(info.dirty) > 0 && !*force:
			rep.Actions = append(rep.Actions, render.PruneAction{
				Verb: "skip", Name: t.Name, Branch: t.Branch,
				Detail: fmt.Sprintf("%s — dirty (%s), use --force", v.Reason, plural(len(info.dirty), "path")),
			})
			rep.Skipped++
		case v.Prunable:
			if !*dryRun {
				if err := ws.git.RemoveWorktree(t.Path); err != nil {
					fmt.Fprintf(stderr, "copse prune: %v\n", err)
					return ExitErr
				}
				if info.branch && !*keepBranch {
					if err := ws.git.DeleteBranch(t.Branch); err != nil {
						fmt.Fprintf(stderr, "copse prune: %v\n", err)
						return ExitErr
					}
				}
				ws.state.Remove(t.Name)
				changed = true
			}
			detail := v.Reason
			if *keepBranch {
				detail += " — branch kept"
			}
			rep.Actions = append(rep.Actions, render.PruneAction{
				Verb: "prune", Name: t.Name, Branch: t.Branch, Detail: detail,
			})
			rep.Pruned++
		default:
			detail := v.Reason
			if !info.present {
				detail += " — worktree missing (copse rm to forget)"
			}
			rep.Actions = append(rep.Actions, render.PruneAction{
				Verb: "keep", Name: t.Name, Branch: t.Branch, Detail: detail,
			})
			rep.Kept++
		}
	}
	if changed {
		_ = ws.git.PruneWorktrees()
		if err := ws.state.Save(ws.statePath); err != nil {
			fmt.Fprintf(stderr, "copse prune: %v\n", err)
			return ExitErr
		}
	}
	if *format == "json" {
		if err := render.PruneJSON(stdout, rep); err != nil {
			fmt.Fprintf(stderr, "copse prune: %v\n", err)
			return ExitErr
		}
		return ExitOK
	}
	render.PruneText(stdout, rep)
	return ExitOK
}
