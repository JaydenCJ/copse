// Package cli implements the copse command-line interface. Run takes
// argv and two writers and returns an exit code, so every command is
// testable in-process without building a binary.
package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/JaydenCJ/copse/internal/version"
)

// Exit codes. Documented in the README; `env --check` uses ExitDrift as
// its machine-readable verdict.
const (
	ExitOK    = 0 // success
	ExitDrift = 1 // env --check found drifted files
	ExitUsage = 2 // bad invocation
	ExitErr   = 3 // git/filesystem failure, unknown task, unsafe request
)

// Run dispatches argv and returns the process exit code. A leading
// `-C dir` runs copse as if started inside dir, like git's own -C.
func Run(args []string, stdout, stderr io.Writer) int {
	dir := ""
	for len(args) > 0 && args[0] == "-C" {
		if len(args) < 2 {
			fmt.Fprintln(stderr, "copse: -C needs a directory argument")
			return ExitUsage
		}
		dir = args[1]
		args = args[2:]
	}
	if len(args) == 0 {
		return runList(dir, nil, stdout, stderr)
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "new":
		return runNew(dir, rest, stdout, stderr)
	case "ls", "list":
		return runList(dir, rest, stdout, stderr)
	case "env":
		return runEnv(dir, rest, stdout, stderr)
	case "rm", "remove":
		return runRemove(dir, rest, stdout, stderr)
	case "prune":
		return runPrune(dir, rest, stdout, stderr)
	case "path":
		return runPath(dir, rest, stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "copse %s\n", version.Version)
		return ExitOK
	case "help", "--help", "-h":
		usage(stdout)
		return ExitOK
	default:
		fmt.Fprintf(stderr, "copse: unknown command %q\n\n", cmd)
		usage(stderr)
		return ExitUsage
	}
}

// multiFlag is a repeatable string flag.
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// parseArgs parses fs against args, letting flags appear before or after
// positional arguments (Go's flag package stops at the first positional).
func parseArgs(fs *flag.FlagSet, args []string, maxPositional int) ([]string, error) {
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	var pos []string
	rest := fs.Args()
	for len(rest) > 0 {
		if strings.HasPrefix(rest[0], "-") && rest[0] != "-" {
			if err := fs.Parse(rest); err != nil {
				return nil, err
			}
		} else {
			pos = append(pos, rest[0])
			if err := fs.Parse(rest[1:]); err != nil {
				return nil, err
			}
		}
		rest = fs.Args()
	}
	if len(pos) > maxPositional {
		err := fmt.Errorf("unexpected argument %q", pos[maxPositional])
		fmt.Fprintf(fs.Output(), "copse %s: %v\n", fs.Name(), err)
		return nil, err
	}
	return pos, nil
}

func runPath(dir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("path", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pos, err := parseArgs(fs, args, 1)
	if err != nil {
		return ExitUsage
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, "copse path: exactly one task name required")
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
	fmt.Fprintln(stdout, t.Path)
	return ExitOK
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `copse %s — task-scoped git worktrees

Usage:
  copse [-C dir] <command> [flags] [args]

Commands:
  new <task>    create a named worktree + branch and carry env files in
  ls            list tasks with merge/dirty state (default command)
  env <task>    re-sync carried env files (--check verifies only)
  rm <task>     remove one task: worktree, branch, and state
  prune         remove every task whose branch has merged into the base
  path <task>   print the task's worktree path (for cd and scripts)
  version       print the version

New flags:
  --branch NAME    branch to create (default: <prefix><task>)
  --from REF       start point (default: the base branch)
  --base BRANCH    base branch recorded for merge detection
  --carry GLOB     extra carry pattern (repeatable; default: .env, .env.*)
  --no-carry       skip env carrying entirely
  --note TEXT      free-form note shown by copse ls
  --porcelain      print only the new worktree path

Env flags:
  --check          report drift without writing; exit 1 when drift exists
  --all            apply to every task

Rm flags:
  --keep-branch    keep the branch; remove only worktree and state
  --force          remove even when dirty or unmerged

Prune flags:
  --dry-run        show what would happen, change nothing
  --gone           also prune tasks whose upstream branch was deleted
  --keep-branch    remove worktrees but keep the branches
  --force          prune merged tasks even when their worktree is dirty
  --base BRANCH    override the base branch — ls takes this flag too
  --format FORMAT  text (default) or json — ls takes this flag too

Configuration (git config):
  copse.root          where task worktrees live (default: ../<repo>.copse)
  copse.branchprefix  branch name prefix (default: copse/)
  copse.base          base branch (default: origin/HEAD, then main/master)
  copse.carry         carry glob, repeatable (default: .env and .env.*)

Exit codes: 0 ok · 1 drift found (env --check) · 2 usage error · 3 runtime error
`, version.Version)
}
