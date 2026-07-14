// Tests for carry-pattern matching. The defaults (".env", ".env.*") must
// find env files at any depth, while anchored patterns must stay put —
// carrying the wrong file into a worktree can leak a production secret
// into a throwaway checkout.
package carry

import "testing"

func TestMatchBasenamePatternAtAnyDepth(t *testing.T) {
	for _, rel := range []string{".env", "services/api/.env", "a/b/c/.env"} {
		if !Match(".env", rel) {
			t.Fatalf("pattern .env must match %q", rel)
		}
	}
	if !Match(".env.*", "services/api/.env.local") {
		t.Fatal("pattern .env.* must match nested .env.local")
	}
	if Match(".env", "env") || Match(".env", "src/.envelope") {
		t.Fatal(".env must not match env or .envelope")
	}
}

func TestMatchStarStaysWithinOneSegment(t *testing.T) {
	if !Match("*.local", "notes.local") {
		t.Fatal("*.local must match notes.local")
	}
	// A no-slash pattern applies to the basename, so depth is fine…
	if !Match("*.local", "deep/notes.local") {
		t.Fatal("basename pattern must match at depth")
	}
	// …but a slashed pattern's * must not cross a separator.
	if Match("config/*.local", "config/sub/notes.local") {
		t.Fatal("* must not cross path separators in anchored patterns")
	}
}

func TestMatchQuestionMarkIsExactlyOneCharacter(t *testing.T) {
	if !Match(".env.?", ".env.1") {
		t.Fatal("? must match one character")
	}
	if Match(".env.?", ".env.12") || Match(".env.?", ".env.") {
		t.Fatal("? must match exactly one character")
	}
}

func TestMatchDoubleStarSpansSegments(t *testing.T) {
	if !Match("services/**/.env", "services/api/v2/.env") {
		t.Fatal("** must span multiple segments")
	}
	if !Match("**/secrets.json", "secrets.json") {
		t.Fatal("** must also match zero segments")
	}
	if Match("services/**/.env", "web/api/.env") {
		t.Fatal("literal prefix before ** must still anchor")
	}
}

func TestMatchAnchoredPatternOnlyAtRoot(t *testing.T) {
	if !Match("config/dev.local", "config/dev.local") {
		t.Fatal("literal anchored pattern must match itself")
	}
	if Match("config/dev.local", "nested/config/dev.local") {
		t.Fatal("anchored pattern must not float to other depths")
	}
}

func TestMatchStarBacktracking(t *testing.T) {
	// Forces the iterative matcher to re-expand an earlier star.
	if !Match("*a*b", "xaXaYb") {
		t.Fatal("backtracking star match failed")
	}
	if Match("*a*b", "xaXaYc") {
		t.Fatal("must fail when the suffix never appears")
	}
}

func TestMatchEdgeCases(t *testing.T) {
	if !Match("/config/dev.local/", "config/dev.local") {
		t.Fatal("surrounding slashes in patterns must be tolerated")
	}
	if Match("", ".env") {
		t.Fatal("empty pattern must not match")
	}
}
