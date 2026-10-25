// Package dqfile is the file-gate adapter: the batch channel. It streams, so a file larger
// than memory is gated in one pass, and it refuses an unverified file before reading a row.
package dqfile

import (
	"bufio"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dennisholee/dq-guardrail/engine"
	"gopkg.in/yaml.v3"
)

// Reject is one rejected record, reported with the row number a data engineer can act on.
type Reject struct {
	Row           int
	CorrelationID string
	Violations    []engine.Violation
}

// Result is what the gate did.
type Result struct {
	Records  int
	Accepted int
	Rejected int
	Rejects  []Reject
	Refused  string // set when the file was refused before any record was read
}

// Run gates one delivery. It returns an error only for toolchain problems; a refused file and
// a rejected record are results, not errors, because both are expected outcomes.
func Run(m *engine.Model, e *engine.Engine, manifestPath, dataPath, outDir string) (Result, error) {
	res := Result{}

	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return res, fmt.Errorf("read manifest: %w", err)
	}
	var manifest engine.FileManifest
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		return res, fmt.Errorf("parse manifest: %w", err)
	}

	plan := m.PlanFor("file", manifest.Intent, manifest.Object)
	if plan == nil {
		plan = m.PlanFor("http", manifest.Intent, manifest.Object)
	}
	if plan == nil {
		res.Refused = fmt.Sprintf("no compiled plan for %s/%s", manifest.Intent, manifest.Object)
		return res, nil
	}

	// Verify before reading a record: an unverified file may be truncated or the wrong file.
	digest, err := fileDigest(dataPath)
	if err != nil {
		return res, err
	}
	if digest != manifest.Checksum.Value {
		res.Refused = fmt.Sprintf("checksum mismatch: manifest says %s, file is sha256:%s", manifest.Checksum.Value, digest)
		return res, nil
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return res, err
	}
	acceptedPath := filepath.Join(outDir, "accepted.jsonl")
	rejectsPath := filepath.Join(outDir, "rejects.csv")

	accepted, err := os.Create(acceptedPath)
	if err != nil {
		return res, err
	}
	defer accepted.Close()

	rejects, err := os.Create(rejectsPath)
	if err != nil {
		return res, err
	}
	defer rejects.Close()

	w := csv.NewWriter(rejects)
	// Column order is the contract in solution-architecture.md §6.4; correlationId is appended
	// so a row can be joined to the verdict and the HTTP evidence for the same record.
	_ = w.Write([]string{"row", "ruleId", "severity", "path", "message", "correlationId"})

	handle := func(record map[string]any) error {
		res.Records++
		decision, err := e.Decide(plan, engine.Request{
			Payload:       record,
			CorrelationID: manifest.DeliveryID,
		})
		if err != nil {
			return err
		}
		if decision.OK() {
			res.Accepted++
			line, _ := json.Marshal(record)
			_, werr := accepted.Write(append(line, '\n'))
			return werr
		}
		res.Rejected++
		res.Rejects = append(res.Rejects, Reject{Row: res.Records, CorrelationID: manifest.DeliveryID, Violations: decision.Violations})
		for _, v := range decision.Violations {
			_ = w.Write([]string{
				fmt.Sprint(res.Records), v.RuleID, v.Severity, v.Path, v.Message, manifest.DeliveryID,
			})
		}
		return nil
	}

	switch manifest.Format.Type {
	case "jsonl":
		err = streamJSONL(dataPath, handle)
	case "csv":
		err = streamCSV(dataPath, manifest, handle)
	default:
		res.Refused = "unsupported format: " + manifest.Format.Type
		return res, nil
	}
	if err != nil {
		return res, err
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return res, err
	}

	// The acceptance marker is written last and only when nothing was rejected: a downstream
	// job that starts on its presence must never see a partially accepted file.
	if res.Rejected == 0 && res.Records == manifest.RecordCount {
		if err := os.WriteFile(filepath.Join(outDir, "_SUCCESS"), []byte("accepted\n"), 0o644); err != nil {
			return res, err
		}
	}
	return res, nil
}

func streamJSONL(path string, handle func(map[string]any) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return fmt.Errorf("record is not a JSON object: %w", err)
		}
		if err := handle(record); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func streamCSV(path string, manifest engine.FileManifest, handle func(map[string]any) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	if manifest.Format.CSV != nil && manifest.Format.CSV.Delimiter != "" {
		r.Comma = rune(manifest.Format.CSV.Delimiter[0])
	}

	header, err := r.Read()
	if err != nil {
		return err
	}
	for {
		row, err := r.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		record := make(map[string]any, len(header))
		for i, key := range header {
			if i < len(row) {
				record[key] = row[i]
			}
		}
		if err := handle(record); err != nil {
			return err
		}
	}
}

func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
