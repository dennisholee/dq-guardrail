package engine

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestDecisionAllocationBudget guards the shape of the hot path rather than its exact speed:
// allocations are deterministic across machines, wall-clock is not, so this is safe to run in
// CI. The budget is deliberately loose — roughly twice the current 201 — because it exists to
// catch an order-of-magnitude regression such as compiling CEL per request (which costs
// ~240µs and hundreds of allocations per expression) or re-marshalling the payload per rule.
//
// The wall-clock claim lives in two documents rather than here: the delivery design pack budgets
// ~100µs of added overhead and data-plane-operations-guide.yml alarms at p99 > 1ms
// (DQ_LatencyBudgetBreach). A mean of 13.6µs on a synthetic payload is consistent with both, but
// a mean is not a p99 and a laptop is not production — that comparison needs a load test.
func TestDecisionAllocationBudget(t *testing.T) {
	m, err := Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	e, err := NewEngineWithClock(m, func() time.Time {
		return time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	plan := m.PlanFor("http", "onboard", "party")
	if plan == nil {
		t.Fatal("no plan")
	}

	raw, err := os.ReadFile(sampleDir + "/evidence/scenarios/onboard-surname-only.json")
	if err != nil {
		t.Fatalf("read scenario: %v", err)
	}
	var sc struct {
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(raw, &sc); err != nil {
		t.Fatalf("decode scenario: %v", err)
	}

	const budget = 500
	allocs := testing.AllocsPerRun(50, func() {
		if _, err := e.Decide(plan, Request{
			Payload:       sc.Payload,
			CorrelationID: "01J8Z6Q4T7X9K2M3N4P5Q6R7S8",
		}); err != nil {
			t.Fatalf("decide: %v", err)
		}
	})
	if allocs > budget {
		t.Fatalf("a decision allocates %.0f times; the budget is %d. Something on the hot path "+
			"started doing per-request or per-rule work that belongs at construction.", allocs, budget)
	}
	t.Logf("a decision allocates %.0f times (budget %d)", allocs, budget)
}
