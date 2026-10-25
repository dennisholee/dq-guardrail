package engine

import (
	"fmt"
	"strings"
)

// LintS2 enforces invariant S2: a rule may not restate a governed enumeration.
//
// "Governed" is decided mechanically rather than by shape: a list literal is a violation
// only if it contains a code that a value set already owns. That distinction matters —
// `in ['UNKNOWN', 'N/A']` is a lint guard on placeholder tokens that no value set governs,
// and failing it would push authors to invent a value set for every constant. `country in
// ['GB','NG']`, by contrast, duplicates country-code-iso3166-1 and must fail, because when
// that value set changes the rule silently does not.
//
// The rule language has no symbolic reference to a value set yet, so the fix for a real
// violation is to move the check up to the contract's `$ref` or to add that syntax. This
// scanner is not a CEL front end; replacing it with a cel-go AST walk changes the parsing
// and not the definition.
func LintS2(m *Model, f *Findings) {
	governed := map[string]string{}
	for name, vs := range m.ValueSets {
		for _, code := range vs.Codes() {
			governed[code] = name
		}
	}

	for _, p := range m.AllPacks() {
		for _, r := range p.Rules() {
			for _, expr := range []string{r.When, r.Assert} {
				for _, lits := range stringListLiterals(expr) {
					restated := []string{}
					for _, lit := range lits {
						code := unquote(lit)
						if vs, ok := governed[code]; ok {
							restated = append(restated, code+" ("+vs+")")
						}
					}
					if len(restated) == 0 {
						continue
					}
					f.AddRule(CodeS2LiteralEnum, SeverityError, p.Path,
						fmt.Sprintf("rule %s restates %d governed code(s) in a list: %s — reference the value set instead",
							r.ID, len(restated), joinStrings(restated)), r.ID)
				}
			}
		}
	}
}

func unquote(lit string) string {
	if len(lit) >= 2 && (lit[0] == '\'' || lit[0] == '"') && lit[len(lit)-1] == lit[0] {
		return lit[1 : len(lit)-1]
	}
	return lit
}

// stringListLiterals finds every `['A', 'B']` style list of string literals in expr.
func stringListLiterals(expr string) [][]string {
	out := [][]string{}
	for i := 0; i < len(expr); {
		switch expr[i] {
		case '\'', '"':
			i = skipString(expr, i)
		case '[':
			if items, n, ok := parseStringList(expr[i:]); ok {
				out = append(out, items)
				i += n
			} else {
				i++
			}
		default:
			i++
		}
	}
	return out
}

// skipString returns the index just past the string literal starting at i.
func skipString(expr string, i int) int {
	quote := expr[i]
	i++
	for i < len(expr) {
		if expr[i] == '\\' {
			i += 2
			continue
		}
		if expr[i] == quote {
			return i + 1
		}
		i++
	}
	return i
}

// parseStringList matches `[ 'a' , 'b' ]` at the start of s and returns the items and the
// number of bytes consumed.
func parseStringList(s string) ([]string, int, bool) {
	if len(s) == 0 || s[0] != '[' {
		return nil, 0, false
	}
	i := 1
	items := []string{}
	for {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n') {
			i++
		}
		if i >= len(s) {
			return nil, 0, false
		}
		if s[i] == ']' {
			if len(items) == 0 {
				return nil, 0, false
			}
			return items, i + 1, true
		}
		if s[i] != '\'' && s[i] != '"' {
			return nil, 0, false
		}
		start := i
		i = skipString(s, i)
		items = append(items, s[start:i])

		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n') {
			i++
		}
		if i < len(s) && s[i] == ',' {
			i++
			continue
		}
	}
}

func preview(items []string) string {
	if len(items) <= 3 {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:3], ", ") + ", …"
}
