// Tests for text and JSON rendering. Output is a contract: the text
// table is what humans scan five times a day, and the JSON envelope is
// what scripts parse — both must be stable for identical input.
package render

import (
	"encoding/json"
	"strings"
	"testing"
)

func sampleRows() []TaskRow {
	return []TaskRow{
		{
			Name: "auth-retry", Branch: "copse/auth-retry", Base: "main",
			Path: "/work/demo.copse/auth-retry", Display: "../demo.copse/auth-retry",
			State: "merged", Reason: "merged into main (ancestor)",
			Ahead: 0, Behind: 2, Created: "2026-07-12T09:00:00Z",
			Carried: []string{".env"},
		},
		{
			Name: "rate-limit", Branch: "copse/rate-limit", Base: "main",
			Path: "/work/demo.copse/rate-limit", Display: "../demo.copse/rate-limit",
			State: "active", Reason: "2 commits ahead of main", Dirty: true,
			Ahead: 2, Behind: 0, Created: "2026-07-12T09:05:00Z",
			Note:    "429 retry middleware",
			Carried: []string{".env"},
		},
	}
}

func TestListTextAlignsHeaderAndRows(t *testing.T) {
	var b strings.Builder
	ListText(&b, ListReport{Repo: "demo", Base: "main", Tasks: sampleRows()})
	out := b.String()
	lines := strings.Split(out, "\n")
	if !strings.Contains(lines[0], "copse — 2 tasks in demo (base: main)") {
		t.Fatalf("header wrong: %q", lines[0])
	}
	var headerIdx int
	for i, l := range lines {
		if strings.HasPrefix(l, "NAME") {
			headerIdx = i
			break
		}
	}
	header := lines[headerIdx]
	for _, col := range []string{"NAME", "STATE", "DIRTY", "AHEAD", "BEHIND", "BRANCH", "PATH"} {
		if !strings.Contains(header, col) {
			t.Fatalf("header missing %s: %q", col, header)
		}
	}
	// Column starts must line up between header and data rows.
	row := lines[headerIdx+1]
	if strings.Index(header, "STATE") != strings.Index(row, "merged") {
		t.Fatalf("STATE column misaligned:\n%q\n%q", header, row)
	}
	if !strings.Contains(out, "└─ 429 retry middleware") {
		t.Fatalf("note line missing:\n%s", out)
	}
}

func TestListTextEmptyStateGivesGuidance(t *testing.T) {
	var b strings.Builder
	ListText(&b, ListReport{Repo: "demo", Base: "main"})
	out := b.String()
	if !strings.Contains(out, "0 tasks") || !strings.Contains(out, "copse new <name>") {
		t.Fatalf("empty state must point at copse new:\n%s", out)
	}
}

func TestListTextUnknownCountsRenderAsDash(t *testing.T) {
	rows := []TaskRow{{Name: "lost", Branch: "copse/lost", State: "missing", Ahead: -1, Behind: -1}}
	var b strings.Builder
	ListText(&b, ListReport{Repo: "demo", Base: "main", Tasks: rows})
	if !strings.Contains(b.String(), "-") {
		t.Fatalf("unknown ahead/behind must render as '-':\n%s", b.String())
	}
}

func TestListTextMentionsUnmanagedWorktrees(t *testing.T) {
	var b strings.Builder
	ListText(&b, ListReport{Repo: "demo", Base: "main", Tasks: sampleRows(), Unmanaged: 1})
	if !strings.Contains(b.String(), "1 unmanaged worktree") {
		t.Fatalf("unmanaged footer missing:\n%s", b.String())
	}
}

