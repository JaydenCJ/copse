// Package carry copies the untracked configuration files a task worktree
// needs — .env files and friends — from the main worktree into task
// worktrees, and reports drift between the two.
package carry

import (
	"path"
	"strings"
)

// Match reports whether a carry pattern matches a worktree-relative path.
//
// Patterns use "/" separators. Within one segment, "*" matches any run of
// characters and "?" matches exactly one; a whole segment of "**" matches
// any number of segments, including zero. A pattern containing no "/" is
// matched against the file's basename, which is why the default ".env.*"
// finds env files at any depth.
func Match(pattern, rel string) bool {
	if !strings.Contains(pattern, "/") {
		return matchSegment(pattern, path.Base(rel))
	}
	return matchSegments(splitSlash(pattern), splitSlash(rel))
}

func splitSlash(p string) []string {
	return strings.Split(strings.Trim(p, "/"), "/")
}

// matchSegments matches pattern segments against path segments; "**"
// greedily tries every possible number of swallowed segments.
func matchSegments(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}
	if pat[0] == "**" {
		for skip := 0; skip <= len(segs); skip++ {
			if matchSegments(pat[1:], segs[skip:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	if !matchSegment(pat[0], segs[0]) {
		return false
	}
	return matchSegments(pat[1:], segs[1:])
}

// matchSegment matches one glob segment (with * and ?) against one path
// segment, iteratively with single-star backtracking — no regexp, no
// recursion depth to worry about.
func matchSegment(pat, seg string) bool {
	pi, si := 0, 0
	starPi, starSi := -1, -1
	for si < len(seg) {
		switch {
		case pi < len(pat) && (pat[pi] == '?' || pat[pi] == seg[si]):
			pi++
			si++
		case pi < len(pat) && pat[pi] == '*':
			starPi, starSi = pi, si
			pi++
		case starPi >= 0:
			// Backtrack: let the last * absorb one more character.
			starSi++
			pi, si = starPi+1, starSi
		default:
			return false
		}
	}
	for pi < len(pat) && pat[pi] == '*' {
		pi++
	}
	return pi == len(pat)
}
