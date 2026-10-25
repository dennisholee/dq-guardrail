package dqfile

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dennisholee/dq-guardrail/engine"
)

const sampleDir = "../../../examples/party-model"

func setup(t *testing.T) (*engine.Model, *engine.Engine) {
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
	return m, e
}

// TestGateRejectsOneRecord is the acceptance case: the sample delivery has one individual with
// no given name, so two records are accepted, one is rejected, and no marker is written.
func TestGateRejectsOneRecord(t *testing.T) {
	m, e := setup(t)
	out := t.TempDir()

	res, err := Run(m, e,
		filepath.Join(sampleDir, "evidence/deliveries/onboard-party-20261020.manifest.json"),
		filepath.Join(sampleDir, "evidence/deliveries/onboard-party-20261020.jsonl"),
		out)
	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	if res.Refused != "" {
		t.Fatalf("the file should not be refused: %s", res.Refused)
	}
	if res.Records != 3 || res.Accepted != 2 || res.Rejected != 1 {
		t.Fatalf("records=%d accepted=%d rejected=%d, want 3/2/1", res.Records, res.Accepted, res.Rejected)
	}
	if res.Rejects[0].Violations[0].RuleID != "SEM-PTY-002" {
		t.Fatalf("expected SEM-PTY-002, got %s", res.Rejects[0].Violations[0].RuleID)
	}

	if _, err := os.Stat(filepath.Join(out, "_SUCCESS")); !os.IsNotExist(err) {
		t.Fatal("no marker may be written when a record was rejected")
	}
	rejects, err := os.ReadFile(filepath.Join(out, "rejects.csv"))
	if err != nil {
		t.Fatalf("rejects.csv: %v", err)
	}
	if !strings.Contains(string(rejects), "SEM-PTY-002") || !strings.Contains(string(rejects), "3,SEM-PTY-002") {
		t.Fatalf("rejects.csv does not name the failing row: %s", rejects)
	}
}

// TestGateRefusesBeforeReading: a checksum mismatch must stop the gate before a single record
// is read, because a partial read of an unverified file is worse than no read.
func TestGateRefusesBeforeReading(t *testing.T) {
	m, e := setup(t)
	dir := t.TempDir()

	raw, err := os.ReadFile(filepath.Join(sampleDir, "evidence/deliveries/onboard-party-20261020.manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	corrupted := strings.Replace(string(raw),
		"9c538173a554c8ce57ba5c6f84c5b0487243f3e9676dcb3a50f891e32cd3e367",
		"0000000000000000000000000000000000000000000000000000000000000000", 1)
	manifestPath := filepath.Join(dir, "tampered.manifest.json")
	if err := os.WriteFile(manifestPath, []byte(corrupted), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Run(m, e, manifestPath,
		filepath.Join(sampleDir, "evidence/deliveries/onboard-party-20261020.jsonl"),
		filepath.Join(dir, "out"))
	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	if res.Refused == "" {
		t.Fatal("a checksum mismatch must refuse the file")
	}
	if res.Records != 0 {
		t.Fatalf("no record may be read after a refusal, read %d", res.Records)
	}
}

// TestGateWritesSuccessWhenClean: the marker appears only when every record passed and the
// count matches the manifest, which is what a downstream job is allowed to wait on.
func TestGateWritesSuccessWhenClean(t *testing.T) {
	m, e := setup(t)
	dir := t.TempDir()

	record := map[string]any{
		"party": map[string]any{
			"partyId": "PTY-00000201",
			"individual": map[string]any{"names": []any{
				map[string]any{"type": "FIRST", "value": "Amara"},
				map[string]any{"type": "LAST", "value": "Okafor"},
			}},
		},
		"relationship": map[string]any{"type": "CUSTOMER", "lineOfBusiness": "RETAIL"},
		"contactPoints": []any{map[string]any{"partyId": "PTY-00000201", "contacts": []any{
			map[string]any{"type": "CORRESPONDENCE", "address": map[string]any{
				"street1": "14 Marina Road", "city": "Lagos", "postalCode": "101241", "country": "NG"}},
		}}},
	}
	line, _ := json.Marshal(record)
	data := append(line, '\n')
	dataPath := filepath.Join(dir, "clean.jsonl")
	if err := os.WriteFile(dataPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(data)
	manifest := fmt.Sprintf(`{
  "schemaVersion": "1.0", "intent": "onboard", "object": "party", "objectVersion": "1.0.0",
  "deliveryId": "01J9B2M4X8Q7R6S5T4V3W2X1Y0", "producedAt": "2026-10-20T02:15:00Z",
  "source": {"system": "test"}, "format": {"type": "jsonl", "encoding": "utf-8"},
  "recordCount": 1, "checksum": {"algorithm": "sha256", "value": "%x"}
}`, sum)
	manifestPath := filepath.Join(dir, "clean.manifest.json")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "out")
	res, err := Run(m, e, manifestPath, dataPath, out)
	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	if res.Refused != "" || res.Accepted != 1 || res.Rejected != 0 {
		t.Fatalf("refused=%q accepted=%d rejected=%d", res.Refused, res.Accepted, res.Rejected)
	}
	if _, err := os.Stat(filepath.Join(out, "_SUCCESS")); err != nil {
		t.Fatal("a clean delivery must write the acceptance marker")
	}
}
