package engine

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Mode is the binding's enforcement mode, mirrored here so the library can behave like the
// adapter that will host it.
type Mode string

const (
	ModeOff     Mode = "OFF"
	ModeShadow  Mode = "SHADOW"
	ModeWarn    Mode = "WARN"
	ModeEnforce Mode = "ENFORCE"
)

// Request is one record to decide.
type Request struct {
	Intent        string
	Object        string
	Payload       map[string]any
	Providers     map[string]any
	CorrelationID string
}

// Decision is the outcome of evaluating one record against one plan.
type Decision struct {
	Intent        string
	Object        string
	ObjectVersion string
	Violations    []Violation
	Structural    int // violations produced by tier 0
	RulesRun      int
	Relaxed       []string // rules that failed but are waived by this plan
}

// OK reports whether the record may pass.
func (d Decision) OK() bool { return len(d.Violations) == 0 }

// Engine owns the compiled artefacts needed to decide a record: the tier-0 schemas, the CEL
// evaluator, and the authored rules with their pack digests.
type Engine struct {
	model   *Model
	records *RecordValidator
	eval    *Evaluator
	rules   map[string]*Rule
	packs   map[string]*Pack
	objCode map[string]string
	objPack map[string]string
}

// NewEngine compiles everything needed to decide records for this tree, with no clock: rules
// that read `now()` cannot be evaluated, which is the right default for conformance runs.
func NewEngine(m *Model) (*Engine, error) {
	return newEngine(m, NewEvaluator)
}

// NewEngineWithClock builds an engine whose `now()` resolves through clock — the runtime
// configuration.
func NewEngineWithClock(m *Model, clock func() time.Time) (*Engine, error) {
	return newEngine(m, func() (*Evaluator, error) { return NewRuntimeEvaluator(clock) })
}

func newEngine(m *Model, newEval func() (*Evaluator, error)) (*Engine, error) {
	records, err := NewRecordValidator(m)
	if err != nil {
		return nil, err
	}
	eval, err := newEval()
	if err != nil {
		return nil, err
	}

	e := &Engine{
		model:   m,
		records: records,
		eval:    eval,
		rules:   m.RuleIndex(),
		packs:   m.PackByName(),
		objCode: map[string]string{},
		objPack: map[string]string{},
	}
	for _, p := range m.ObjectPacks {
		e.objCode[p.Spec.Object.Name] = ObjectCode(m, p.Spec.Object.Name)
		e.objPack[p.Spec.Object.Name] = p.Metadata.Name
	}

	// Compile every expression now. A gate should not compile CEL on the request path, and a
	// rule that does not compile should be known before traffic, not by the first record that
	// reaches it. Failures are deliberately not fatal here: a clock-dependent rule cannot
	// compile against the deterministic evaluator, and it must still fail when reached.
	rules := make([]Rule, 0, len(e.rules))
	for _, r := range e.rules {
		rules = append(rules, *r)
	}
	_ = eval.Warm(rules)

	return e, nil
}

// PlanFor returns the compiled plan for a channel, intent and object, so an adapter need not
// hold the model separately.
func (e *Engine) PlanFor(channel, intent, object string) *Plan {
	return e.model.PlanFor(channel, intent, object)
}

// PackDigestFor returns the digest of the object pack that governs an object. It is available
// even when a record passes, which is what lets a consumer see which rules decided it.
func (e *Engine) PackDigestFor(object string) string {
	return packDigestOf(e.packs[e.objPack[object]])
}

// Decide evaluates one record. Tier 0 runs first and always: a record that does not match the
// contract is not worth evaluating semantically, and reporting both would bury the structural
// fault under consequences of it.
//
// A composite request — one that carries the object inside a named member, as /onboard-party
// does — is bound so that object rules see the object and intent rules see the whole request.
// That distinction is already declared per rule as `origin`, so no heuristic is involved.
func (e *Engine) Decide(plan *Plan, req Request) (Decision, error) {
	d := Decision{Intent: plan.Intent, Object: plan.Object, ObjectVersion: plan.ObjectVersion}

	objectPayload := req.Payload
	if sub, ok := req.Payload[plan.Object].(map[string]any); ok {
		objectPayload = sub
	}

	problems, err := e.records.Validate(plan.Object, objectPayload)
	if err != nil {
		return d, err
	}
	for _, p := range problems {
		d.Violations = append(d.Violations, e.structuralViolation(plan, p, req.CorrelationID))
		d.Structural++
	}
	if d.Structural > 0 {
		return d, nil
	}

	relaxed := map[string]bool{}
	for _, rx := range plan.Relax {
		relaxed[rx.Rule] = true
	}

	for _, pr := range plan.Rules {
		src, ok := e.rules[pr.ID]
		if !ok {
			continue
		}
		scope := objectPayload
		if pr.Origin == "intent" {
			scope = req.Payload
		}
		out, err := e.eval.EvalRule(*src, EvalInput{This: scope, Providers: req.Providers})
		if err != nil {
			// An expression that does not compile is a defect in the pack: it cannot work for
			// any record, so it fails the run rather than the record. An expression that
			// compiles but cannot be evaluated against this record is a property of the data,
			// and the record fails closed rather than becoming a 500 at the gate.
			var compileErr *CompileError
			if errors.As(err, &compileErr) {
				return d, fmt.Errorf("rule %s: %w", pr.ID, err)
			}
			// The CEL error text can embed operand values, so it is not published; the rule id
			// and the path are enough to locate the defect, and the message says what happened.
			d.Violations = append(d.Violations, e.evaluationViolation(plan, src, req.CorrelationID))
			continue
		}
		if !out.Ran {
			continue
		}
		d.RulesRun++
		if out.Passed {
			continue
		}
		// A relaxed rule that fails is recorded, not enforced: the waiver is what makes an
		// otherwise-blocked business process possible, and it must still be visible.
		if relaxed[pr.ID] {
			d.Relaxed = append(d.Relaxed, pr.ID)
			continue
		}
		d.Violations = append(d.Violations, e.ruleViolation(plan, src, req.CorrelationID))
	}
	return d, nil
}

