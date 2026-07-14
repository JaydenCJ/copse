// Package render turns copse reports into terminal text and stable JSON.
// Rendering never talks to git; it formats data the CLI already
// gathered, so identical input yields byte-identical output.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// TaskRow is one task as shown by `copse ls`. Ahead/Behind of -1 mean
// "unknown" (the branch or base no longer resolves) and render as "-".
type TaskRow struct {
	Name    string   `json:"name"`
	Branch  string   `json:"branch"`
	Base    string   `json:"base"`
	Path    string   `json:"path"` // absolute
	Display string   `json:"-"`    // path relative to the repo, for text output
	State   string   `json:"state"`
	Reason  string   `json:"reason,omitempty"`
	Dirty   bool     `json:"dirty"`
	Ahead   int      `json:"ahead"`
	Behind  int      `json:"behind"`
	Created string   `json:"created_at"`
	Note    string   `json:"note,omitempty"`
	Carried []string `json:"carried"`
}

// ListReport is the full `copse ls` payload.
type ListReport struct {
	Repo      string
	Base      string
	Tasks     []TaskRow
	Unmanaged int // worktrees registered in git but not managed by copse
}

// Plural formats "1 task" / "3 tasks" for the simple -s words copse uses.
func Plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func cell(n int) string {
	if n < 0 {
		return "-"
	}
	return strconv.Itoa(n)
}

// ListText renders the task table with computed column widths.
func ListText(w io.Writer, r ListReport) {
	fmt.Fprintf(w, "copse — %s in %s (base: %s)\n", Plural(len(r.Tasks), "task"), r.Repo, r.Base)
	if len(r.Tasks) == 0 {
		fmt.Fprintf(w, "\nno tasks yet — create one with: copse new <name>\n")
		return
	}
	fmt.Fprintln(w)
	cols := []struct {
		header string
		right  bool
		get    func(TaskRow) string
	}{
		{"NAME", false, func(t TaskRow) string { return t.Name }},
		{"STATE", false, func(t TaskRow) string { return t.State }},
		{"DIRTY", false, func(t TaskRow) string {
			if t.Dirty {
				return "yes"
			}
			return "-"
		}},
		{"AHEAD", true, func(t TaskRow) string { return cell(t.Ahead) }},
		{"BEHIND", true, func(t TaskRow) string { return cell(t.Behind) }},
		{"BRANCH", false, func(t TaskRow) string { return t.Branch }},
		{"PATH", false, func(t TaskRow) string { return t.Display }},
	}
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = len(c.header)
		for _, t := range r.Tasks {
			if n := len(c.get(t)); n > widths[i] {
				widths[i] = n
			}
		}
	}
	line := func(get func(i int) string) string {
		parts := make([]string, len(cols))
		for i := range cols {
			if cols[i].right {
				parts[i] = fmt.Sprintf("%*s", widths[i], get(i))
			} else {
				parts[i] = fmt.Sprintf("%-*s", widths[i], get(i))
			}
		}
		return strings.TrimRight(strings.Join(parts, "  "), " ")
	}
	fmt.Fprintln(w, line(func(i int) string { return cols[i].header }))
	for _, t := range r.Tasks {
		t := t
		fmt.Fprintln(w, line(func(i int) string { return cols[i].get(t) }))
		if t.Note != "" {
			fmt.Fprintf(w, "  └─ %s\n", t.Note)
		}
	}
	if r.Unmanaged > 0 {
		fmt.Fprintf(w, "\nnot shown: %s outside copse — see git worktree list\n", Plural(r.Unmanaged, "unmanaged worktree"))
	}
}