func TestListJSONEnvelopeAndDeterminism(t *testing.T) {
	var a, b strings.Builder
	rep := ListReport{Repo: "demo", Base: "main", Tasks: sampleRows()}
	if err := ListJSON(&a, rep); err != nil {
		t.Fatal(err)
	}
	if err := ListJSON(&b, rep); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Fatal("JSON output must be byte-identical for identical input")
	}
	var doc struct {
		Tool   string `json:"tool"`
		Schema int    `json:"schema_version"`
		Tasks  []struct {
			Name    string   `json:"name"`
			State   string   `json:"state"`
			Carried []string `json:"carried"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(a.String()), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if doc.Tool != "copse" || doc.Schema != 1 || len(doc.Tasks) != 2 {
		t.Fatalf("envelope wrong: %+v", doc)
	}
	if doc.Tasks[0].Carried == nil {
		t.Fatal("carried must serialize as an array, never null")
	}
	// Zero tasks must serialize as [], never null, so jq pipelines hold.
	var empty strings.Builder
	if err := ListJSON(&empty, ListReport{Repo: "demo", Base: "main"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(empty.String(), `"tasks": []`) {
		t.Fatalf("empty tasks must serialize as []:\n%s", empty.String())
	}
}

func TestPruneTextDryRunUsesConditionalVerbs(t *testing.T) {
	rep := PruneReport{
		DryRun: true, Base: "main",
		Actions: []PruneAction{
			{Verb: "prune", Name: "auth-retry", Branch: "copse/auth-retry", Detail: "merged into main (ancestor)"},
			{Verb: "keep", Name: "rate-limit", Branch: "copse/rate-limit", Detail: "2 commits ahead of main"},
		},
		Pruned: 1, Kept: 1,
	}
	var b strings.Builder
	PruneText(&b, rep)
	out := b.String()
	if !strings.Contains(out, "would prune  auth-retry") {
		t.Fatalf("dry-run must say 'would prune':\n%s", out)
	}
	if !strings.Contains(out, "would prune 1 of 2 tasks") || !strings.Contains(out, "--dry-run") {
		t.Fatalf("dry-run summary wrong:\n%s", out)
	}
}

func TestPruneTextRealRunSummarizesCounts(t *testing.T) {
	rep := PruneReport{
		Base: "main",
		Actions: []PruneAction{
			{Verb: "prune", Name: "a", Detail: "merged into main (ancestor)"},
			{Verb: "skip", Name: "b", Detail: "merged into main (squash) — dirty (1 path), use --force"},
			{Verb: "keep", Name: "c", Detail: "1 commit ahead of main"},
		},
		Pruned: 1, Kept: 1, Skipped: 1,
	}
	var b strings.Builder
	PruneText(&b, rep)
	if !strings.Contains(b.String(), "pruned 1, kept 1, skipped 1 (base: main)") {
		t.Fatalf("summary wrong:\n%s", b.String())
	}
	// With nothing to consider, say so instead of printing a bare summary.
	b.Reset()
	PruneText(&b, PruneReport{Base: "main"})
	if !strings.Contains(b.String(), "no tasks to consider") {
		t.Fatalf("empty prune output wrong:\n%s", b.String())
	}
}

func TestPruneJSONEnvelope(t *testing.T) {
	var b strings.Builder
	err := PruneJSON(&b, PruneReport{DryRun: true, Base: "main", Pruned: 1,
		Actions: []PruneAction{{Verb: "prune", Name: "a", Branch: "copse/a", Detail: "merged into main (ancestor)"}}})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Tool    string `json:"tool"`
		DryRun  bool   `json:"dry_run"`
		Actions []struct {
			Action string `json:"action"`
		} `json:"actions"`
	}
	if err := json.Unmarshal([]byte(b.String()), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if doc.Tool != "copse" || !doc.DryRun || len(doc.Actions) != 1 || doc.Actions[0].Action != "prune" {
		t.Fatalf("envelope wrong: %+v", doc)
	}
}

func TestEnvTextSyncCheckAndEmpty(t *testing.T) {
	var b strings.Builder
	EnvText(&b, EnvReport{Task: "rate-limit", Lines: []EnvLine{{".env", "copy"}, {".env.test", "same"}}})
	out := b.String()
	if !strings.Contains(out, "env rate-limit — 2 files carried") || !strings.Contains(out, "copy") {
		t.Fatalf("sync output wrong:\n%s", out)
	}

	b.Reset()
	EnvText(&b, EnvReport{Task: "rate-limit", Check: true, Drift: 1,
		Lines: []EnvLine{{".env", "stale"}, {".env.test", "ok"}}})
	out = b.String()
	if !strings.Contains(out, "drift in 1 of 2 files") || !strings.Contains(out, "stale") {
		t.Fatalf("check output wrong:\n%s", out)
	}

	b.Reset()
	EnvText(&b, EnvReport{Task: "rate-limit", Check: true, Lines: []EnvLine{{".env", "ok"}}})
	if !strings.Contains(b.String(), "1 file in sync") {
		t.Fatalf("clean check output wrong:\n%s", b.String())
	}

	b.Reset()
	EnvText(&b, EnvReport{Task: "bare"})
	if !strings.Contains(b.String(), "nothing to carry") {
		t.Fatalf("empty output wrong:\n%s", b.String())
	}
}
