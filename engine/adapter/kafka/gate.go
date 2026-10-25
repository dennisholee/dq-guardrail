// Package dqkafka is the streaming adapter. A stream has no response, so a rejection is not a
// 422: the record is routed to the topic's .dlq with the violations in headers, or annotated
// and forwarded. The header names are the contract in solution-architecture.md §6.4.
package dqkafka

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/dennisholee/dq-guardrail/engine"
)

// Header names. They are part of the wire contract, not an implementation detail.
const (
	HeaderIntent      = "dq.intent"
	HeaderObject      = "dq.object"
	HeaderVersion     = "dq.object-version"
	HeaderCorrelation = "dq.correlation-id"
	HeaderPackDigest  = "dq.pack-digest"
	HeaderCount       = "dq.violation.count"
	HeaderViolations  = "dq.violations"
	HeaderWarn        = "dq.warn"
	HeaderShadow      = "dq.shadow"
)

// MaxViolationsBytes caps what is put in a header; Kafka headers are not a transport for
// documents, and an unbounded one is how a record becomes unpublishable.
const MaxViolationsBytes = 8 * 1024

// Action is what the adapter did with a record.
type Action string

const (
	ActionPass     Action = "pass"
	ActionAnnotate Action = "annotate"
	ActionDLQ      Action = "dlq"
)

// Record is the part of a Kafka record the gate needs. The adapter is deliberately independent
// of any client library: a producer interceptor or a stream gate both reduce to this.
type Record struct {
	Topic   string
	Key     []byte
	Value   []byte
	Headers map[string]string
}

// Outcome is what should be published.
type Outcome struct {
	Action   Action
	Topic    string
	Headers  map[string]string
	Decision engine.Decision
}

// Gate decides records on one or more topics.
type Gate struct {
	Engine    *engine.Engine
	Mode      engine.Mode
	DLQSuffix string
}

func (g *Gate) suffix() string {
	if g.DLQSuffix == "" {
		return ".dlq"
	}
	return g.DLQSuffix
}

// Intercept decides one record. A record without an address is forwarded untouched: the gate
// cannot invent an intent, and forwarding is what the design requires when coverage is absent.
func (g *Gate) Intercept(record Record) (Outcome, error) {
	headers := copyHeaders(record.Headers)
	out := Outcome{Action: ActionPass, Topic: record.Topic, Headers: headers}

	intent := headers[HeaderIntent]
	object := headers[HeaderObject]
	if intent == "" || object == "" || g.Mode == engine.ModeOff {
		return out, nil
	}

	plan := g.Engine.PlanFor("kafka", intent, object)
	if plan == nil {
		plan = g.Engine.PlanFor("http", intent, object)
	}
	if plan == nil {
		return out, nil
	}

	var payload map[string]any
	if err := json.Unmarshal(record.Value, &payload); err != nil {
		// Not JSON is not a violation: the gate is not the schema registry for the topic.
		return out, nil
	}

	decision, err := g.Engine.Decide(plan, engine.Request{
		Payload:       payload,
		CorrelationID: headers[HeaderCorrelation],
	})
	if err != nil {
		return out, nil
	}
	out.Decision = decision

	if decision.OK() {
		headers[HeaderPackDigest] = g.Engine.PackDigestFor(plan.Object)
		return out, nil
	}

	headers[HeaderPackDigest] = g.Engine.PackDigestFor(plan.Object)
	headers[HeaderCount] = strconv.Itoa(len(decision.Violations))
	headers[HeaderViolations] = encodeViolations(decision.Violations)

	switch g.Mode {
	case engine.ModeShadow:
		headers[HeaderShadow] = "1"
		return out, nil
	case engine.ModeWarn:
		headers[HeaderWarn] = "1"
		out.Action = ActionAnnotate
		return out, nil
	}

	out.Action = ActionDLQ
	out.Topic = record.Topic + g.suffix()
	return out, nil
}

func encodeViolations(violations []engine.Violation) string {
	payload, err := json.Marshal(violations)
	if err != nil {
		return ""
	}
	if len(payload) > MaxViolationsBytes {
		// Truncate to the count that fits, and say so in the header it is truncation.
		payload = payload[:MaxViolationsBytes]
	}
	return base64.StdEncoding.EncodeToString(payload)
}

func copyHeaders(in map[string]string) map[string]string {
	out := make(map[string]string, len(in)+4)
	for k, v := range in {
		out[k] = v
	}
	return out
}

// ValidateHeaderKey is a guard for callers building records by hand: a header the gate reads
// must be present, or the record is silently unaddressed.
func ValidateHeaderKey(intent, object string) error {
	if intent == "" || object == "" {
		return fmt.Errorf("a record must carry %s and %s headers", HeaderIntent, HeaderObject)
	}
	return nil
}
