package engine

import (
	"fmt"
	"sync"
	"time"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
	"cel.dev/cel-go/ext"
)

// Evaluator compiles and runs rule expressions. One environment is shared across rules so
// that compilation is paid once per expression.
//
// The clock is opt-in. A fixture run must be reproducible, so by default `now()` does not
// exist and a rule that reads it fails loudly instead of passing by accident. A runtime that
// decides live traffic injects a clock, which is what makes SEM-CHG-002 and SEM-CHG-003
// enforceable at all.
type Evaluator struct {
	mu   sync.RWMutex
	env  *cel.Env
	prog map[string]cel.Program
}

// NewEvaluator builds the deterministic environment used for fixtures and conformance runs.
func NewEvaluator() (*Evaluator, error) { return newEvaluator(nil) }

// NewRuntimeEvaluator builds an environment where `now()` resolves through clock.
func NewRuntimeEvaluator(clock func() time.Time) (*Evaluator, error) {
	if clock == nil {
		return nil, fmt.Errorf("a runtime evaluator requires a clock")
	}
	return newEvaluator(clock)
}

func newEvaluator(clock func() time.Time) (*Evaluator, error) {
	opts := []cel.EnvOption{
		cel.Variable("this", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("refdata", cel.ListType(cel.DynType)),
		cel.Variable("delta", cel.ListType(cel.DynType)),
		cel.Variable("history", cel.ListType(cel.DynType)),
		ext.Strings(),
	}
	if clock != nil {
		opts = append(opts, cel.Function("now",
			cel.Overload("now", []*cel.Type{}, cel.TimestampType,
				cel.FunctionBinding(func(...ref.Val) ref.Val {
					return types.Timestamp{Time: clock().UTC()}
				}))))
	}

	env, err := cel.NewEnv(opts...)
	if err != nil {
		return nil, err
	}
	return &Evaluator{env: env, prog: map[string]cel.Program{}}, nil
}

// CompileError marks an expression that does not compile. The distinction matters at runtime:
// a rule that cannot compile is broken for every record, while a rule that compiles but cannot
// be evaluated is only unable to answer for this one.
type CompileError struct{ Err error }

func (e *CompileError) Error() string { return e.Err.Error() }
func (e *CompileError) Unwrap() error { return e.Err }

// EvalInput is one record plus whatever provider data the rule may read.
type EvalInput struct {
	This      map[string]any
	Providers map[string]any
}

// EvalOutcome says whether a rule ran and whether it passed. A rule whose `when` guard is
// false has not passed; it did not apply.
type EvalOutcome struct {
	RuleID string
	Ran    bool
	Passed bool
}

// program returns the compiled program for expr, compiling it on first use.
//
// The cache is guarded because the engine is shared across the goroutines that serve requests.
// A plain map here is not a slow path — it is a crash: concurrent writes kill the process, not
// the request.
func (e *Evaluator) program(expr string) (cel.Program, error) {
	e.mu.RLock()
	p, ok := e.prog[expr]
	e.mu.RUnlock()
	if ok {
		return p, nil
	}

	ast, iss := e.env.Compile(expr)
	if iss != nil && iss.Err() != nil {
		return nil, &CompileError{Err: iss.Err()}
	}
	compiled, err := e.env.Program(ast)
	if err != nil {
		return nil, &CompileError{Err: err}
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	// Another goroutine may have compiled the same expression while this one was compiling;
	// either program is equivalent, so reuse the stored one rather than replacing it.
	if existing, ok := e.prog[expr]; ok {
		return existing, nil
	}
	e.prog[expr] = compiled
	return compiled, nil
}

// Warm compiles every expression up front. The first request should not pay for compilation,
// and a broken expression should be found before traffic arrives rather than by the first
// record that reaches the rule.
//
// Failures are returned rather than fatal: the deterministic evaluator has no `now()`, so a
// clock-dependent rule legitimately fails to compile there and must still fail loudly when a
// record reaches it. An unsuccessful expression is never cached.
func (e *Evaluator) Warm(rules []Rule) []error {
	var errs []error
	for _, r := range rules {
		for _, expr := range []string{r.When, r.Assert} {
			if expr == "" {
				continue
			}
			if _, err := e.program(expr); err != nil {
				errs = append(errs, fmt.Errorf("rule %s: %w", r.ID, err))
			}
		}
	}
	return errs
}

func (e *Evaluator) evalBool(expr string, vars map[string]any) (bool, error) {
	p, err := e.program(expr)
	if err != nil {
		return false, err
	}
	out, _, err := p.Eval(vars)
	if err != nil {
		return false, err
	}
	b, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("expression returned %T, not a bool", out.Value())
	}
	return b, nil
}

// EvalRule evaluates one rule: the guard first, then the assertion.
func (e *Evaluator) EvalRule(r Rule, in EvalInput) (EvalOutcome, error) {
	vars := map[string]any{
		"this":    in.This,
		"refdata": listOrEmpty(in.Providers["refdata"]),
		"delta":   listOrEmpty(in.Providers["delta"]),
		"history": listOrEmpty(in.Providers["history"]),
	}
	out := EvalOutcome{RuleID: r.ID}

	if r.When != "" {
		ok, err := e.evalBool(r.When, vars)
		if err != nil {
			return out, fmt.Errorf("when: %w", err)
		}
		if !ok {
			return out, nil // did not apply
		}
	}
	out.Ran = true

	ok, err := e.evalBool(r.Assert, vars)
	if err != nil {
		return out, fmt.Errorf("assert: %w", err)
	}
	out.Passed = ok
	return out, nil
}

// emptyList is shared rather than allocated per rule: most rules read no provider data, and a
// fresh empty slice for each of them is pure garbage. CEL only reads the list.
var emptyList = []any{}

func listOrEmpty(v any) any {
	if v == nil {
		return emptyList
	}
	return v
}

// FixtureResult is the outcome of one fixture.
type FixtureResult struct {
	Pack     string
	PackPath string
	Rule     string
	Expect   string
	Ran      bool
	Passed   bool
	Mismatch bool
	Err      error
}

// RunFixtures executes every fixture in every object pack. An intent pack has no fixtures:
// its input is a composite request, which the fixture format cannot express.
func RunFixtures(m *Model) ([]FixtureResult, error) {
	ev, err := NewEvaluator()
	if err != nil {
		return nil, err
	}

	results := []FixtureResult{}
	for _, p := range m.AllPacks() {
		byID := map[string]Rule{}
		for _, r := range p.Rules() {
			byID[r.ID] = r
		}
		for _, f := range p.Spec.Tests {
			res := FixtureResult{Pack: p.Metadata.Name, PackPath: p.Path, Rule: f.Rule, Expect: f.Expect}
			r, known := byID[f.Rule]
			if !known {
				res.Mismatch = true
				res.Err = fmt.Errorf("fixture refers to %s, which the pack does not define", f.Rule)
				results = append(results, res)
				continue
			}

			out, evalErr := ev.EvalRule(r, splitFixtureInput(f.Input))
			res.Err = evalErr
			if evalErr != nil {
				res.Mismatch = true
			} else {
				res.Ran, res.Passed = out.Ran, out.Passed
				switch f.Expect {
				case "pass":
					res.Mismatch = !out.Passed
				case "fail":
					res.Mismatch = out.Passed
				case "edge":
					// recorded, never counted as a failure: an edge case documents behaviour
				default:
					res.Mismatch = true
					res.Err = fmt.Errorf("unknown expectation %q", f.Expect)
				}
			}
			results = append(results, res)
		}
	}
	return results, nil
}

// splitFixtureInput separates the payload from the provider data a fixture carries under the
// provider's own name.
func splitFixtureInput(input map[string]any) EvalInput {
	this := map[string]any{}
	providers := map[string]any{}
	for k, v := range input {
		switch k {
		case "refdata", "delta", "history":
			providers[k] = v
		default:
			this[k] = v
		}
	}
	return EvalInput{This: this, Providers: providers}
}
