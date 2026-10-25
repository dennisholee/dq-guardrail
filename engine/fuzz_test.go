package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// FuzzDecideNoLeak asserts one invariant over arbitrary JSON: no value the caller sent appears
// in the decision. Example tests only prove it for the values someone thought to try; a fuzz
// target tries the ones nobody thought of — the number where a string was expected, the array
// where a scalar was expected, the deeply nested object where a flat one was expected.
//
// The seed corpus is the sample's own payloads, so the first run starts where traffic starts.
func FuzzDecideNoLeak(f *testing.F) {
	m, err := Load(sampleDir)
	if err != nil {
		f.Fatalf("load: %v", err)
	}
	e, err := NewEngineWithClock(m, func() time.Time {
		return time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	})
	if err != nil {
		f.Fatalf("engine: %v", err)
	}
	plan := m.PlanFor("http", "onboard", "party")
	if plan == nil {
		f.Fatal("no plan")
	}

	for _, seed := range seedPayloads(f) {
		f.Add([]byte(seed))
	}

	// Authored text is not leaked data. A payload value that happens to equal a word the pack
	// itself writes — an enum code like CUSTOMER, which appears in a rule's message — explains
	// itself. The oracle ignores those, and still catches every value that is only in the
	// payload, which is what a leak means.
	authored := authoredCorpus(m)

	f.Fuzz(func(t *testing.T, data []byte) {
		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			return
		}

		decision, err := e.Decide(plan, Request{
			Payload:       payload,
			CorrelationID: "01J8Z6Q4T7X9K2M3N4P5Q6R7S8",
		})
		if err != nil {
			return
		}
		if !decision.OK() && len(decision.Violations) == 0 {
			t.Fatal("a failing decision carried no violations, so a caller cannot act on it")
		}

		body, err := json.Marshal(ProblemFromDecision(decision, "/onboard-party", "01J8Z6Q4T7X9K2M3N4P5Q6R7S8"))
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		for _, value := range distinctiveStrings(payload) {
			if strings.Contains(authored, value) {
				continue
			}
			if strings.Contains(string(body), value) {
				t.Fatalf("payload value %q escaped into the decision: %s", value, body)
			}
		}
	})
}

// authoredCorpus is every string the packs and plans write, so the oracle can tell a value the
// caller sent from a word the rules already contained.
func authoredCorpus(m *Model) string {
	var b strings.Builder
	for _, pack := range m.AllPacks() {
		for _, r := range pack.Rules() {
			b.WriteString(r.ID)
			b.WriteString(" ")
			b.WriteString(r.Message)
			b.WriteString(" ")
			b.WriteString(r.JSONPath)
			b.WriteString(" ")
			b.WriteString(r.Dimension)
			b.WriteString(" ")
		}
	}
	for _, c := range m.Contracts {
		b.WriteString(c.Object)
		b.WriteString(" ")
	}
	return b.String()
}

// distinctiveStrings returns the payload's string leaves that are long enough to be a
// meaningful leak. Short values collide with ordinary words in a message and would produce
// noise rather than findings.
func distinctiveStrings(node any) []string {
	out := []string{}
	var walk func(any)
	walk = func(n any) {
		switch v := n.(type) {
		case string:
			if len(v) >= 8 {
				out = append(out, v)
			}
		case map[string]any:
			for _, val := range v {
				walk(val)
			}
		case []any:
			for _, val := range v {
				walk(val)
			}
		}
	}
	walk(node)
	return out
}

// seedPayloads reads the sample's scenario payloads as fuzz seeds.
func seedPayloads(f *testing.F) []string {
	f.Helper()
	dir := sampleDir + "/evidence/scenarios"
	entries, err := os.ReadDir(dir)
	if err != nil {
		f.Fatalf("read scenarios: %v", err)
	}
	seeds := []string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			f.Fatalf("read %s: %v", entry.Name(), err)
		}
		var sc struct {
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(raw, &sc); err != nil || len(sc.Payload) == 0 {
			continue
		}
		seeds = append(seeds, string(sc.Payload))
	}
	return seeds
}