// structuralViolation names a contract failure with a reserved rule id: the last segment is
// 000, which authored rules may not use, so a structural failure is never mistaken for a rule
// somebody wrote.
func (e *Engine) structuralViolation(plan *Plan, p StructureProblem, correlationID string) Violation {
	code := e.objCode[plan.Object]
	if code == "" {
		code = "OBJ"
	}
	pack := e.packs[e.objPack[plan.Object]]

	return Violation{
		RuleID:        fmt.Sprintf("SYN-%s-000", code),
		Severity:      SeverityViolationCritical,
		Path:          pointerToJSONPath(p.Path),
		Dimension:     "validity",
		Intent:        plan.Intent,
		Object:        plan.Object,
		ObjectVersion: plan.ObjectVersion,
		PackDigest:    packDigestOf(pack),
		CorrelationID: correlationID,
		Message:       clamp("does not match the canonical contract: "+p.Message, 200),
		Retryable:     false,
	}
}

// evaluationViolation reports a rule that compiled but could not run against this record. It
// fails closed: a record whose rules could not be evaluated has not been proven good, and a
// gate that passes it is a gate that reports success for work it did not do.
func (e *Engine) evaluationViolation(plan *Plan, r *Rule, correlationID string) Violation {
	pack := e.packs[r.PackName]
	path := r.JSONPath
	if path == "" {
		path = "$"
	}
	return Violation{
		RuleID:        r.ID,
		Severity:      SeverityViolationCritical,
		Path:          path,
		Dimension:     r.Dimension,
		Intent:        plan.Intent,
		Object:        plan.Object,
		ObjectVersion: plan.ObjectVersion,
		PackDigest:    packDigestOf(pack),
		CorrelationID: correlationID,
		Message:       clamp("the rule could not be evaluated against this record, so the record cannot pass", 200),
		Retryable:     false,
	}
}

func (e *Engine) ruleViolation(plan *Plan, r *Rule, correlationID string) Violation {
	pack := e.packs[r.PackName]
	path := r.JSONPath
	if path == "" {
		path = "$"
	}
	return Violation{
		RuleID:        r.ID,
		Severity:      r.Severity,
		Path:          path,
		Dimension:     r.Dimension,
		Intent:        plan.Intent,
		Object:        plan.Object,
		ObjectVersion: plan.ObjectVersion,
		PackDigest:    packDigestOf(pack),
		CorrelationID: correlationID,
		Message:       clamp(r.Message, 200),
		Retryable:     false,
	}
}

func packDigestOf(p *Pack) string {
	if p == nil {
		return ""
	}
	return p.Digest
}

// pointerToJSONPath turns a JSON pointer into the $ path the violation schema requires:
// /individual/names/0 becomes $.individual.names[0].
func pointerToJSONPath(pointer string) string {
	if pointer == "" || pointer == "/" {
		return "$"
	}
	var b strings.Builder
	b.WriteString("$")
	for _, seg := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		seg = strings.ReplaceAll(seg, "~1", "/")
		seg = strings.ReplaceAll(seg, "~0", "~")
		if _, err := strconv.Atoi(seg); err == nil {
			b.WriteString("[" + seg + "]")
			continue
		}
		b.WriteString("." + seg)
	}
	return b.String()
}

func clamp(s string, max int) string {
	// The fast path avoids converting to runes for the case that actually occurs: a message
	// that already fits. Byte length bounds rune length, so if the bytes fit, so do the runes.
	if len(s) <= max {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

// Violation severities, spelled as the violation schema requires. The finding severities above
// are the CLI's vocabulary; these are the data plane's.
const (
	SeverityViolationCritical = "CRITICAL"
	SeverityViolationMajor    = "MAJOR"
	SeverityViolationMinor    = "MINOR"
)
