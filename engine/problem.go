package engine

// ProblemFromDecision renders the RFC 9457 document a caller receives. It lives with the
// engine rather than in an adapter so that every channel produces the same body.
func ProblemFromDecision(d Decision, instance, correlationID string) *Verdict {
	if d.OK() {
		return nil
	}
	digest := ""
	if len(d.Violations) > 0 {
		digest = d.Violations[0].PackDigest
	}
	return &Verdict{
		Type:          "https://registry.internal/problems/data-quality-violation",
		Title:         "Data quality violation",
		Status:        422,
		Detail:        pluralViolations(len(d.Violations)),
		Instance:      instance,
		CorrelationID: correlationID,
		PackDigest:    digest,
		Violations:    d.Violations,
	}
}

func pluralViolations(n int) string {
	if n == 1 {
		return "1 violation"
	}
	return itoa(n) + " violations"
}
