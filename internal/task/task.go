// Package task stores copse's own view of the world: which named tasks
// exist, which branch and worktree belong to each, and which env files
// were carried in. The state file lives inside the repository's common
// git directory, so it is shared by every worktree and never committed.
package task

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SchemaVersion identifies the tasks.json layout this build understands.
const SchemaVersion = 1

// Task is one managed worktree. StartHash pins the commit the branch was
// cut at, so copse can tell a freshly created task (tip == StartHash,
// nothing to lose) apart from one whose commits genuinely merged.
type Task struct {
	Name      string    `json:"name"`
	Branch    string    `json:"branch"`
	Path      string    `json:"path"` // absolute worktree path
	Base      string    `json:"base"` // base branch recorded at creation
	StartHash string    `json:"start_hash,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Note      string    `json:"note,omitempty"`
	Carried   []string  `json:"carried,omitempty"` // worktree-relative env files
}

// State is the full contents of tasks.json.
type State struct {
	Schema int    `json:"schema_version"`
	Tasks  []Task `json:"tasks"`
}

// Load reads the state file. A missing file yields an empty state, so a
// repository becomes copse-managed the first time `copse new` runs.
func Load(path string) (*State, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &State{Schema: SchemaVersion}, nil
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if s.Schema > SchemaVersion {
		return nil, fmt.Errorf("%s: schema_version %d is newer than this copse understands (max %d) — upgrade copse", path, s.Schema, SchemaVersion)
	}
	s.Schema = SchemaVersion
	s.sort()
	return &s, nil
}

// Save writes the state file atomically (temp file + rename), creating
// the parent directory on first use.
func (s *State) Save(path string) error {
	s.sort()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *State) sort() {
	sort.Slice(s.Tasks, func(i, j int) bool { return s.Tasks[i].Name < s.Tasks[j].Name })
}

// Find returns a pointer into the task list for the named task, or nil.
func (s *State) Find(name string) *Task {
	for i := range s.Tasks {
		if s.Tasks[i].Name == name {
			return &s.Tasks[i]
		}
	}
	return nil
}

// Add appends a task; the caller must have checked for duplicates.
func (s *State) Add(t Task) {
	s.Tasks = append(s.Tasks, t)
	s.sort()
}

// Remove deletes the named task and reports whether it existed.
func (s *State) Remove(name string) bool {
	for i := range s.Tasks {
		if s.Tasks[i].Name == name {
			s.Tasks = append(s.Tasks[:i], s.Tasks[i+1:]...)
			return true
		}
	}
	return false
}

// ValidateName enforces the task-name grammar. Names become directory
// names and branch-name suffixes, so the grammar is deliberately strict:
// start with a letter or digit, continue with letters, digits, ".", "_"
// or "-", at most 64 characters. The extra suffix rules mirror git's own
// ref-name restrictions.
func ValidateName(name string) error {
	if name == "" {
		return errors.New("task name is empty")
	}
	if len(name) > 64 {
		return fmt.Errorf("task name %q is longer than 64 characters", name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("task name %q must not contain %q", name, "..")
	}
	for i, r := range name {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		if i > 0 {
			ok = ok || r == '.' || r == '_' || r == '-'
		}
		if !ok {
			return fmt.Errorf("task name %q: character %q not allowed (want [A-Za-z0-9._-], starting with a letter or digit)", name, r)
		}
	}
	if strings.HasSuffix(name, ".") {
		return fmt.Errorf("task name %q must not end with a dot", name)
	}
	if strings.HasSuffix(name, ".lock") {
		return fmt.Errorf("task name %q must not end with .lock (git ref restriction)", name)
	}
	return nil
}
