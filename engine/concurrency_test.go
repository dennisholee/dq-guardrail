package engine

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestEngineIsConcurrencySafe: the HTTP adapter serves requests concurrently, so Decide is
// called from many goroutines against one Engine. Run with -race.
func TestEngineIsConcurrencySafe(t *testing.T) {
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

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				payload := map[string]any{
					"party": map[string]any{
						"partyId": fmt.Sprintf("PTY-%08d", g*100+i),
						"individual": map[string]any{
							"names": []any{map[string]any{"type": "LAST", "value": "Okafor"}},
						},
					},
				}
				if _, err := e.Decide(plan, Request{
					Payload:       payload,
					CorrelationID: "01J8Z6Q4T7X9K2M3N4P5Q6R7S8",
				}); err != nil {
					errs <- err
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent decide: %v", err)
	}
}
