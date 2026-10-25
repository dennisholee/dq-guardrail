# Control Plane Operating Model

### Managing and disseminating business objects and validation rules

| | |
|---|---|
| **Version** | 0.1 (living) |
| **Status** | In use from Wave M1 |
| **Audience** | Platform, data governance, domain stewards, SRE |
| **Normative for** | Lifecycle transitions, per-channel propagation, change ceremony, per-rule suppression, inventory and impact queries, reference-data cadence, break-glass |
| **Cites** | [Solution Architecture](solution-architecture.md) — decisions, invariants (S1–S7), component IDs (C1–C12), waves (M0–M7) |
| **Change control** | Platform lead; reviewed monthly at Platform Ops |
| **Parent decision** | D-011 in [Appendix E](solution-architecture.md#appendix-e--decision-log) |

> **This document describes how the control plane is operated. It does not decide what the architecture is.** Where the two appear to conflict, the Solution Architecture governs and this document is corrected. It consumes identifiers and never redefines them.

## Contents

1. [The two events: publish and adopt](#1-the-two-events-publish-and-adopt)
2. [Roles and their working rhythm](#2-roles-and-their-working-rhythm)
3. [Lifecycle states and transitions](#3-lifecycle-states-and-transitions)
4. [Dissemination: how content reaches each channel](#4-dissemination-how-content-reaches-each-channel)
5. [Change classes and their ceremony](#5-change-classes-and-their-ceremony)
6. [Per-rule suppression](#6-per-rule-suppression)
7. [Answering the three questions](#7-answering-the-three-questions)
8. [Reference data and value sets](#8-reference-data-and-value-sets)
9. [Break-glass](#9-break-glass)
10. [Onboarding a workload or an object](#10-onboarding-a-workload-or-an-object)
- [Appendix A — Trace one rule end-to-end](#appendix-a--trace-one-rule-end-to-end)
- [Appendix B — Governance calendar](#appendix-b--governance-calendar)

---

## 1. The two events: publish and adopt

The architecture states the constraint (S7). This is what it means for the people doing the work.

| | **Publish** | **Adopt** |
|---|---|---|
| Performed by | CI, automatically | A platform engineer, deliberately |
| Trigger | Merge to main | A binding change |
| Output | `dq-bundle@sha256:Dn` exists | `bundle: sha256:Dn` in one binding |
| Duration | About 3 minutes | Under 5 minutes to propagate |
| Failure mode | Build fails; nothing published | Rollout fails; workload stays on its previous digest |
| Undo | Delete the tag reference; the digest remains resolvable | Revert the binding line |

**The adoption ledger is the bindings directory.** Because every workload's rule version is a Git line, "what is live where" is a repository query rather than a database lookup:

```bash
grep -rn 'sha256:' bindings/            # every workload → digest
git log -p -- bindings/customer-service.yaml   # when it changed, and by whom
```

This is a deliberate design property, not an accident: the answer to *which rules are enforcing right now* must be recoverable from artefacts under review, without a control-plane database that can disagree with Git.

**What each role does about it.**

| Role | Weekly | On a publish event |
|---|---|---|
| Domain steward | Reviews violations for their object | Nothing — publishing does not affect them |
| Domain architect | Reviews proposed rule changes | Notes which workloads *should* adopt |
| Platform engineer | Reviews the adoption backlog | Decides adoption order; updates bindings |
| SRE | Watches coverage SLI and digest skew | Confirms propagation within the 5-minute SLO |

**The most common misunderstanding.** "The rule is merged, so it is live" is false. Between publish and adoption the system is behaving exactly as designed. The Operating Model therefore requires that every rule change request state its **intended adoption wave** at review time, so the gap is planned rather than discovered.

---

## 2. Roles and their working rhythm

RACI is defined once in [Solution Architecture §7.7](solution-architecture.md#77-operating-model-and-decision-rights). This section defines the *rhythm* — how much time each role commits, and what "done" looks like for them.

| Role | Commitment | Artefacts they touch | Definition of done |
|---|---|---|---|
| **Object steward** | 2 hours per week | `catalog/business-objects/*`, thresholds in `*.meta.yaml` | Scorecard within threshold, or a remediation plan |
| **Domain architect** | 4 hours per week | `rules/object/**`, `rules/intent/**` | Rules merged, tested, and adopted by the intended wave |
| **Platform engineer** | 1 day per week | `bindings/**`, `engine/**`, CI | Adoption backlog cleared; coverage SLI green |
| **Reference-data owner** | Quarterly | `catalog/value-sets/**` | Value sets current against their issuing authority |
| **Governance lead** | 2 hours per week | Decision log, suppressions register | Open suppressions and dimension breaches reviewed |

The two hours for a steward is the single most important number here. Below it, rules degrade into a platform-owned list nobody defends; above it, the role becomes a full-time job that will not be filled.

---
## 3. Lifecycle states and transitions

State **names** are normative in [Solution Architecture §7.8](solution-architecture.md#78-lifecycle-publish-adopt-retire). This section defines who moves each artefact, on what evidence, and which transitions are automated.

| From | To | Trigger | Actor | Evidence required | Automated |
|---|---|---|---|---|---|
| `draft` | `proposed` | Pull request opened | Author | Branch pushed | Yes — CI starts |
| `proposed` | `proposed` | CI failure | Author | — | Yes — blocks merge |
| `proposed` | `draft` | Review rejected or withdrawn | Reviewer | Review comment | No |
| `proposed` | `published` | Merge | Required approvers | All gates green + approvals per §7.3 | Partly — CI publishes |
| `published` | `adopted` | Binding updated | Platform engineer | Adoption PR referencing the publish | No |
| `published` | `superseded` | Never adopted, newer digest published | CI | Later digest exists | Yes |
| `adopted` | `superseded` | Newer digest adopted for that workload | Platform engineer | Binding diff | No |
| `superseded` | `retired` | Retention period expires | Platform engineer | Retention policy | Yes — scheduled job |
| any | `retired` | Object decommissioned | ARB decision | Decision log entry | No |

**Applicability by artefact type.** Not every state applies to every artefact, and pretending otherwise is what produces process theatre:

| Artefact | States that apply | Note |
|---|---|---|
| Object contract | all six | `retired` only by ARB decision; the `$id` is never reused |
| Rule pack | all six | Retirement is additive: the rule stays in the file with a retirement date and a successor |
| Intent pack | all six | Same as rule pack |
| Binding | `draft` → `proposed` → `adopted` only | A binding is never "published" or "retired"; it is superseded by its next revision in Git |
| Value set | `draft` → `proposed` → `published` → `adopted` | Adopted only as part of a bundle |

**Two transitions that need explicit handling.**

*Withdrawn after publication.* A rule may be published and then found defective before any workload adopts it. The correct action is to publish a superseding bundle and leave the defective digest resolvable for evidence — never to delete it. Deleting a digest breaks S6 for any record that was evaluated by it, and a record can outlive the rule that judged it.

*Retirement of a rule that is still referenced.* A rule cannot be retired while any live binding's plan evaluates it at `ENFORCE`. `dqctl status --rule <id>` is the check; the compiler also refuses to build a retirement whose successor does not exist.

---

## 4. Dissemination: how content reaches each channel

One bundle, three consumption patterns, three different definitions of "propagated".

| Channel | Adoption unit | Mechanism | Cache layers | Reload trigger | Propagation SLO | Rollback |
|---|---|---|---|---|---|---|
| **HTTP — WASM (C7a)** | Binding for the workload or namespace | Argo CD → binding → filter configuration | Image-baked fallback → pod `emptyDir` → node | Filter reload on pod rollout | ≤ 5 min to 95% of pods | Revert binding line, Argo syncs |
| **HTTP — ext_proc (C7b)** | Per-namespace service binding | Argo CD → deployment env | Image-baked fallback → `emptyDir` | Pod restart | ≤ 5 min | Revert binding, redeploy |
| **HTTP — library (C7c)** | Application build | Dependency bump in the app | In-process | Application redeploy | Next app release | Revert app version |
| **Kafka (C8)** | Gate deployment binding | Argo CD → deployment env | `emptyDir` | Deployment rollout | ≤ 10 min | Revert binding, redeploy |
| **File (C9)** | Workflow's binding reference | Argo Workflows template variable | Job-local `emptyDir` | Next job start | At the next scheduled run | Revert binding; next run uses previous |

**The consistency unit is the record, not the fleet.** There is no instant at which every workload runs the same rule set, and there should not be — staged adoption is the mechanism by which a bad rule is contained. What must hold instead is that **every verdict states which digest produced it**, so any record can be explained after the fact. That is what S6 buys, and it is the reason the digest travels on the wire.

**Two consequences specific to streaming and batch.**

*Kafka* adoption has a boundary. Records processed before the gate rolled out were decided by the previous digest; records after, by the new one. A DLQ replay therefore re-evaluates under the *current* digest, which may produce a different verdict — expected, and the reason replay output records the digest it used.

*File* adoption is deferred to the next run. A file dropped at 01:59 and gated by a job starting at 02:00 uses whatever digest the workflow references at 02:00. During an incident, do not expect a binding change to affect a job already running.

**Verification after adoption.** Three checks, in order, and each has a defined failure response:

| Check | Command or signal | If it fails |
|---|---|---|
| Digest converged | `dq.pack.digest` per workload equals the binding | Wait one propagation window, then investigate rollout |
| Bundle source is primary | `dq.bundle.source` = `primary`, not `fallback` | Registry pull or signature verification is failing |
| Coverage unchanged | Coverage SLI within 0.1% of its pre-adoption value | New rules are skipping — check `dq.rule.skipped{reason}` |

---
## 5. Change classes and their ceremony

One process for all changes is either too slow for a typo or too loose for a tightening. Five classes, each with proportionate ceremony.

| Class | Examples | Approvals | Notice | Ceremony | Typical lead time |
|---|---|---|---|---|---|
| **C1 · Editorial** | Message wording, test fixtures, comments | 1 reviewer | None | PR + CI | Minutes |
| **C2 · Additive** | New `MINOR` rule, new optional field in a minor schema version | Domain architect | None | PR + CI + shadow period | 1 day + shadow |
| **C3 · Tightening** | New `required` rule, narrower pattern, `MAJOR` severity | Architect + steward | 5 working days | PR + CI + shadow + consumer notice | 5 days + shadow |
| **C4 · Relaxation** | `relax` block, retiring a rule | Steward + governance lead | 10 working days if consumer-visible | PR + justification + expiry | 10 days |
| **C5 · Breaking** | Schema major bump, response-shape change | ARB | 90 days | New `$id` + parallel pack + deprecation window | One quarter |

**Why notice periods differ.** C3 and C4 change what a *different team's* traffic does; C5 changes what a *different organisation's* client must implement. Notice is proportional to who outside the change must act, not to the size of the diff.

**Shadow is not optional for C2–C4.** Every tightening and relaxation is adopted in `SHADOW` for one full review cycle before promotion. A change that skips shadow has no false-positive evidence, and the quality circuit breaker will eventually trip on it in production instead.

**Reviewer guidance for C4.** The most common bad relaxation is one that exists to avoid fixing data. Three questions at review: *Is there a `relax` with an expiry? Is there a named owner for removing it? Would this relaxation still be acceptable if it were permanent?* If the third answer is "yes", it is not a relaxation — it is a definition change (C5) and belongs in the schema.

---

## 6. Per-rule suppression

Decision D-010. Until it is implemented, the only options are reverting a digest or setting a whole workload to `OFF`; both are too blunt for one misfiring rule.

**Syntax** in the binding:

```yaml
apiVersion: dq.example.com/v1
kind: QualityBinding
metadata: { name: customer-service, namespace: app-customer }
spec:
  bundle: ghcr.io/org/dq-bundle@sha256:abc123…
  fallbackDigest: sha256:def456…   # required whenever mode is ENFORCE
  mode: ENFORCE
  suppress:
    - rule: SEM-CUS-005            # MAJOR; CRITICAL rules cannot be suppressed
      until: 2026-11-07T00:00:00Z  # mandatory, at most 30 days ahead
      ticket: INC-4412
      approver: platform.lead      # a named individual, not a team
      reason: "false positive when verifiedOn is absent on migrated records"
```

**Rules of use.**

| Rule | Rationale |
|---|---|
| Expiry is mandatory and capped at 30 days | A suppression without an end is a rule deletion with extra steps |
| `CRITICAL` rules cannot be suppressed, only relaxed through C4 | Critical rules protect regulated fields; the higher ceremony is deliberate |
| Every suppression emits `dq.rule.suppressed{rule}` and **reduces the coverage SLI** | Silent suppression would make the coverage metric a lie |
| The register is reviewed at every DQ Council | Open suppressions are the clearest signal of an unfinished change |

**Suppression versus relaxation — the distinction that matters.**

| | Suppress | Relax |
|---|---|---|
| Applies to | One rule, one workload | A rule, across a layer, for all workloads |
| Duration | Days (maximum 30) | Until expiry, typically a quarter |
| Cause | The rule is defective, or the data is temporarily invalid | The invariant genuinely does not apply to this context |
| Evidence | A ticket and an incident | A justification and two approvers |
| Ends with | A rule fix or a data fix | Removal at expiry, or promotion to a schema change |

Choose suppression when the *rule* is wrong. Choose relaxation when the *context* is genuinely different. Choosing suppression to avoid fixing data is the failure mode this table exists to prevent.

---
## 7. Answering the three questions

For any rule, four questions should be answerable in under a minute without reading a document. Each maps to a read-only query over Git and the registry — there is no control-plane database to fall out of step.

| Question | Command | Reads from |
|---|---|---|
| **Who owns this rule?** | `dqctl status --rule SEM-CUS-001` | Pack metadata plus CODEOWNERS |
| **Where is it live?** | `dqctl status --object customer` | Bindings × digest → resolved plans |
| **Since when, and why?** | `dqctl status --rule SEM-CUS-001 --history` | Git log plus violation digests |
| **Who is affected by a change?** | `dqctl impact customer@1.4.0` | OpenAPI operations, topics, file manifests, bindings |

**`dqctl impact` is the one to build first.** Before any C4 or C5 change it answers "which API operations, which topics, which batch manifests and which workloads consume this object version". Without it, a major bump is planned by asking people around a table, which produces both false confidence and unnecessary delay.

**Weekly hygiene.** Five checks the platform engineer runs each week. All five are cheap, and all five catch real problems.

| Check | Threshold | Action |
|---|---|---|
| Published digests never adopted | Older than 14 days | Adopt, or abandon the publish explicitly |
| Suppressions nearing expiry | Expiring within 7 days | Fix the rule or renew with justification |
| Workloads on `fallbackDigest` | Any | Registry or signature failure — investigate now |
| Digest skew within one workload | More than one digest | Rollout is stuck; check the filter reload |
| `CRITICAL` rules without a test fixture | Any | The L5 conformance gate has a hole |

---

## 8. Reference data and value sets

Two kinds of reference data with deliberately different dissemination behaviour, because conflating them produces either stale value sets or a network dependency in the request path.

| Entity | Where it lives | Owner | Refresh cadence | How it reaches the runtime |
|---|---|---|---|---|
| **ISO 3166-1, currencies, jurisdiction codes** | `catalog/value-sets`, pinned in the bundle | Reference-data owner | Quarterly, or on an ISO bulletin | Bundle publish, then adoption |
| **Jurisdiction tax-ID validation rules** | Rule pack plus reference snapshot | Domain architect | Semi-annual | Bundle publish, then adoption |
| **Internal reference data** (product, branch, account class) | Live provider (`RefDataLookup`) | Data platform | Continuous, TTL 15 minutes | No adoption — cached lookup |

**The trade-off, stated plainly.** A bundle-pinned value set cannot change without a release, which is correct for data that moves quarterly and unacceptable for data that moves hourly. A live provider has no release cost but introduces a runtime dependency whose failure degrades to `SKIPPED`. Default to the bundle; move to a live provider only when the refresh cadence genuinely demands it.

**Staleness control.** A value set older than 120 days raises a warning at DQ Council. This catches the common failure in which a country is added to ISO 3166, nothing breaks, and the omission surfaces a year later as an unexplained rejection.

---

## 9. Break-glass

Enforcement will eventually block something critical, at an inconvenient hour. A documented emergency path is safer than an undocumented one.

| Action | Authority | Maximum duration | Review |
|---|---|---|---|
| Set a binding to `WARN`, `SHADOW` or `OFF` | Platform lead or delegate | Until the next business day | Next Platform Ops |
| Add a `suppress` block | Platform lead or delegate | 30 days | DQ Council |
| Revert a binding to a previous digest | Any platform engineer on call | Until the incident closes | Platform Ops |

**Constraints.** Break-glass still passes CI — no build is ever bypassed. It never relaxes a `CRITICAL` rule (that remains a C4 change). It must reference an incident ID. And every break-glass merge is tagged, so the next Platform Ops reviews it as a standing agenda item.

**The 24-hour rule.** Within one business day, a break-glass change becomes one of three things: reverted, converted into a proper suppression with an expiry, or escalated into a C4 relaxation. A break-glass change that survives a week without one of those outcomes is an undocumented permanent exception, which is the condition the whole governance model exists to prevent.

---

## 10. Onboarding a workload or an object

**A new workload** joins in four steps, none of which requires an engine change:

| Step | Owner | Artefact |
|---|---|---|
| 1 · Declare the address | App team | Path follows `/{intent}-{businessObject}`; `x-dq-intent` and `x-dq-object` set |
| 2 · Reference the contract | App team | `$ref` to the canonical schema in the OpenAPI document |
| 3 · Create the binding | Platform | Digest, mode `SHADOW`, channel behaviour |
| 4 · Pass the coverage checks | Platform | `dqctl impact` clean; adoption verification passes |

**A new business object** takes longer because it changes the vocabulary:

| Step | Owner | Gate |
|---|---|---|
| 1 · Draft the contract and metadata | Steward | Includes the allowed-intent list |
| 2 · Approve the intent vocabulary | Domain architect | Any new intent is a governed addition, not free text |
| 3 · Write the object pack and at least one intent pack | Domain architect | Each rule carries a test fixture |
| 4 · Confirm consumers | Steward | `dqctl impact` identifies every writer before enforcement |
| 5 · Adopt in `SHADOW` on one channel | Platform | One review cycle before promotion |

Step 2 is the step most often skipped, and skipping it is how `{intent}-{businessObject}` decays into an ungoverned naming convention. If a team needs a new intent, they need a new intent in the registry — not a longer hyphenated word.

---
## Appendix A — Trace one rule end-to-end

The fastest way to internalise the model is to follow a single rule. A steward adds `SEM-CUS-005` on a Monday morning.

| Time | Actor | Event | Clock | Evidence produced |
|---|---|---|---|---|
| 09:10 | Steward | Edits `rules/object/customer.v1.pack.yaml`, adds the rule with a pass and a fail fixture, opens PR | Edit | Branch |
| 09:12 | CI | Spectral → `dqctl lint` → `dqctl align` → `dqctl test` → `bundle` → `publish` → `cosign sign` → `attest` → **D2** | Build (~3 min) | Bundle digest, gate log |
| 09:15 | Architect | Reviews the rule; the steward approves per CODEOWNERS | Edit | Two approvals |
| 09:20 | CI | Merge. **D2 now exists; it is live nowhere.** | — | Publish event |
| 09:35 | Platform engineer | Opens the adoption PR: `customer-service` binding → D2, mode `SHADOW` | Adopt | Binding diff |
| 09:36–09:41 | Argo CD, Envoy | Binding applied, filter reloaded, pods pull and verify D2 | Propagate | `dq.pack.digest` = D2 |
| +1 cycle | DQ Council | Reviews the shadow report; false-positive rate shown against the baseline | Governance | Promotion evidence |
| Then | Platform engineer | One-line PR: mode → `WARN`, later `ENFORCE` | Adopt | Binding diff |

**What this trace demonstrates.** Four roles act at four points, and only one of them changes production behaviour: the platform engineer's adoption. The steward never touches a cluster, the architect never approves a deployment, and neither of them waits for a release train.

**The same trace under failure.** If the shadow report shows a 2% false-positive rate, nothing is reverted: the workload is already at `SHADOW`, so the rule is revised in a new bundle and never reaches `WARN`. This is the entire reason shadow is a state rather than a setting.

---

## Appendix B — Governance calendar

Three rhythms, each with a fixed agenda. The value is not the meeting — it is that the agenda forces the evidence into existence.

| Cadence | Forum | Standing agenda | Output |
|---|---|---|---|
| **Weekly** | Platform Ops | Coverage SLI, digest skew, adoption backlog, open break-glass changes | Remediation actions |
| **Monthly** | DQ Council | Scorecard by object and dimension, open suppressions, threshold changes, value-set staleness | Governance findings raised against stewards |
| **Quarterly** | DQ Council + reference-data owner | Value-set refresh, retired-rule review, threshold re-baselining, adoption wave planning | Updated thresholds; new wave plan |
| **Annually** | Design Authority | Major-version deprecation plan, decision-log review, documentation review (this document included) | Deprecation calendar; ADR updates |
| **At each gate** | Design Authority | Gate evidence per [Solution Architecture §9.3](solution-architecture.md#93-decision-gates) | Go or hold |

**Two items that must never drop off the monthly agenda.** Open suppressions, because each one is a rule that is not protecting anything. And dimensions below threshold for two consecutive periods, because the second period converts a warning into a governance finding — the mechanism that keeps the steward accountable for the number rather than for the meeting.

---

## How this document is maintained

| | |
|---|---|
| Owner | Platform lead |
| Review | Monthly at Platform Ops; annually at Design Authority |
| Change control | Pull request; no approval from the architecture board required, because it records procedure rather than decision |
| Escalation rule | If a change here would alter a decision, invariant or component boundary, it is **not** an edit to this document — it is a decision-log entry in the Solution Architecture first (D-011) |
| Size limit | Roughly 15 pages. Beyond that, extract runbooks and the steward quickstart into their own documents rather than growing this one |

**Why the size limit matters.** This document is read by people under pressure, usually mid-incident or mid-adoption. The moment it becomes a reference work rather than a working manual, it stops being read and the procedures revert to tribal knowledge.




