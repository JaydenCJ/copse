package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JaydenCJ/copse/internal/carry"
	"github.com/JaydenCJ/copse/internal/task"
)

// runNew creates a task: branch + worktree + carried env files + state.
func runNew(dir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.SetOutput(stderr)
	branch := fs.String("branch", "", "branch to create (default: <prefix><task>)")
	from := fs.String("from", "", "start point (default: the base branch)")
	base := fs.String("base", "", "base branch recorded for merge detection")
	note := fs.String("note", "", "free-form note shown by copse ls")
	noCarry := fs.Bool("no-carry", false, "do not copy env files into the new worktree")
	porcelain := fs.Bool("porcelain", false, "print only the new worktree path")
	var extraCarry multiFlag
	fs.Var(&extraCarry, "carry", "extra carry glob (repeatable)")
	pos, err := parseArgs(fs, args, 1)
	if err != nil {
		return ExitUsage
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, "copse new: exactly one task name required")
		return ExitUsage
	}
	name := pos[0]
	if err := task.ValidateName(name); err != nil {
		fmt.Fprintf(stderr, "copse new: %v\n", err)
		return ExitUsage
	}
	ws, err := open(dir)
	if err != nil {
		fmt.Fprintf(stderr, "copse: %v\n", err)
		return ExitErr
	}
	if existing := ws.state.Find(name); existing != nil {
		fmt.Fprintf(stderr, "copse new: task %q already exists (%s)\n", name, existing.Path)
		return ExitErr
	}
	branchName := *branch
	if branchName == "" {
		branchName = ws.cfg.prefix + name
	}
	baseName := *base
	if baseName == "" {
		baseName = ws.cfg.base
	}
	fromRef := *from
	if fromRef == "" {
		fromRef = baseName
	}
	if ws.git.HasLocalBranch(branchName) {
		fmt.Fprintf(stderr, "copse new: branch %q already exists — pick another task name or pass --branch\n", branchName)
		return ExitErr
	}
	if !ws.git.HasRef(fromRef) {
		fmt.Fprintf(stderr, "copse new: start point %q does not resolve — commit first, set copse.base, or pass --from\n", fromRef)
		return ExitErr
	}
	wtPath := filepath.Join(ws.cfg.root, name)
	if _, err := os.Stat(wtPath); err == nil {
		fmt.Fprintf(stderr, "copse new: %s already exists\n", wtPath)
		return ExitErr
	}
	if err := os.MkdirAll(ws.cfg.root, 0o755); err != nil {
		fmt.Fprintf(stderr, "copse new: %v\n", err)
		return ExitErr
	}
	if err := ws.git.AddWorktree(wtPath, branchName, fromRef); err != nil {
		fmt.Fprintf(stderr, "copse new: %v\n", err)
		return ExitErr
	}
	startHash, err := ws.git.Hash(branchName)
	if err != nil {
		fmt.Fprintf(stderr, "copse new: %v\n", err)
		return ExitErr
	}
	// Record the task before carrying, so a failed carry leaves a task
	// that `copse env` can repair instead of an orphaned worktree.
	ws.state.Add(task.Task{
		Name:      name,
		Branch:    branchName,
		Path:      wtPath,
		Base:      baseName,
		StartHash: startHash,
		CreatedAt: time.Now().UTC(),
		Note:      *note,
	})
	if err := ws.state.Save(ws.statePath); err != nil {
		fmt.Fprintf(stderr, "copse new: %v\n", err)
		return ExitErr
	}
	patterns := append(append([]string{}, ws.cfg.carry...), extraCarry...)
	var carried []string
	if !*noCarry {
		tracked, err := ws.git.LsFiles()
		if err != nil {
			fmt.Fprintf(stderr, "copse new: %v\n", err)
			return ExitErr
		}
		carried, err = carry.Discover(ws.top, patterns, tracked)
		if err != nil {
			fmt.Fprintf(stderr, "copse new: %v\n", err)
			return ExitErr
		}
		if len(carried) > 0 {
			if _, err := carry.Sync(ws.top, wtPath, carried); err != nil {
				fmt.Fprintf(stderr, "copse new: %v\n", err)
				return ExitErr
			}
			ws.state.Find(name).Carried = carried
			if err := ws.state.Save(ws.statePath); err != nil {
				fmt.Fprintf(stderr, "copse new: %v\n", err)
				return ExitErr
			}
		}
	}
	if *porcelain {
		fmt.Fprintln(stdout, wtPath)
		return ExitOK
	}
	short, _ := ws.git.ShortHash(branchName)
	fmt.Fprintf(stdout, "created task %s\n", name)
	fmt.Fprintf(stdout, "  branch    %s  (from %s @ %s)\n", branchName, fromRef, short)
	fmt.Fprintf(stdout, "  worktree  %s\n", displayPath(ws.top, wtPath))
	switch {
	case *noCarry:
		fmt.Fprintf(stdout, "  carried   nothing (--no-carry)\n")
	case len(carried) == 0:
		fmt.Fprintf(stdout, "  carried   nothing matched (patterns: %s)\n", strings.Join(patterns, ", "))
	default:
		fmt.Fprintf(stdout, "  carried   %s\n", strings.Join(carried, ", "))
	}
	return ExitOK
}
