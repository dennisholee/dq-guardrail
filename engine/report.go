package engine

import (
	"encoding/json"
	"io"
	"time"
)

// Exit codes are part of the CLI contract (DDP §3.5): 1 means the documents are wrong,
// 3 means the toolchain failed. Conflating them makes CI failures unreadable.
const (
	ExitOK        = 0
	ExitFindings  = 1
	ExitToolchain = 3
)

// OutputSchemaVersion is the major version of the JSON document this tool writes. It is
// bumped only for a breaking change to the document shape, never for a new finding code.
const OutputSchemaVersion = "1"

// Report is the single JSON document written to stdout by every command that supports
// --json. Human-readable progress goes to stderr; nothing but JSON goes to stdout.
type Report struct {
	SchemaVersion string             `json:"schemaVersion"`
	OK            bool               `json:"ok"`
	Command       string             `json:"command"`
	Version       string             `json:"version"`
	DurationMs    int64              `json:"durationMs"`
	Findings      []Finding          `json:"findings"`
	Artifacts     map[string]*string `json:"artifacts"`
}

// Reporter accumulates findings and timing for one command invocation.
type Reporter struct {
	command   string
	version   string
	start     time.Time
	findings  *Findings
	artifacts map[string]*string
}

func NewReport(command, version string) *Reporter {
	return &Reporter{
		command:   command,
		version:   version,
		start:     time.Now(),
		findings:  &Findings{},
		artifacts: map[string]*string{},
	}
}

// Findings is the accumulator for this invocation.
func (r *Reporter) Findings() *Findings { return r.findings }

// Artifact records a produced path, e.g. the bundle archive.
func (r *Reporter) Artifact(name, path string) {
	p := path
	r.artifacts[name] = &p
}

// Document finalises the report. It must be called once, immediately before writing.
func (r *Reporter) Document() *Report {
	return &Report{
		SchemaVersion: OutputSchemaVersion,
		OK:            !r.findings.HasErrors(),
		Command:       r.command,
		Version:       r.version,
		DurationMs:    time.Since(r.start).Milliseconds(),
		Findings:      r.findings.Items(),
		Artifacts:     r.artifacts,
	}
}

// WriteJSON writes exactly one JSON document followed by a newline.
func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(r)
}

// ExitCode maps the report to the documented process exit code.
func (r *Report) ExitCode() int {
	if r.OK {
		return ExitOK
	}
	return ExitFindings
}
