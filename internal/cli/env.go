package cli

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/JaydenCJ/copse/internal/carry"
	"github.com/JaydenCJ/copse/internal/render"
	"github.com/JaydenCJ/copse/internal/task"
)

// runEnv re-syncs carried env files into task worktrees, or, with
// --check, reports drift without writing and exits 1 when any exists.
func runEnv(dir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("env", flag.ContinueOnError)
	fs.SetOutput(stderr)
	check := fs.Bool("check", false, "report drift without writing; exit 1 when drift exists")
	all := fs.Bool("all", false, "apply to every task")
	pos, err := parseArgs(fs, args, 1)
	if err != nil {
		return ExitUsage
	}
	if *all == (len(pos) == 1) {
		fmt.Fprintln(stderr, "copse env: name exactly one task, or pass --all")
		return ExitUsage
	}
	ws, err := open(dir)
	if err != nil {
		fmt.Fprintf(stderr, "copse: %v\n", err)
		return ExitErr
	}
	var targets []task.Task
	if *all {
		targets = append(targets, ws.state.Tasks...)
	} else {
		t := ws.state.Find(pos[0])
		if t == nil {
			fmt.Fprintf(stderr, "copse: no task named %q (see copse ls)\n", pos[0])
			return ExitErr
		}
		targets = append(targets, *t)
	}
	if len(targets) == 0 {
		fmt.Fprintln(stdout, "copse env: no tasks")
		return ExitOK
	}
	tracked, err := ws.git.LsFiles()
	if err != nil {
		fmt.Fprintf(stderr, "copse env: %v\n", err)
		return ExitErr
	}
	files, err := carry.Discover(ws.top, ws.cfg.carry, tracked)
	if err != nil {
		fmt.Fprintf(stderr, "copse env: %v\n", err)
		return ExitErr
	}
	totalDrift := 0
	changed := false
	for i, t := range targets {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		if _, err := os.Stat(t.Path); err != nil {
			fmt.Fprintf(stderr, "copse env: worktree of task %s is missing (%s) — forget it with copse rm %s\n", t.Name, t.Path, t.Name)
			return ExitErr
		}
		rep := render.EnvReport{Task: t.Name, Check: *check}
		if *check {
			results, drift, err := carry.Check(ws.top, t.Path, files)
			if err != nil {
				fmt.Fprintf(stderr, "copse env: %v\n", err)
				return ExitErr
			}
			for _, res := range results {
				rep.Lines = append(rep.Lines, render.EnvLine{Path: res.Rel, Status: string(res.Drift)})
			}
			rep.Drift = drift
			totalDrift += drift
		} else {
			results, err := carry.Sync(ws.top, t.Path, files)
			if err != nil {
				fmt.Fprintf(stderr, "copse env: %v\n", err)
				return ExitErr
			}
			for _, res := range results {
				rep.Lines = append(rep.Lines, render.EnvLine{Path: res.Rel, Status: string(res.Action)})
			}
			ws.state.Find(t.Name).Carried = files
			changed = true
		}
		render.EnvText(stdout, rep)
	}
	if changed {
		if err := ws.state.Save(ws.statePath); err != nil {
			fmt.Fprintf(stderr, "copse env: %v\n", err)
			return ExitErr
		}
	}
	if *check && totalDrift > 0 {
		return ExitDrift
	}
	return ExitOK
}
