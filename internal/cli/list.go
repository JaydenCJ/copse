package cli

import (
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/JaydenCJ/copse/internal/render"
)

// runList renders the task table (the default command).
func runList(dir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "text", "output format: text or json")
	baseFlag := fs.String("base", "", "override the base branch")
	if _, err := parseArgs(fs, args, 0); err != nil {
		return ExitUsage
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintf(stderr, "copse ls: unknown --format %q (want text or json)\n", *format)
		return ExitUsage
	}
	ws, err := open(dir)
	if err != nil {
		fmt.Fprintf(stderr, "copse: %v\n", err)
		return ExitErr
	}
	base := *baseFlag
	if base == "" {
		base = ws.cfg.base
	}
	rep := render.ListReport{Repo: ws.repo, Base: base, Unmanaged: ws.unmanagedCount()}
	for _, t := range ws.state.Tasks {
		info, err := ws.inspect(t, *baseFlag, true)
		if err != nil {
			fmt.Fprintf(stderr, "copse: %v\n", err)
			return ExitErr
		}
		carried := t.Carried
		if carried == nil {
			carried = []string{}
		}
		rep.Tasks = append(rep.Tasks, render.TaskRow{
			Name:    t.Name,
			Branch:  t.Branch,
			Base:    info.base,
			Path:    t.Path,
			Display: displayPath(ws.top, t.Path),
			State:   info.stateLabel(),
			Reason:  info.verdict.Reason,
			Dirty:   len(info.dirty) > 0,
			Ahead:   info.ahead,
			Behind:  info.behind,
			Created: t.CreatedAt.UTC().Format(time.RFC3339),
			Note:    t.Note,
			Carried: carried,
		})
	}
	if *format == "json" {
		if err := render.ListJSON(stdout, rep); err != nil {
			fmt.Fprintf(stderr, "copse: %v\n", err)
			return ExitErr
		}
		return ExitOK
	}
	render.ListText(stdout, rep)
	return ExitOK
}
