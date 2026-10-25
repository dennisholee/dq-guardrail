// Package dqhttp is the library adapter: HTTP enforcement without a sidecar, a filter or a
// second deployment target. It is the harness the WASM filter is later validated against, so
// it deliberately does nothing the filter could not do.
package dqhttp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

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
// What it does with a record it cannot decide is a contract, not an implementation detail, and it
// is specified in the Delivery Design Pack §2.7:
//
//   - an unrouted path, or mode OFF, is passed through: there is nothing to decide;
//   - a body that is not application/json, or does not parse, is refused with 415
//     DQ_UNSUPPORTED_MEDIA_TYPE under ENFORCE — passing it on would be a silent pass, and a
//     silent pass is the failure this gate exists to prevent;
//   - a pack that cannot be evaluated is refused with 500 DQ_RULE_ERROR under ENFORCE, for the
//     same reason;
//   - under SHADOW and WARN both of those pass through untouched, because those modes observe
//     rather than enforce.
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
			enforce := cfg.Mode == engine.ModeEnforce

			// A declared Content-Type that is not JSON is refused before the body is read. An
			// absent header is not a claim, so the body is attempted instead: clients that omit
			// it are common and are not lying about their payload.
			if ct := r.Header.Get("content-type"); ct != "" {
				mt, _, err := mime.ParseMediaType(ct)
				if err != nil || (mt != "application/json" && !strings.HasSuffix(mt, "+json")) {
					if !enforce {
						next.ServeHTTP(w, r)
						return
					}
					writeFailure(w, http.StatusUnsupportedMediaType, "DQ_UNSUPPORTED_MEDIA_TYPE",
						"Unsupported media type",
						"this endpoint validates application/json records", r.URL.Path, cfg.Correlation(r))
					return
				}
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
				if !enforce {
					next.ServeHTTP(w, r)
					return
				}
				writeFailure(w, http.StatusRequestEntityTooLarge, "DQ_PAYLOAD_TOO_LARGE",
					"Record too large",
					fmt.Sprintf("the record exceeds the gate's %d byte limit", cfg.MaxBody),
					r.URL.Path, cfg.Correlation(r))
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))

			var payload map[string]any
			if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
				if !enforce {
					next.ServeHTTP(w, r)
					return
				}
				writeFailure(w, http.StatusUnsupportedMediaType, "DQ_UNSUPPORTED_MEDIA_TYPE",
					"Unsupported media type",
					"the body is not a JSON object, so it cannot be validated",
					r.URL.Path, cfg.Correlation(r))
				return
			}

			correlationID := cfg.Correlation(r)
			decision, err := cfg.Engine.Decide(plan, engine.Request{
				Payload:       payload,
				CorrelationID: correlationID,
			})
			if err != nil {
				// A pack that does not compile is a toolchain fault, not a record fault. Under
				// ENFORCE the record cannot be proven good, so it is refused — with a code that
				// says "we could not tell", not one that blames the caller's data. The error
				// text is not published: it can carry operand values.
				if !enforce {
					next.ServeHTTP(w, r)
					return
				}
				writeFailure(w, http.StatusInternalServerError, "DQ_RULE_ERROR",
					"Rule error",
					"the rule pack could not be evaluated for this request",
					r.URL.Path, correlationID)
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

// writeFailure renders one of the platform failures from §2.6. They are deliberately not
// violations: a violation says the record is wrong, a failure says the gate could not tell.
// Mixing them teaches clients to retry bad data or to quarantine good data.
//
// The code is carried as an extension member so the metric label
// (dq.failures{code=…}) and the wire response name the same thing.
func writeFailure(w http.ResponseWriter, status int, code, title, detail, instance, correlationID string) {
	w.Header().Set("content-type", "application/problem+json")
	if correlationID != "" {
		w.Header().Set("x-dq-correlation-id", correlationID)
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":          "https://registry.internal/problems/" + strings.ToLower(strings.ReplaceAll(code, "_", "-")),
		"title":         title,
		"status":        status,
		"detail":        detail,
		"instance":      instance,
		"correlationId": correlationID,
		"code":          code,
	})
}
