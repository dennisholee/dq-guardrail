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

// Failure is the RFC 9457 document for a platform failure: the gate could not decide. It is
// deliberately not a Violation — a violation says the record is wrong, a failure says we could
// not tell, and conflating them teaches clients either to retry bad data or to quarantine good
// data (Delivery Design Pack §2.6, §2.7).
//
// The shape lives here rather than in an adapter because every channel must render it
// identically; two adapters that invent their own failure bodies cannot pass T4.
type Failure struct {
	Type          string `json:"type"`
	Title         string `json:"title"`
	Status        int    `json:"status"`
	Detail        string `json:"detail"`
	Instance      string `json:"instance"`
	CorrelationID string `json:"correlationId"`
	Code          string `json:"code"`
}

// FailureProblem renders one platform failure. code is the §2.6 code (DQ_PAYLOAD_TOO_LARGE and
// friends) and is repeated in the document so the wire response and the dq.failures metric label
// name the same thing.
func FailureProblem(status int, code, title, detail, instance, correlationID string) *Failure {
	return &Failure{
		Type:          "https://registry.internal/problems/" + problemSlug(code),
		Title:         title,
		Status:        status,
		Detail:        detail,
		Instance:      instance,
		CorrelationID: correlationID,
		Code:          code,
	}
}

// problemSlug turns DQ_UNSUPPORTED_MEDIA_TYPE into dq-unsupported-media-type.
func problemSlug(code string) string {
	out := make([]byte, 0, len(code))
	for i := 0; i < len(code); i++ {
		switch c := code[i]; {
		case c == '_':
			out = append(out, '-')
		case c >= 'A' && c <= 'Z':
			out = append(out, c+('a'-'A'))
		default:
			out = append(out, c)
		}
	}
	return string(out)
}

func pluralViolations(n int) string {
	if n == 1 {
		return "1 violation"
	}
	return itoa(n) + " violations"
}
