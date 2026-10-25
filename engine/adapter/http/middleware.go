// Package dqhttp is the library adapter: HTTP enforcement without a sidecar, a filter or a
// second deployment target. It is the harness the WASM filter is later validated against, so
// it deliberately does nothing the filter could not do.
package dqhttp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/dennisholee/dq-guardrail/engine"
)

// Config is everything the middleware needs. Plans are keyed by address key, e.g.
// "/onboard-party", which is exactly what the route already is.
type Config struct {
	Engine      *engine.Engine
	Plans       map[string]*engine.Plan
	Mode        engine.Mode
	MaxBody     int64
	OnDecision  func(engine.Decision)
	Correlation func(*http.Request) string
}

// Middleware decides the configured endpoints.
//
// Anything it cannot decide it passes through: an unrouted path, a body that is not JSON, or
// a mode of OFF. Guessing at a payload it could not parse would be worse than letting the
// service answer, which is also what the design requires — the gate reduces coverage when it
// fails, it does not invent violations.
func Middleware(cfg Config) func(http.Handler) http.Handler {
	if cfg.MaxBody == 0 {
		cfg.MaxBody = 1 << 20
	}
	if cfg.Correlation == nil {
		cfg.Correlation = func(r *http.Request) string {
			if v := r.Header.Get("x-correlation-id"); v != "" {
				return v
			}
			return r.Header.Get("Idempotency-Key")
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			plan, ok := cfg.Plans[r.URL.Path]
			if !ok || cfg.Mode == engine.ModeOff || cfg.Engine == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Read one byte past the limit so an oversized body is detected rather than
			// silently truncated. A truncated body is the worst outcome available: it is not
			// evaluated, not rejected, and the service is handed corrupt JSON and blamed for it.
			body, err := io.ReadAll(io.LimitReader(r.Body, cfg.MaxBody+1))
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			if int64(len(body)) > cfg.MaxBody {
				// The gate declines to evaluate a record it cannot hold. It must not damage it:
				// the bytes already read are stitched back onto the rest of the stream.
				r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), r.Body))
				if cfg.Mode == engine.ModeShadow || cfg.Mode == engine.ModeWarn {
					next.ServeHTTP(w, r)
					return
				}
				w.Header().Set("content-type", "application/problem+json")
				w.Header().Set("x-dq-correlation-id", cfg.Correlation(r))
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"type":          "https://registry.internal/problems/record-too-large",
					"title":         "Record too large",
					"status":        http.StatusRequestEntityTooLarge,
					"detail":        fmt.Sprintf("the record exceeds the gate's %d byte limit", cfg.MaxBody),
					"instance":      r.URL.Path,
					"correlationId": cfg.Correlation(r),
				})
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))

			var payload map[string]any
			if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
				next.ServeHTTP(w, r)
				return
			}

			correlationID := cfg.Correlation(r)
			decision, err := cfg.Engine.Decide(plan, engine.Request{
				Payload:       payload,
				CorrelationID: correlationID,
			})
			if err != nil {
				// A rule that cannot be evaluated reduces coverage; it must not reject traffic.
				next.ServeHTTP(w, r)
				return
			}
			if cfg.OnDecision != nil {
				cfg.OnDecision(decision)
			}

			if decision.OK() {
				w.Header().Set("DQ-Pack-Digest", cfg.Engine.PackDigestFor(plan.Object))
				next.ServeHTTP(w, r)
				return
			}

			switch cfg.Mode {
			case engine.ModeShadow:
				w.Header().Set("x-dq-shadow", "1")
				next.ServeHTTP(w, r)
			case engine.ModeWarn:
				w.Header().Set("DQ-Pack-Digest", cfg.Engine.PackDigestFor(plan.Object))
				w.Header().Set("x-dq-violations", strconv.Itoa(len(decision.Violations)))
				w.Header().Set("x-dq-correlation-id", correlationID)
				w.Header().Set("x-dq-warn", "1")
				next.ServeHTTP(w, r)
			default:
				problem := engine.ProblemFromDecision(decision, r.URL.Path, correlationID)
				w.Header().Set("content-type", "application/problem+json")
				w.Header().Set("DQ-Pack-Digest", cfg.Engine.PackDigestFor(plan.Object))
				w.Header().Set("x-dq-correlation-id", correlationID)
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = json.NewEncoder(w).Encode(problem)
			}
		})
	}
}

func readerOf(b []byte) io.Reader { return bytes.NewReader(b) }
