package dqkafka

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/dennisholee/dq-guardrail/engine"
)

const sampleDir = "../../../examples/party-model"

func gateAt(t *testing.T, mode engine.Mode) *Gate {
	t.Helper()
	m, err := engine.Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	e, err := engine.NewEngineWithClock(m, func() time.Time {
		return time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	return &Gate{Engine: e, Mode: mode}
}

func record(t *testing.T, payload map[string]any) Record {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return Record{
		Topic: "customer.onboard.v1",
		Value: body,
		Headers: map[string]string{
			HeaderIntent:      "onboard",
			HeaderObject:      "party",
			HeaderCorrelation: "01J8Z6Q4T7X9K2M3N4P5Q6R7S8",
		},
	}
}

func validPayload() map[string]any {
	return map[string]any{
		"party": map[string]any{
			"partyId": "PTY-00000001",
			"individual": map[string]any{"names": []any{
				map[string]any{"type": "FIRST", "value": "Amara"},
				map[string]any{"type": "LAST", "value": "Okafor"},
			}},
		},
		"relationship": map[string]any{"type": "CUSTOMER", "lineOfBusiness": "RETAIL"},
		"contactPoints": []any{map[string]any{"partyId": "PTY-00000001", "contacts": []any{
			map[string]any{"type": "CORRESPONDENCE", "address": map[string]any{
				"street1": "14 Marina Road", "city": "Lagos", "postalCode": "101241", "country": "NG"}},
		}}},
	}
}

func badPayload() map[string]any {
	p := validPayload()
	p["party"].(map[string]any)["individual"] = map[string]any{"names": []any{
		map[string]any{"type": "LAST", "value": "Okafor"},
	}}
	return p
}

// TestKafkaPassesAValidRecord: the record goes on unchanged, with the digest that decided it.
func TestKafkaPassesAValidRecord(t *testing.T) {
	out, err := gateAt(t, engine.ModeEnforce).Intercept(record(t, validPayload()))
	if err != nil {
		t.Fatalf("intercept: %v", err)
	}
	if out.Action != ActionPass || out.Topic != "customer.onboard.v1" {
		t.Fatalf("action=%s topic=%s", out.Action, out.Topic)
	}
	if out.Headers[HeaderPackDigest] == "" {
		t.Fatal("a passed record must carry the pack digest that decided it")
	}
}

// TestKafkaRoutesToDLQInEnforce: a stream has no response, so a CRITICAL violation becomes a
// dead letter rather than a rejection.
func TestKafkaRoutesToDLQInEnforce(t *testing.T) {
	out, err := gateAt(t, engine.ModeEnforce).Intercept(record(t, badPayload()))
	if err != nil {
		t.Fatalf("intercept: %v", err)
	}
	if out.Action != ActionDLQ || out.Topic != "customer.onboard.v1.dlq" {
		t.Fatalf("action=%s topic=%s", out.Action, out.Topic)
	}
	if out.Headers[HeaderCount] != "1" {
		t.Fatalf("violation count header %q", out.Headers[HeaderCount])
	}

	raw, err := base64.StdEncoding.DecodeString(out.Headers[HeaderViolations])
	if err != nil {
		t.Fatalf("dq.violations is not base64: %v", err)
	}
	var violations []engine.Violation
	if err := json.Unmarshal(raw, &violations); err != nil {
		t.Fatalf("dq.violations is not the violation array: %v", err)
	}
	if len(violations) != 1 || violations[0].RuleID != "SEM-PTY-002" {
		t.Fatalf("unexpected violations: %+v", violations)
	}
}

// TestKafkaAnnotatesInWarn: the design's "MINOR annotates and forwards" behaviour.
func TestKafkaAnnotatesInWarn(t *testing.T) {
	out, err := gateAt(t, engine.ModeWarn).Intercept(record(t, badPayload()))
	if err != nil {
		t.Fatalf("intercept: %v", err)
	}
	if out.Action != ActionAnnotate || out.Topic != "customer.onboard.v1" {
		t.Fatalf("action=%s topic=%s", out.Action, out.Topic)
	}
	if out.Headers[HeaderWarn] != "1" {
		t.Fatal("warn mode must set dq.warn=1")
	}
}

// TestKafkaShadowForwardsUnchanged: shadow measures without changing the topic.
func TestKafkaShadowForwardsUnchanged(t *testing.T) {
	out, err := gateAt(t, engine.ModeShadow).Intercept(record(t, badPayload()))
	if err != nil {
		t.Fatalf("intercept: %v", err)
	}
	if out.Topic != "customer.onboard.v1" || out.Headers[HeaderShadow] != "1" {
		t.Fatalf("topic=%s headers=%v", out.Topic, out.Headers)
	}
}

// TestKafkaForwardsUnaddressed: a record with no dq.intent/dq.object cannot be decided, and
// forwarding is what the design requires when coverage is absent.
func TestKafkaForwardsUnaddressed(t *testing.T) {
	r := record(t, badPayload())
	delete(r.Headers, HeaderIntent)
	out, err := gateAt(t, engine.ModeEnforce).Intercept(r)
	if err != nil {
		t.Fatalf("intercept: %v", err)
	}
	if out.Action != ActionPass || out.Topic != "customer.onboard.v1" {
		t.Fatalf("action=%s topic=%s", out.Action, out.Topic)
	}
}

// TestKafkaForwardsNonJSON: the gate is not the schema registry for the topic.
func TestKafkaForwardsNonJSON(t *testing.T) {
	r := record(t, validPayload())
	r.Value = []byte("not json")
	out, err := gateAt(t, engine.ModeEnforce).Intercept(r)
	if err != nil {
		t.Fatalf("intercept: %v", err)
	}
	if out.Action != ActionPass {
		t.Fatalf("action=%s", out.Action)
	}
}
