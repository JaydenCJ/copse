// Tests for the task state store. tasks.json is the only mutable thing
// copse owns, so load/save must be lossless, atomic, and honest about
// versions it does not understand.
package task

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sample() Task {
	return Task{
		Name:      "rate-limit",
		Branch:    "copse/rate-limit",
		Path:      "/work/demo.copse/rate-limit",
		Base:      "main",
		CreatedAt: time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC),
		Note:      "429 retry middleware",
		Carried:   []string{".env", "services/api/.env.local"},
	}
}

func TestLoadMissingFileGivesEmptyState(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "copse", "tasks.json"))
	if err != nil {
		t.Fatalf("missing file must not error: %v", err)
	}
	if len(s.Tasks) != 0 || s.Schema != SchemaVersion {
		t.Fatalf("want empty state at current schema, got %+v", s)
	}
}

func TestSaveLoadRoundTripIsLossless(t *testing.T) {
	path := filepath.Join(t.TempDir(), "copse", "tasks.json")
	s := &State{Schema: SchemaVersion}
	s.Add(sample())
	if err := s.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Tasks) != 1 {
		t.Fatalf("want 1 task, got %+v", got.Tasks)
	}
	g, w := got.Tasks[0], sample()
	if g.Name != w.Name || g.Branch != w.Branch || g.Path != w.Path ||
		g.Base != w.Base || !g.CreatedAt.Equal(w.CreatedAt) || g.Note != w.Note ||
		len(g.Carried) != 2 || g.Carried[0] != ".env" {
		t.Fatalf("round trip lost data:\n got %+v\nwant %+v", g, w)
	}
}

func TestSaveSortsTasksAndEndsWithNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	s := &State{}
	b := sample()
	b.Name = "zeta"
	a := sample()
	a.Name = "alpha"
	s.Tasks = []Task{b, a} // deliberately unsorted
	if err := s.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(raw), "\n") {
		t.Fatal("state file must end with a newline")
	}
	if strings.Index(string(raw), "alpha") > strings.Index(string(raw), "zeta") {
		t.Fatal("tasks must be saved sorted by name")
	}
	// No .tmp litter from the atomic write.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file must be renamed away")
	}
}

func TestLoadRejectsNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	if err := os.WriteFile(path, []byte(`{"schema_version": 99, "tasks": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "schema_version 99") {
		t.Fatalf("newer schema must be refused loudly, got %v", err)
	}
}

func TestLoadRejectsCorruptJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("corrupt state must error, not silently reset")
	}
}

func TestFindAddRemove(t *testing.T) {
	s := &State{}
	s.Add(sample())
	if s.Find("rate-limit") == nil {
		t.Fatal("Find must locate an added task")
	}
	if s.Find("nope") != nil {
		t.Fatal("Find must return nil for unknown names")
	}
	// Find returns a live pointer: mutations must stick.
	s.Find("rate-limit").Note = "updated"
	if s.Tasks[0].Note != "updated" {
		t.Fatal("Find must return a pointer into the slice")
	}
	if !s.Remove("rate-limit") || len(s.Tasks) != 0 {
		t.Fatalf("Remove failed: %+v", s.Tasks)
	}
	if s.Remove("rate-limit") {
		t.Fatal("second Remove must report false")
	}
}

func TestValidateNameAcceptsTypicalTaskNames(t *testing.T) {
	for _, name := range []string{"rate-limit", "fix_429", "issue.1234", "A1", "x", "retry-v0.2"} {
		if err := ValidateName(name); err != nil {
			t.Fatalf("ValidateName(%q) = %v, want nil", name, err)
		}
	}
}

func TestValidateNameRejectsEmptyAndOverlong(t *testing.T) {
	if ValidateName("") == nil {
		t.Fatal("empty name must be rejected")
	}
	if ValidateName(strings.Repeat("a", 65)) == nil {
		t.Fatal("65-char name must be rejected")
	}
	if err := ValidateName(strings.Repeat("a", 64)); err != nil {
		t.Fatalf("64-char name should be fine: %v", err)
	}
}

func TestValidateNameRejectsPathTraversal(t *testing.T) {
	// Names become directory names under the copse root; anything that
	// could escape it must die here, before any filesystem call.
	for _, name := range []string{"..", "a..b", "x/../y", "sub/dir"} {
		if ValidateName(name) == nil {
			t.Fatalf("ValidateName(%q) must reject traversal-capable names", name)
		}
	}
}

func TestValidateNameRejectsShellAndUnicodeNoise(t *testing.T) {
	for _, name := range []string{"a b", "a\tb", "a;b", "tsk*", "täsk", "a\nb"} {
		if ValidateName(name) == nil {
			t.Fatalf("ValidateName(%q) must be rejected", name)
		}
	}
}

func TestValidateNameRejectsBadFirstAndLastChars(t *testing.T) {
	for _, name := range []string{"-flag", ".hidden", "_x", "ends.", "x.lock"} {
		if ValidateName(name) == nil {
			t.Fatalf("ValidateName(%q) must be rejected", name)
		}
	}
}
