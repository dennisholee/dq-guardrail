package engine

import "sort"

// Severity follows Delivery Design Pack §3.5. Only SeverityError fails a build.
type Severity string

const (
	SeverityError Severity = "error"
	SeverityWarn  Severity = "warn"
	SeverityInfo  Severity = "info"
)

// Finding codes are stable identifiers from the code registry and are never renumbered
// (DDP §3.5). A new check gets a new code; an existing code never changes meaning.
const (
	CodeParse           = "DQ_DOC_PARSE"
	CodeSchema          = "DQ_SCHEMA_VIOLATION"
	CodeRefUnresolved   = "DQ_REF_UNRESOLVED"
	CodeVocabulary      = "DQ_CONTRACT_VOCABULARY"
	CodeCombinator      = "DQ_CONTRACT_COMBINATOR"
	CodeTemporal        = "DQ_TEMPORAL_UNQUOTED"
	CodeVersionDrift    = "DQ_VERSION_DRIFT"
	CodeIntentUnknown   = "DQ_INTENT_UNKNOWN"
	CodeRuleID          = "DQ_RULE_ID"
	CodeRuleCode        = "DQ_RULE_CODE"
	CodeRulePath        = "DQ_RULE_PATH"
	CodeFixtureMissing  = "DQ_FIXTURE_MISSING"
	CodeFixtureOrphan   = "DQ_FIXTURE_ORPHAN"
	CodeFixtureProvider = "DQ_FIXTURE_PROVIDER"
	CodeFixtureFailed   = "DQ_FIXTURE_FAILED"
	CodePlanDrift       = "DQ_PLAN_DRIFT"
	CodeDigestMismatch  = "DQ_DIGEST_MISMATCH"
	CodeChainUnknown    = "DQ_CHAIN_UNKNOWN"
	CodeChainMismatch   = "DQ_CHAIN_MISMATCH"
	CodeBindingPath     = "DQ_BINDING_PATH"
	CodeBindingPlan     = "DQ_BINDING_PLAN"
	CodeSuppressUnknown = "DQ_SUPPRESS_UNKNOWN"
	CodeSuppressWindow  = "DQ_SUPPRESS_WINDOW"
	CodeVerdictInvalid  = "DQ_VERDICT_INVALID"
	CodeCoverageGap     = "DQ_COVERAGE_GAP"
	CodeAPIKey          = "DQ_API_ADDRESS_KEY"
	CodeL1NotReferenced = "DQ_L1_NOT_REFERENCED"
	CodeL1Restated      = "DQ_L1_RESTATED"
	CodeL3Shadowed      = "DQ_L3_SHADOWED_FIELD"
	CodeL4RuleField     = "DQ_L4_RULE_FIELD_MISSING"
	CodeL4Uncovered     = "DQ_L4_FIELD_UNCOVERED"
	CodeRegistryMissing = "DQ_REGISTRY_MISSING"
	CodeRelaxUnknown    = "DQ_RELAX_UNKNOWN"
	CodeRelaxExpired    = "DQ_RELAX_EXPIRED"
	CodeS2LiteralEnum   = "DQ_S2_LITERAL_ENUM"
	CodeGeneratedStale  = "DQ_GENERATED_STALE"
	CodeGateRefused     = "DQ_GATE_REFUSED"
	CodeGateRejected    = "DQ_GATE_RECORD_REJECTED"
	CodePatternUnsafe   = "DQ_PATTERN_UNSAFE"
)

// Finding is one result. Path is file-relative and, where useful, carries a fragment:
// "spec/openapi/party-api.yaml#/paths/~1onboard-party".
type Finding struct {
	Code     string   `json:"code"`
	Severity Severity `json:"severity"`
	Path     string   `json:"path"`
	Message  string   `json:"message"`
	RuleID   *string  `json:"ruleId"`
}

// Findings accumulates results in insertion order and sorts once, at the end, so that the
// CLI output is deterministic regardless of map iteration order upstream.
type Findings struct {
	items []Finding
}

func (f *Findings) Add(code string, sev Severity, path, message string) {
	f.items = append(f.items, Finding{Code: code, Severity: sev, Path: path, Message: message})
}

func (f *Findings) AddRule(code string, sev Severity, path, message, ruleID string) {
	id := ruleID
	f.items = append(f.items, Finding{Code: code, Severity: sev, Path: path, Message: message, RuleID: &id})
}

// Error is the common case: an error-severity finding with no rule association.
func (f *Findings) Error(code, path, message string) {
	f.Add(code, SeverityError, path, message)
}

func (f *Findings) Len() int { return len(f.items) }

func (f *Findings) HasErrors() bool {
	for _, i := range f.items {
		if i.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Count returns the number of findings at each severity.
func (f *Findings) Count() map[Severity]int {
	out := map[Severity]int{}
	for _, i := range f.items {
		out[i.Severity]++
	}
	return out
}

// mergeFindings appends one finding set to another, preserving rule associations.
func mergeFindings(dst, src *Findings) {
	for _, x := range src.Items() {
		if x.RuleID != nil {
			dst.AddRule(x.Code, x.Severity, x.Path, x.Message, *x.RuleID)
			continue
		}
		dst.Add(x.Code, x.Severity, x.Path, x.Message)
	}
}

// Items returns a copy sorted by severity, then path, then code — stable across runs.
func (f *Findings) Items() []Finding {
	out := make([]Finding, len(f.items))
	copy(out, f.items)
	rank := map[Severity]int{SeverityError: 0, SeverityWarn: 1, SeverityInfo: 2}
	sort.SliceStable(out, func(i, j int) bool {
		if rank[out[i].Severity] != rank[out[j].Severity] {
			return rank[out[i].Severity] < rank[out[j].Severity]
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Code < out[j].Code
	})
	return out
}
