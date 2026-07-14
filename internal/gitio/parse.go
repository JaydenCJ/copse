package gitio

import "strings"

// Worktree is one entry of `git worktree list --porcelain`. The first
// entry git prints is always the main worktree.
type Worktree struct {
	Path       string
	Head       string
	Branch     string // short branch name; empty when detached or bare
	Bare       bool
	Detached   bool
	Locked     bool
	LockReason string
	Prunable   bool
}

// ParseWorktrees parses `git worktree list --porcelain` output: blocks of
// "key value" lines separated by blank lines.
func ParseWorktrees(raw []byte) []Worktree {
	var (
		wts []Worktree
		cur *Worktree
	)
	flush := func() {
		if cur != nil {
			wts = append(wts, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			flush()
			continue
		}
		key, val, _ := strings.Cut(line, " ")
		switch key {
		case "worktree":
			flush()
			cur = &Worktree{Path: val}
		case "HEAD":
			if cur != nil {
				cur.Head = val
			}
		case "branch":
			if cur != nil {
				cur.Branch = strings.TrimPrefix(val, "refs/heads/")
			}
		case "bare":
			if cur != nil {
				cur.Bare = true
			}
		case "detached":
			if cur != nil {
				cur.Detached = true
			}
		case "locked":
			if cur != nil {
				cur.Locked = true
				cur.LockReason = val
			}
		case "prunable":
			if cur != nil {
				cur.Prunable = true
			}
		}
	}
	flush()
	return wts
}

// Cherry summarizes `git cherry <upstream> <head>` output: how many
// commits on head are patch-equivalent to something in upstream ("-")
// versus genuinely new ("+").
type Cherry struct {
	Equivalent int
	New        int
}

// ParseCherry counts the marker lines of `git cherry` output.
func ParseCherry(raw []byte) Cherry {
	var c Cherry
	for _, line := range strings.Split(string(raw), "\n") {
		switch {
		case strings.HasPrefix(line, "- "):
			c.Equivalent++
		case strings.HasPrefix(line, "+ "):
			c.New++
		}
	}
	return c
}

// StatusEntry is one record of `git status --porcelain -z`.
type StatusEntry struct {
	Code string // two-character XY status, e.g. " M", "??", "A "
	Path string
}

// ParseStatus parses NUL-separated porcelain v1 status output. Rename and
// copy records carry a second path token (the source), which is consumed
// and dropped: for dirty-checking, only the destination matters.
func ParseStatus(raw []byte) []StatusEntry {
	var entries []StatusEntry
	tokens := strings.Split(string(raw), "\x00")
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if len(tok) < 4 { // "XY <path>" is the shortest valid record
			continue
		}
		code, path := tok[:2], tok[3:]
		entries = append(entries, StatusEntry{Code: code, Path: path})
		if code[0] == 'R' || code[0] == 'C' {
			i++ // skip the rename/copy source path token
		}
	}
	return entries
}

// DirtyPaths returns the paths that make a task worktree unsafe to
// remove: any change to tracked files, plus untracked files that copse
// did not carry in itself. Carried env files are expected to be untracked
// — they must not make every task look permanently dirty.
func DirtyPaths(entries []StatusEntry, carried []string) []string {
	carriedSet := make(map[string]bool, len(carried))
	for _, c := range carried {
		carriedSet[c] = true
	}
	var dirty []string
	for _, e := range entries {
		if e.Code == "!!" {
			continue // ignored files never block removal
		}
		if e.Code == "??" && carriedSet[e.Path] {
			continue
		}
		dirty = append(dirty, e.Path)
	}
	return dirty
}

// ParseUpstreamGone interprets a "%(upstream:short)<US>%(upstream:track)"
// for-each-ref line: true only when an upstream is configured and its
// tracking state is "[gone]".
func ParseUpstreamGone(line string) bool {
	upstream, track, ok := strings.Cut(line, "\x1f")
	if !ok || upstream == "" {
		return false
	}
	return track == "[gone]"
}
