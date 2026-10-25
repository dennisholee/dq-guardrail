package engine

import (
	"fmt"
	"regexp"
	"strings"
)

// A contract pattern is executed against every payload that reaches the gate, and a rule's
// matches() regex against every record on that channel. A pattern that backtracks
// catastrophically does not produce a wrong answer — it produces no answer, at whatever rate
// traffic arrives. This audit is static because the alternative is discovering it in production.

// maxPatternLength bounds a pattern that must run per record.
const maxPatternLength = 512

var matchesLiteralRe = regexp.MustCompile(`matches\('([^']*)'\)`)

// AuditPatterns reports every pattern that is unsafe to run per record.
func AuditPatterns(m *Model) *Findings {
	f := &Findings{}

	for name, c := range m.Contracts {
		for _, p := range contractPatterns(c.Raw) {
			reportPattern(f, c.Path, "contract "+name, p)
		}
	}
	for _, pack := range m.AllPacks() {
		for _, r := range pack.Rules() {
			for _, expr := range []string{r.When, r.Assert} {
				for _, mm := range matchesLiteralRe.FindAllStringSubmatch(expr, -1) {
					reportPattern(f, pack.Path, "rule "+r.ID, mm[1])
				}
			}
		}
	}
	return f
}

func reportPattern(f *Findings, path, owner, pattern string) {
	for _, reason := range patternProblems(pattern) {
		f.Error(CodePatternUnsafe, path, fmt.Sprintf("%s: %s", owner, reason))
	}
}

// contractPatterns collects every "pattern" value in a contract, wherever it sits.
func contractPatterns(node any) []string {
	out := []string{}
	var walk func(any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if p, ok := v["pattern"].(string); ok {
				out = append(out, p)
			}
			for _, val := range v {
				walk(val)
			}
		case []any:
			for _, val := range v {
				walk(val)
			}
		}
	}
	walk(node)
	return out
}

// patternProblems reports why a pattern is unsafe, or nothing when it is fine.
func patternProblems(pattern string) []string {
	problems := []string{}

	if pattern == "" {
		return append(problems, "pattern is empty")
	}
	if len(pattern) > maxPatternLength {
		problems = append(problems, fmt.Sprintf("pattern is %d characters; the limit is %d", len(pattern), maxPatternLength))
	}
	// Anchored matters for correctness before it matters for speed: an unanchored pattern
	// accepts a partial match, which is a validation hole rather than a slow path.
	if !strings.HasPrefix(pattern, "^") || !strings.HasSuffix(pattern, "$") {
		problems = append(problems, "pattern is not anchored with ^ and $, so it matches a substring")
	}
	if hasNestedQuantifier(pattern) {
		problems = append(problems, "pattern nests a quantifier inside another (for example (a+)+), which can backtrack catastrophically")
	}
	return problems
}

// hasNestedQuantifier reports whether a quantified group contains a quantifier of its own.
// It is a static heuristic over the pattern text, not a proof: it catches the shapes that
// actually occur, and every shape it flags is worth rewriting regardless.
func hasNestedQuantifier(pattern string) bool {
	type group struct {
		quantified bool
	}
	stack := []group{}
	escaped := false
	inClass := false

	quantifierAt := func(i int) bool {
		if i >= len(pattern) {
			return false
		}
		switch pattern[i] {
		case '*', '+':
			return true
		case '{':
			return strings.Contains(pattern[i:], "}")
		}
		return false
	}

	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]

		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if inClass {
			if ch == ']' {
				inClass = false
			}
			continue
		}
		switch ch {
		case '[':
			inClass = true
		case '(':
			stack = append(stack, group{})
		case ')':
			if len(stack) == 0 {
				return false
			}
			inner := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			// A group that contains a quantifier and is itself quantified is the shape.
			if inner.quantified && quantifierAt(i+1) {
				return true
			}
			if inner.quantified && len(stack) > 0 {
				stack[len(stack)-1].quantified = true
			}
		case '*', '+':
			if len(stack) > 0 {
				stack[len(stack)-1].quantified = true
			}
		}
	}
	return false
}
