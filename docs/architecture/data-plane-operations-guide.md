# Data Plane Operations Guide

### Running the engine, the gate job and the stream gate

| | |
|---|---|
| **Version** | 0.1 |
| **Status** | In use from Wave M2 |
| **Audience** | SRE, platform on-call, data platform, service support |
| **Normative for** | Metric catalogue, alert catalogue, runbooks, capacity model, retention schedule, support model, cutover and decommission |
| **Cites** | [Solution Architecture](solution-architecture.md) — `C1–C12`, failure modes (§5.2), SLIs (§5.1) · [Operating Model](control-plane-operating-model.md) — lifecycle, break-glass · [Delivery Design Pack](delivery-design-pack.md) — contracts |
| **Owner** | Platform lead |
| **Change control** | Pull request; reviewed weekly at Platform Ops |

> **Why this document exists.** The architecture defines the data plane and lists ten failure modes with a detection signal each. It does not say what to do when a signal fires. This guide is the procedure behind each signal — the thing an on-call engineer opens at 03:00 when the coverage SLI is red and they have never seen this system before.

## Contents

1. [What runs where, and who is on call](#1-what-runs-where-and-who-is-on-call)
2. [Metric catalogue](#2-metric-catalogue)
3. [Alert catalogue](#3-alert-catalogue)
4. [Runbooks](#4-runbooks)
5. [Capacity and sizing model](#5-capacity-and-sizing-model)
6. [Retention and records schedule](#6-retention-and-records-schedule)
7. [Support model](#7-support-model)
8. [Cutover and decommission per object](#8-cutover-and-decommission-per-object)
9. [Dashboards](#9-dashboards)
- [Appendix A — On-call quick card](#appendix-a--on-call-quick-card)
- [Appendix B — What remains open](#appendix-b--what-remains-open)

---

## 1. What runs where, and who is on call

| Component | Runtime | Scaling | Restart impact | On-call owner |
|---|---|---|---|---|
| `C6` engine in WASM (`C7a`) | Inside Envoy, no pod | With Envoy workers | Traffic continues; bundle reloads | Platform |
| `C6` in library (`C7c`) | Application pod | With the app | Application-specific | Application team |
| `C6` via `ext_proc` (`C7b`) | Pod, per namespace | HPA on CPU | Requests fail per `onBundleFailure` | Platform |
| `C8` stream gate | Pod, partition-bound | Partitions; `maxReplicas` = partitions | Records replay from committed offset | Data platform |
| `C9` gate job | Kubernetes Job | One per file | Job retried; no partial acceptance | Data platform |
| `C11` telemetry | Collector DaemonSet | Node-bound | Metrics buffered, then dropped | SRE |

**Two structural facts that shape every runbook.** The HTTP path has **no dependency to recover** — if the registry is down, healthy pods keep serving their loaded bundle. And the **batch path is not real-time**: a binding change does not affect a job already running, so "restart the job" is a legitimate first action for file-channel incidents.

---

## 2. Metric catalogue

Telemetry uses OpenTelemetry instrument names with dots; Prometheus exposition converts them to underscores. **Label sets are a contract** — adding a label is a breaking change for dashboards and for cardinality budgeting.

| Metric | Type | Unit | Labels | Cardinality ceiling | Emitted by |
|---|---|---|---|---|---|
| `dq.rules.evaluated` | counter | evaluations | `channel`, `intent`, `object`, `result` (pass/fail/skipped) | 3 × 6 × 20 × 3 = 1,080 | C6 |
| `dq.violations` | counter | violations | `channel`, `rule_id`, `severity`, `dimension` | ~400 × 1 | C6 |
| `dq.failures` | counter | failures | `code` (§2.6 failure codes), `channel` | 7 × 3 = 21 | C6, adapters |
| `dq.duration` | histogram | seconds | `channel`, `outcome` | 3 × 3 = 9 | C6, adapters |
| `dq.rule.skipped` | counter | rules skipped | `rule_id`, `reason` (cost_limit/provider/unavailable) | 400 × 3 = 1,200 | C6 |
| `dq.rule.suppressed` | gauge | rules | `rule_id` | 400 | C6 |
| `dq.rule.suppression_expired` | counter | events | `rule_id` | 400 | C6 |
| `dq.engine.panic` | counter | panics | `adapter` | 6 | C7a, C7b |
| `dq.bundle.source` | gauge | 1 | `channel`, `source` (primary/fallback) | 6 | C6 |
| `dq.bundle.age_seconds` | gauge | seconds | `workload` | 500 | C10 operator |
| `dq.bundle.loaded_digest` | gauge *(info)* | 1 | `workload`, `digest` | **unbounded** — see below | C6 |
| `dq.coverage.ratio` | gauge | ratio 0–1 | `channel`, `object` | 3 × 20 = 60 | C11, derived |
| `dq.gate.lag_records` | gauge | records | `topic` | 50 | C8 |
| `dq.gate.rejected_records` | counter | records | `topic`, `reason` | 50 × 5 | C8 |
| `dq.file.records` | counter | records | `outcome` (accepted/rejected) | 2 | C9 |
| `dq.file.duration` | histogram | seconds | — | — | C9 |

**The digest cardinality trap, resolved.** `dq.bundle.loaded_digest` carries a value that changes on every release. As a *label* it is unbounded and will eventually take down the metrics backend. It is therefore emitted as an **info-style gauge** (value 1, digest carried as a label but excluded from retention by relabelling at the collector), and **no alert is built on it**. Ageing is alerted on `dq.bundle.age_seconds`, and divergence on the CRD condition `DigestDrift` — both of which are bounded.

**Coverage is computed, not counted.** `dq.coverage.ratio` is derived, not emitted directly:

```
coverage = sum(rate(dq_rules_evaluated{result!="skipped"}))
         / sum(rate(dq_rules_evaluated))
```

Reported per channel and per object, never as a single estate-wide number — an estate average hides one unenrolled service completely.

---
## 3. Alert catalogue

| Alert | Expression (PromQL sketch) | For | Sev | Automatic action | Runbook |
|---|---|---|---|---|---|
| `DQ_CoverageBelowTarget` | `dq_coverage_ratio < 0.999` | 10m | **P1** page | none | [F-05](#f-05--coverage-below-target) |
| `DQ_BundleOnFallback` | `dq_bundle_source{source="fallback"} == 1` | 5m | P2 ticket | none | [F-01](#f-01--registry-unreachable-or-bundle-on-fallback) |
| `DQ_BundleStale` | `dq_bundle_age_seconds > 604800` | 1h | P3 | none | [F-02](#f-02--bundle-stale) |
| `DQ_DigestDrift` | CRD condition `DigestDrift=True` | 15m | P2 | none | [F-03](#f-03--digest-drift) |
| `DQ_ViolationSpike` | `rate(dq_violations[10m]) > 5 × avg_over_time(dq_violations[7d:1h])` | 10m | **P1** page | **trip the quality circuit breaker to `SHADOW`** | [F-06](#f-06--violation-spike) |
| `DQ_RuleSkippedHigh` | `rate(dq_rule_skipped[15m]) / rate(dq_rules_evaluated[15m]) > 0.001` | 15m | P2 | none | [F-04](#f-04--rules-skipping) |
| `DQ_EnginePanic` | `increase(dq_engine_panic[5m]) > 0` | 0m | **P1** page | revert to previous digest if latency also breached | [F-07](#f-07--engine-panic) |
| `DQ_EngineUnavailable` | `rate(dq_failures{code="DQ_ENGINE_UNAVAILABLE"}[5m]) > 0` | 5m | **P1** page | none | [F-13](#f-13--engine-unavailable) |
| `DQ_LatencyBudgetBreach` | `histogram_quantile(0.99, sum by (le) (rate(dq_duration_bucket{channel="http"}[10m]))) > 0.001` | 10m | P2 | none | [F-08](#f-08--latency-budget-breach) |
| `DQ_GateLag` | `dq_gate_lag_records > 10000` | 10m | P2 | scale within `maxReplicas` | [F-09](#f-09--stream-gate-lag) |
| `DQ_RejectionRateHigh` | `rate(dq_gate_rejected_records[10m]) / rate(dq_file_records[10m]) > 0.05` | 10m | **P1** page | pause forwarding when policy is `fail-file` | [F-10](#f-10--rejection-rate-high) |
| `DQ_GateJobStalled` | no `dq_file_duration` sample in 2 × schedule interval | 1 cycle | P2 | none | [F-11](#f-11--file-gate-job-stalled) |
| `DQ_SuppressionExpiring` | CRD condition `SuppressionExpiring=True` | daily | P3 | none | [F-12](#f-12--suppression-expiring-or-expired) |
| `DQ_ValueSetStale` | value-set age > 120 days | daily | P3 | none | Governance — DQ Council |

**House rule: an alert without a runbook is deleted.** Alert sprawl is the main reason rotas stop reading them, and every row above resolves to a numbered procedure.

**Burn-rate alerting replaces static thresholds** once four weeks of baseline exist: 2% of the coverage budget burned in one hour (14.4× rate) pages immediately; 5% in six hours (6× rate) opens a ticket. Static thresholds apply only in the first month, when there is no baseline to burn against.

---
## 4. Runbooks

Grouped by where the fault lives. Each is written for an engineer who has not seen this system before.

### Bundle and control-plane dependency

#### F-01 · Registry unreachable or bundle on fallback

| | |
|---|---|
| **Symptom** | `DQ_BundleOnFallback` firing; `dq.bundle.source=fallback`; CRD `BundleVerified=False` |
| **First 5 minutes** | `kubectl get qb -n <ns> -o yaml` and read `status.observedDigest`. Confirm the registry is reachable from a node. Check `imagePullSecrets` and OIDC token freshness. |
| **Then** | If the primary is unreachable, **leave it** — the fallback is a *declared* digest and is serving correctly. Do not bump digests during an outage. If the fallback digest does not match `compiledBundleDigest`, the workload is running unvalidated: escalate immediately. |
| **Escalate if** | A fail-closed binding is returning 5xx, or the digest mismatch above occurs. Platform lead within 15 minutes. |

#### F-02 · Bundle stale

| | |
|---|---|
| **Symptom** | `dq.bundle_age_seconds > 604800`; no recent adoption for the workload |
| **First 5 minutes** | `dqctl status --binding <name>` — is there a newer published digest nobody adopted? |
| **Then** | Usually an **adoption backlog**, not an incident: raise it in the weekly hygiene review. If the workload should be current and is not, treat as F-03. |
| **Escalate if** | A `P1`-criticality object is stale beyond 30 days. |

#### F-03 · Digest drift

| | |
|---|---|
| **Symptom** | CRD `DigestDrift=True`; `dq.pack.digest` differs across replicas |
| **First 5 minutes** | `kubectl rollout status` on the workload. Confirm every pod restarted. Read `status.observedDigest`. |
| **Then** | Some pods old → complete the rollout. All pods old → the binding change never reached the controller: check Argo sync status. |
| **Escalate if** | Drift persists beyond two propagation windows (10 minutes HTTP, 20 minutes Kafka). |

### Rule evaluation

#### F-04 · Rules skipping

| | |
|---|---|
| **Symptom** | `DQ_RuleSkippedHigh`; `dq.rule.skipped{reason}` rising |
| **First 5 minutes** | Identify the reason. `cost_limit` → one rule is too expensive; `provider` → a context provider is failing; `unavailable` → tier 2 cannot reach its dependency. |
| **Then** | Cost limit: raise that rule's `costLimit` or rewrite it. Provider: check breaker state and provider latency. **Skipping is not a pass** — coverage is falling while error rates look healthy. |
| **Escalate if** | More than 1% of evaluations skip for over 30 minutes in PROD. |

#### F-05 · Coverage below target

| | |
|---|---|
| **Symptom** | `DQ_CoverageBelowTarget`; violation counts look implausibly healthy |
| **First 5 minutes** | Split by `channel` and `object`. Is one channel at zero? Check `DQ_INTENT_UNKNOWN` (path missing from the binding), `DQ_UNSUPPORTED_MEDIA_TYPE`, `DQ_PAYLOAD_TOO_LARGE`. |
| **Then** | Coverage loss is nearly always one of three: a new route absent from the binding, payloads above `maxRequestBytes`, or rules skipping. Compare `dq.rules.evaluated` with gateway request rate. |
| **Escalate if** | Immediately. This is the alert that means the guardrail is not guarding. |

#### F-06 · Violation spike

| | |
|---|---|
| **Symptom** | `DQ_ViolationSpike`; rejection rate jumps; customer reports |
| **First 5 minutes** | The circuit breaker should already have reverted the binding to `SHADOW` — confirm it did. Determine whether violations concentrate on one `rule_id` (a bad rule) or spread across all rules (an upstream data change). |
| **Then** | One rule → suppress and fix it. All rules → the producer is sending different data; engage the producing team before re-enabling enforcement. |
| **Escalate if** | Immediate page. This is risk R2 from the risk register, and it decides whether the programme keeps its credibility. |

#### F-07 · Engine panic

| | |
|---|---|
| **Symptom** | `DQ_EnginePanic > 0`; Envoy statistics show filter failures |
| **First 5 minutes** | Determine whether traffic is failing closed or failing *open*. Correlate with the most recently adopted digest. |
| **Then** | Revert the binding to the previous digest. A panic is a defect in the engine or in a compiled rule — **never in the data**. Preserve the trace: `adapter`, `rule_id`, digest. |
| **Escalate if** | Any PROD occurrence is P1 and becomes a defect ticket against the engine team. |

#### F-08 · Latency budget breach

| | |
|---|---|
| **Symptom** | `DQ_LatencyBudgetBreach`; p99 overhead above 1 ms |
| **First 5 minutes** | Is payload size up, or rule count up? Read `dq_duration` by `channel` and the resolved plan size for the affected object. |
| **Then** | Do **not** raise cost limits to hide it — that converts a latency problem into a coverage problem. Reduce rule cost or move the expensive rule to tier 2. Then revisit the rule-count budget. |
| **Escalate if** | Sustained beyond 30 minutes, or accompanied by an error-rate increase. |

---
### Streaming and batch

#### F-09 · Stream gate lag

| | |
|---|---|
| **Symptom** | `DQ_GateLag > 10000`; consumer-group lag rising |
| **First 5 minutes** | Check gate pod health and restart count — each restart replays from the committed offset. Confirm partition count ≥ desired replicas. |
| **Then** | Scale to `maxReplicas`, which is bounded by partitions. If CPU-bound on evaluation, tier-2 rules are the usual cause; check `dq_rule_skipped{reason="cost_limit"}`. |
| **Escalate if** | Lag approaches the topic retention window — records will be lost, which is a data-integrity incident, not a performance one. |

#### F-10 · Rejection rate high

| | |
|---|---|
| **Symptom** | `DQ_RejectionRateHigh`; DLQ volume spiking |
| **First 5 minutes** | Decide whether rejecting is *correct*: sample five DLQ records and read the violation reasons. Then confirm circuit-breaker state. |
| **Then** | Correct → the producer has a defect; notify the producing team and keep the DLQ. Incorrect → suppress the offending rule, publish, adopt. If policy is `fail-file`, forwarding is paused: verify downstream is idle and not failed. |
| **Escalate if** | Any pause in PROD, or more than 1% of a day's volume in the DLQ. |

#### F-11 · File gate job stalled

| | |
|---|---|
| **Symptom** | No `dq_file_duration` sample within two schedule intervals; downstream never started |
| **First 5 minutes** | `kubectl get jobs -n batch` — running, failed, or never created? Confirm the manifest actually arrived. |
| **Then** | A running job uses the binding as at job start; re-running picks up the current digest. **Never write `_SUCCESS` by hand** — that is precisely how an unvalidated file reaches downstream. |
| **Escalate if** | The batch window is at risk, or a job has failed twice on the same file. |

### Governance

#### F-12 · Suppression expiring or expired

| | |
|---|---|
| **Symptom** | CRD `SuppressionExpiring=True`; `dq.rule.suppression_expired` counter |
| **First 5 minutes** | `dqctl status --rule <id>` — has the rule been fixed and published? |
| **Then** | Expired → enforcement has resumed automatically; confirm the false positive is genuinely gone before the next peak. Expiring → fix it, or renew with justification at DQ Council. |
| **Escalate if** | Never a page. This is deliberately a governance item so that it cannot mask a technical incident. |

#### F-13 · Engine unavailable

| | |
|---|---|
| **Symptom** | `DQ_EngineUnavailable` — `503` with `DQ_ENGINE_UNAVAILABLE` in `ENFORCE`; request latency at the timeout in every mode |
| **First 5 minutes** | Is the ext_proc service up, and is its bundle loaded? `dq_bundle_source` and the pod's start-up log answer both. Distinguish *unreachable* (the call never landed) from *slow* (the call landed and exceeded `message_timeout`) — the first is a platform fault, the second is usually rule cost or GC. |
| **Then** | If unreachable: restart the service; it loads the pinned digest and serves within seconds. If slow: reduce rule cost rather than raising the timeout, for the same reason F-08 refuses to raise cost limits. **Do not switch the route to `SHADOW` to clear the alert** — that converts an availability incident into a coverage incident, and coverage is the thing the guardrail exists to hold. |
| **Escalate if** | More than one workload is affected, or the service restarts repeatedly — that is a crash loop, and F-07 is the better page. |

---

## 5. Capacity and sizing model

The architecture gives resource requests and limits; this section gives their **derivation**, so capacity can be re-planned when volumes change rather than guessed.

| Quantity | Formula | Notes |
|---|---|---|
| Peak request rate | `daily_records × peak_factor / 86_400` | `peak_factor` between 3 and 8; measured in M0, not assumed |
| Records per second, Kafka | `daily_records × peak_factor / 86_400`, then ÷ partitions | Per-partition throughput is the gate's real constraint |
| Engine CPU, HTTP | `peak_rps × plan_size × 50µs` | `plan_size` = number of rules in the resolved plan |
| Engine memory | `2 × bundle_size + 64 MB` | Two copies during reload, plus runtime overhead |
| WASM footprint | `workers × (5–20 MB)` | Per Envoy worker, not per pod |
| Kafka gate replicas | `max(3, ceil(peak_records_per_sec ÷ 2_500))` | Bound by partition count; 2,500 rec/s per instance is a planning figure pending M4 |
| File job memory | constant, independent of file size | Streaming reader; if memory grows with file size, that is a defect |
| File job duration | `records ÷ 10_000` seconds | Planning figure from M5, to be replaced by measurement |

**Headroom rule.** Provision for **2× peak** and run **N+1 across three availability zones**. The single largest capacity surprise in systems of this shape is not average load — it is a replay: a DLQ or a re-run of a large file reproduces a day's volume in an hour.

**Bundle size ceiling.** A bundle carries every compiled plan, so it grows with objects × intents × channels. Budget 2 MB per object; at roughly 50 objects a bundle reaches 100 MB, which is the point at which the WASM-cached copy and pod start-up time both need revisiting. This is the concrete trigger behind risk R7.

---
## 6. Retention and records schedule

The governing principle: **records outlive the rules that judged them.** A violation from 2029 must still be interpretable in 2036, which is only possible if the bundle it names stays resolvable. Bundles are therefore never purged.

| Artefact | Where | Retention | Legal hold | Purge mechanism |
|---|---|---|---|---|
| HTTP violation metrics | Prometheus / Tempo | 13 months, downsampled after 90 days | n/a | native retention |
| Violation payloads (DLQ) | `.dlq` topic | 30 days hot, then object store | **Yes** | topic retention + bucket lifecycle |
| Reject files (`rejects.csv`) | Object store | Object's `retention` field — typically `P7Y` | **Yes** | bucket lifecycle |
| Gate result manifests | Object store | 7 years | **Yes** | bucket lifecycle |
| Accepted files | Object store | Pipeline's own policy — not DQ's | Yes | not DQ-managed |
| **Bundles, all digests** | OCI registry | **Indefinite** | n/a | **Never purged**; superseded digests retained |
| Bindings | Git | Indefinite | n/a | n/a |
| Rule packs, including retired | Git | Indefinite | n/a | n/a |
| Quality scorecards | Catalogue | 3 years | n/a | catalogue API |
| Engine logs | Loki | 30 days | n/a | native retention |
| Break-glass audit | Git + incident system | 7 years | Yes | incident system policy |

**Two rules that make this operable.** A legal hold on an object immediately suspends purge for every derived artefact of that object — rejects, DLQ, manifests — because a held record's evidence lives in all four places. And an export request must be answerable **from the bundle alone** plus the violation records: if it needs the live platform, the retention model has failed.

---

## 7. Support model

| Tier | Who | Scope | May do | May not do | Response |
|---|---|---|---|---|---|
| **L1** | Service desk | Triage from the alert catalogue; evidence gathering | Acknowledge, open an incident, run read-only `dqctl status`/`impact` | Change a binding, edit a rule | 15 min acknowledge |
| **L2** | Platform on-call | Every runbook F-01–F-12 | Revert a binding, suppress a rule, scale, execute break-glass with platform lead agreement | Change object contracts or rule semantics | P1 immediately; P2 next business hour |
| **L3** | Engine team | Defects in engine, compiler, CEL handling | Fix code, publish a superseding digest | Bypass CI | Per defect priority |

**Escalation matrix.**

| Severity | First responder | Notify | Latest resolution target |
|---|---|---|---|
| **P1** — enforcement wrong, traffic failing, or coverage lost | L2 immediately | Platform lead in 15 min; governance lead if customer-visible | Mitigate within 1 hour |
| **P2** — degraded coverage, lag, drift | L2 on call | Platform lead daily summary | Within one business day |
| **P3** — hygiene, staleness, expiring suppression | L1 → L2 by ticket | DQ Council monthly | Next sprint |

**Hypercare.** Four weeks following the first `ENFORCE` adoption on a channel, with a daily 15-minute review of coverage, false positives and suppressions. Exit criteria: coverage ≥ 99.9% for 14 consecutive days, false-positive rate ≤ 0.01%, and zero unanswered P1s.

**Handover checklist** — the eight items without which a service organisation cannot take this on:

| # | Item | Evidence |
|---|---|---|
| 1 | Alert catalogue live and routed | Alertmanager configuration |
| 2 | Every alert names a runbook | §3 links resolve |
| 3 | Dashboards published | §9 |
| 4 | On-call rota named, including the break-glass authority | Rota + IdP group |
| 5 | Retention configured and verified by a test export | Export test record |
| 6 | Game day executed: kill switch, degrade ladder, fallback digest | Game-day notes |
| 7 | Runbooks rehearsed by someone who did not write them | Signed rehearsal record |
| 8 | Support tiers and escalation agreed in writing | This section, signed |

Item 7 is the one that finds the gaps. A runbook that has never been followed by a second person is a draft.

---
## 8. Cutover and decommission per object

This is where benefit D2 — the payback case — is actually realised. It is therefore a tracked milestone per object, not an afterthought once enforcement works.

| Stage | Guardrail | Legacy validators | Evidence to exit the stage | Rollback |
|---|---|---|---|---|
| **S0 · Baseline** | not deployed | Authoritative | Write-path inventory complete; `dqctl impact` run for the object | n/a |
| **S1 · Shadow** | `SHADOW` — observes, mutates nothing | Authoritative | 2 weeks; false positives ≤ 0.01%; coverage ≥ 99.9% | Remove the binding |
| **S2 · Dual-run** | `WARN` — logs, does not block | Authoritative, with **daily difference review** | 2 weeks; zero unexplained differences between guardrail and legacy verdicts | Set mode `SHADOW` |
| **S3 · Cutover** | `ENFORCE` | Monitoring only — logs but no longer blocks | 4 weeks; no P1 attributable to the guardrail; scorecard stable; **legacy re-arm rehearsed** | Set mode `WARN` and re-arm legacy (one config change) |
| **S4 · Decommission** | `ENFORCE` | **Removed** | Retirement ticket; code deleted; D2 benefit recorded | Restore from Git — only safe because the re-arm was rehearsed at S3 |

**Three rules that this ladder depends on.**

*S2 is never skipped, and it is the stage people skip.* S1 proves the guardrail does not reject good data. S2 proves it agrees with the system it will replace. Those are different claims, and only the second catches semantic divergence — the case where both systems accept a record but for different reasons, which surfaces six months later as a reconciliation break.

*Legacy stays re-armable until S4.* Rollback from S3 is a change to the **legacy** system, not to the guardrail. That asymmetry is the thing to plan for, and it is why the re-arm must be rehearsed before S4 rather than discovered after it.

*Decommission is per object, not per service.* A service may use the guardrail for `customer` and still run a bespoke `portfolio` validator. Benefit is realised object by object, and the D2 counter is objects decommissioned, not services migrated.

**Per-object plan template** — one row per object, reviewed at each gate:

| Field | Example |
|---|---|
| Object | `customer@1.4.0` |
| Legacy validators in scope | `crm-validation`, `batch-loader-checks` |
| Channels in scope | HTTP, Kafka, file |
| Target stage by wave | S3 by M5, S4 by M6 |
| Evidence owner | Domain steward |
| Re-arm path rehearsed | Date + operator |
| Benefit recorded | Days/yr of maintenance retired |

---

## 9. Dashboards

Four dashboards. Each answers one question, and no dashboard exists without a question.

| # | Dashboard | Question it answers | Primary signals |
|---|---|---|---|
| **D1** | Control-plane health | *Is the factory working?* | Published vs adopted digests, adoption lag, open suppressions, stale value sets, align-gate failures |
| **D2** | Data-plane health | *Is the guardrail guarding?* | Coverage ratio by channel and object, p99 added latency, skip rate by reason, panic count, fallback share |
| **D3** | Quality scorecards | *Is the data improving?* | Six dimensions × object × channel against ratified thresholds, with the trend |
| **D4** | Pipeline health (Kafka and file) | *Is streaming and batch healthy?* | Gate lag, DLQ volume, rejection rate, gate-job duration and outcome |

**D2 is the one to build first**, because it is the only dashboard that can show the guardrail silently doing nothing — and a system that appears healthy while validating nothing is worse than no system, since it consumes the credibility that would otherwise fund the fix.

---

## Appendix A — On-call quick card

```
  1. Is it guarding?      dq_coverage_ratio by channel          (D2)
  2. Which bundle?        kubectl get qb -o wide                (status.observedDigest)
  3. Why is it skipping?  dq_rule_skipped by reason             (F-04)
  4. Stop the bleeding    one-line binding PR: mode → SHADOW
                          or add a suppress block with expiry    (OM §6, §9)
  5. Never do this        write _SUCCESS by hand, or edit a digest
                          to "fix" a rollout                    (F-11, F-03)
  6. Escalate             P1 → platform lead in 15 min
  7. After the incident   revert, or convert to suppression with
                          an expiry within one business day      (OM §9)
```

---

## Appendix B — What remains open

| Gap | Status | Wave |
|---|---|---|
| Metric and alert catalogue | **Closed** | §2, §3 |
| Capacity and sizing model | **Closed** | §5 |
| Retention and records schedule | **Closed** | §6 |
| Support model and hypercare | **Closed** | §7 |
| Cutover and decommission | **Closed** | §8 |
| Test strategy, including performance and chaos test plans | **Closed** — Delivery Design Pack §8 | M2 |
| Interface catalogue inventory (as opposed to the wire contracts) | **Open** — generated by `dqctl impact` once that verb exists, never hand-maintained | M2 |
| Requirements traceability matrix | **Closed** — Delivery Design Pack Appendix C | M1 |

Seven of eight closed. The one remaining item is deliberately a generated artefact rather than a document, and each kind of evidence now has exactly one home: procedures here, build contracts in the pack, decisions in the architecture.