// ListJSON renders the stable machine-readable form of `copse ls`.
func ListJSON(w io.Writer, r ListReport) error {
	tasks := r.Tasks
	if tasks == nil {
		tasks = []TaskRow{}
	}
	for i := range tasks {
		if tasks[i].Carried == nil {
			tasks[i].Carried = []string{}
		}
	}
	doc := struct {
		Tool   string    `json:"tool"`
		Schema int       `json:"schema_version"`
		Repo   string    `json:"repo"`
		Base   string    `json:"base"`
		Tasks  []TaskRow `json:"tasks"`
	}{"copse", 1, r.Repo, r.Base, tasks}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

// PruneAction is one decision `copse prune` made (or would make).
type PruneAction struct {
	Verb   string `json:"action"` // prune | drop | skip | keep
	Name   string `json:"name"`
	Branch string `json:"branch"`
	Detail string `json:"detail"`
}

// PruneReport is the full `copse prune` payload.
type PruneReport struct {
	DryRun  bool
	Base    string
	Actions []PruneAction
	Pruned  int
	Kept    int
	Skipped int
}

// PruneText renders one aligned line per task plus a summary.
func PruneText(w io.Writer, r PruneReport) {
	if len(r.Actions) == 0 {
		fmt.Fprintln(w, "copse prune: no tasks to consider")
		return
	}
	verbs := make([]string, len(r.Actions))
	verbW, nameW := 0, 0
	for i, a := range r.Actions {
		verbs[i] = a.Verb
		if r.DryRun && (a.Verb == "prune" || a.Verb == "drop") {
			verbs[i] = "would " + a.Verb
		}
		if len(verbs[i]) > verbW {
			verbW = len(verbs[i])
		}
		if len(a.Name) > nameW {
			nameW = len(a.Name)
		}
	}
	for i, a := range r.Actions {
		fmt.Fprintf(w, "%-*s  %-*s  %s\n", verbW, verbs[i], nameW, a.Name, a.Detail)
	}
	fmt.Fprintln(w)
	if r.DryRun {
		fmt.Fprintf(w, "would prune %d of %s (base: %s) — run without --dry-run to apply\n",
			r.Pruned, Plural(len(r.Actions), "task"), r.Base)
	} else {
		fmt.Fprintf(w, "pruned %d, kept %d, skipped %d (base: %s)\n", r.Pruned, r.Kept, r.Skipped, r.Base)
	}
}

// PruneJSON renders the stable machine-readable form of `copse prune`.
func PruneJSON(w io.Writer, r PruneReport) error {
	actions := r.Actions
	if actions == nil {
		actions = []PruneAction{}
	}
	doc := struct {
		Tool    string        `json:"tool"`
		Schema  int           `json:"schema_version"`
		DryRun  bool          `json:"dry_run"`
		Base    string        `json:"base"`
		Pruned  int           `json:"pruned"`
		Kept    int           `json:"kept"`
		Skipped int           `json:"skipped"`
		Actions []PruneAction `json:"actions"`
	}{"copse", 1, r.DryRun, r.Base, r.Pruned, r.Kept, r.Skipped, actions}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

// EnvLine is one carried file in an env report.
type EnvLine struct {
	Path   string `json:"path"`
	Status string `json:"status"` // copy/update/same, or ok/stale/missing
}

// EnvReport is the payload of `copse env` for one task.
type EnvReport struct {
	Task  string
	Check bool
	Lines []EnvLine
	Drift int
}

// EnvText renders one env sync/check block.
func EnvText(w io.Writer, r EnvReport) {
	switch {
	case len(r.Lines) == 0:
		fmt.Fprintf(w, "env %s — nothing to carry (no untracked files match the carry patterns)\n", r.Task)
		return
	case r.Check && r.Drift > 0:
		fmt.Fprintf(w, "env %s — drift in %d of %s\n", r.Task, r.Drift, Plural(len(r.Lines), "file"))
	case r.Check:
		fmt.Fprintf(w, "env %s — %s in sync\n", r.Task, Plural(len(r.Lines), "file"))
	default:
		fmt.Fprintf(w, "env %s — %s carried\n", r.Task, Plural(len(r.Lines), "file"))
	}
	for _, l := range r.Lines {
		fmt.Fprintf(w, "  %-8s %s\n", l.Status, l.Path)
	}
}
