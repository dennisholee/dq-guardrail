package engine

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// BenchmarkDecide measures one decision on the hot path. The gate calls this per request or per
// record, so anything above a millisecond is a throughput ceiling, not a detail.
func BenchmarkDecide(b *testing.B) {
	m, err := Load(sampleDir)
	if err != nil {
		b.Fatalf("load: %v", err)
	}
	e, err := NewEngineWithClock(m, func() time.Time {
		return time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	})
	if err != nil {
		b.Fatalf("engine: %v", err)
	}
	plan := m.PlanFor("http", "onboard", "party")
	if plan == nil {
		b.Fatal("no plan")
	}

	raw, err := os.ReadFile(sampleDir + "/evidence/scenarios/onboard-surname-only.json")
	if err != nil {
		b.Fatalf("read scenario: %v", err)
	}
	var sc struct {
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(raw, &sc); err != nil {
		b.Fatalf("decode scenario: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decision, err := e.Decide(plan, Request{
			Payload:       sc.Payload,
			CorrelationID: "01J8Z6Q4T7X9K2M3N4P5Q6R7S8",
		})
		if err != nil {
			b.Fatalf("decide: %v", err)
		}
		if decision.OK() {
			b.Fatal("the seed payload must fail, or this benchmark measures the fast path only")
		}
	}
}

// BenchmarkCompile measures expression compilation on its own, so the split between compile and
// evaluate is visible rather than guessed at.
func BenchmarkCompile(b *testing.B) {
	m, err := Load(sampleDir)
	if err != nil {
		b.Fatalf("load: %v", err)
	}
	plan := m.PlanFor("http", "onboard", "party")
	if plan == nil {
		b.Fatal("no plan")
	}
	clock := func() time.Time { return time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC) }
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ev, err := NewRuntimeEvaluator(clock)
		if err != nil {
			b.Fatalf("evaluator: %v", err)
		}
		for _, r := range plan.Rules {
			for _, expr := range []string{r.When, r.Assert} {
				if expr == "" {
					continue
				}
				// Compilation is the measurement; the assertion's result is not.
				_, _ = ev.program(expr)
			}
		}
	}
}
