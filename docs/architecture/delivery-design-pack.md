# Delivery Design Pack

### Build contracts for the DQ Guardrail control and data planes

| | |
|---|---|
| **Version** | 0.1 |
| **Status** | In use from Wave M1 |
| **Audience** | Delivery squads, QA, platform, SRE |
| **Normative for** | Artefact meta-schemas, the `dqctl` CLI contract, environment and promotion model, configuration precedence |
| **Cites** | [Solution Architecture](solution-architecture.md) — decisions, `C1–C12`, `S1–S7`, `KD-1..3`, `M0–M7` · [Operating Model](control-plane-operating-model.md) — procedures and lifecycle |
| **Owner** | Delivery lead |
| **Change control** | Pull request; reviewed at each wave gate |
| **Parent decision** | D-012 in [Appendix E](solution-architecture.md#appendix-e--decision-log) |

> **Purpose.** The architecture specifies *what* and *why*; the operating model specifies *how it is run*. This pack specifies **what a squad codes against**. It adds no architectural decisions. Where the architecture left a specification question open, the resolution is recorded in [§7](#7-resolutions-of-open-specification-questions) against the decision it refines.

## Contents

- **1** · [How this pack is used](#1-how-this-pack-is-used) — [standards conformance](#11-standards-conformance)
- **2** · [Artefact meta-schemas](#2-artefact-meta-schemas) — [intent registry](#20-global-intent-registry) · [object metadata](#21-object-metadata) · [pack](#22-quality-pack) · [intent pack](#23-intent-pack) · [binding CRD](#24-qualitybinding-crd) · [file manifest](#25-file-delivery-manifest) · [violation](#26-violation-object)
- **3** · [`dqctl` command contract](#3-dqctl-command-contract)
- **4** · [Environments and promotion](#4-environments-and-promotion)
- **5** · [Configuration precedence](#5-configuration-precedence)
- **6** · [Coverage of the delivery gaps](#6-coverage-of-the-delivery-gaps)
- **7** · [Resolutions of open specification questions](#7-resolutions-of-open-specification-questions)
- **8** · [Test strategy](#8-test-strategy)
- **A** · [A complete minimal object](#appendix-a--a-complete-minimal-object)
- **B** · [What this pack does not cover](#appendix-b--what-this-pack-does-not-cover)
- **C** · [Requirements traceability matrix](#appendix-c--requirements-traceability-matrix)

---

## 1. How this pack is used

| Role | Uses it for | Section |
|---|---|---|
| Steward | The exact shape of the metadata file they maintain | 2.1 |
| Domain architect | The pack grammar, required fixtures, ID rules | 2.2, 2.3 |
| Engine developer | The violation object, bundle consumption, fallback rules | 2.6, 5 |
| Platform engineer | The CRD contract, environments, promotion | 2.4, 4 |
| Pipeline engineer | Exit codes, JSON output, gate wiring | 3 |
| Batch developer | The manifest contract and reject schema | 2.5 |
| QA | Acceptance surface: what can be asserted at each gate | 3, 4, 6 |

**Three rules of use.** Every schema here is a **contract, not an example** — if code and this pack disagree, the pack is wrong and is fixed in the same pull request. Every example is **minimal but valid**. Anything marked *implementation choice* is deliberately left to the squad.

### 1.1 Standards conformance

Where a public standard exists, this pack adopts it. Where none fits, the deviation is declared rather than hidden — a reviewer should be able to tell the two apart without asking.

| Standard | Where it applies | Posture |
|---|---|---|
| **JSON Schema 2020-12** | §2.1–2.3, the canonical object contract | **Conformant** — `$schema` declared, `$defs` used, `format` treated as annotation so nothing depends on an assertion vocabulary |
| **SemVer 2.0.0** | Every `version` field | **Conformant** — pre-release and build metadata admitted; `dqctl` applies precedence ordering |
| **Kubernetes API conventions** + `metav1.Condition` | §2.4 | **Conformant** — spec/status split, conditions with `observedGeneration`, printer columns, deliberately no `phase` field |
| **OCI distribution spec** | Bundle reference and digest | **Conformant with a documented alias** — canonical form is scheme-less; a leading `oci://` is normalised |
| **Sigstore / cosign** | Signing and attestation (§3) | **Conformant** — keyless, with SBOM and provenance as attestations on the published artefact |
| **CycloneDX 1.5** | SBOM format | **Adopted** — chosen over SPDX for tooling ubiquity in this estate |
| **SLSA v1.0** | Build provenance | **Partially adopted** — provenance is produced; the build level is declared at M7, not claimed now |
| **RFC 9457** | The 422 problem+json rendering | **Conformant** — all five members, extensions namespaced |
| **ISO/IEC 25012** | The six quality dimensions (Architecture §7.6) | **Adopted subset** — validity is added; compliance, portability and recoverability are excluded because the guardrail does not enforce them |
| **DAMA-DMBOK** | Dimension vocabulary and scorecard terms | **Referenced** — vocabulary only, not a conformance target |
| **Bitol ODCS v3** | The business layer of the object contract | **Partially aligned** — §2.1 carries the same concepts in a smaller file; the runtime needs JSON Schema, not a contract document |
| **RFC 4180 / JSON Lines** | File channel formats | **Referenced** — dialect declared in the manifest rather than assumed |
| **ISO/IEC/IEEE 29119 / 29148** | Test levels (§8) and requirements (Appendix C) | **Aligned in substance, not vocabulary** — T0–T6 and R/N identifiers map to these standards without claiming certification |
| **reproducible-builds.org** | Bundling determinism (`SOURCE_DATE_EPOCH`) | **Adopted** |

**Declared deviations — two, both deliberate:**

| Deviation | From | Why |
|---|---|---|
| Exit-code registry of 0–4 | `sysexits.h` (64–78) | CI needs to distinguish "content is wrong" from "toolchain is broken" far more than it needs sysexits granularity. The registry in §3 is the contract |
| `correlationId` is a ULID | W3C Trace Context `traceparent` | The ID must survive a batch job that has no trace context. Where an OTel SDK is present, `traceparent` is carried in addition rather than instead |

---
## 2. Artefact meta-schemas

Five artefact families plus two wire contracts. Each is versioned under `catalog/schemas/` and referenced by `$id`.

### 2.0 Global intent registry

The address key `{intent}-{businessObject}` is only machine-resolvable if intents are a **closed, governed list**. They are declared once, not invented per object.

`catalog/intents.yaml`

```yaml
apiVersion: dq/v1
kind: IntentRegistry
intents:
  - name: create
    description: Bring a new object into existence; the identifier is server-assigned
    owner: domain-architecture
    approvedOn: 2026-09-14
  - name: amend
    description: Modify an existing object; the identifier must be supplied
    owner: domain-architecture
    approvedOn: 2026-09-14
  - name: query
    description: Read path; partial projections permitted
    owner: domain-architecture
    approvedOn: 2026-09-14
```

| Rule | Enforcement |
|---|---|
| An intent may appear in `meta.yaml` only if it exists here | `dqctl lint` |
| `query` is read-only and never appears on a write channel | `dqctl lint` |
| Adding an intent is a governed change, not free text | Two approvals per [Operating Model §10](control-plane-operating-model.md#10-onboarding-a-workload-or-an-object) |

### 2.1 Object metadata

`catalog/schemas/object-metadata.schema.yaml` — validates every `*.meta.yaml`.

```yaml
$schema: https://json-schema.org/draft/2020-12/schema
$id: https://registry.internal/dq/schemas/object-metadata.schema.yaml
type: object
additionalProperties: false
required: [object, version, owner, steward, criticality, sensitivity, allowedIntents, thresholds]
properties:
  object:       { type: string, pattern: '^[a-z][a-z0-9]*$' }
  version:      { type: string, pattern: '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$' }
  owner:        { type: string, minLength: 2 }            # team, not an individual
  steward:      { type: object, required: [name, email] }  # named human, owns the scorecard
  criticality:  { enum: [P1, P2, P3] }
  sensitivity:  { enum: [PUBLIC, INTERNAL, CONFIDENTIAL, RESTRICTED] }
  retention:    { type: string, pattern: '^P[0-9]+(D|M|Y)$' }
  allowedIntents:
    type: array
    minItems: 1
    items: { type: string, pattern: '^[a-z][a-z0-9]*$' }
  eventTimeField: { type: string }                        # required when timeliness is enforced
  thresholds:                                             # percentages; defaults per Architecture §7.6
    type: object
    additionalProperties: false
    properties:
      completeness: { type: number, minimum: 0, maximum: 100 }
      validity:     { type: number, minimum: 0, maximum: 100 }
      accuracy:     { type: number, minimum: 0, maximum: 100 }
      consistency:  { type: number, minimum: 0, maximum: 100 }
      uniqueness:   { type: number, minimum: 0, maximum: 100 }
      timeliness:   { type: number, minimum: 0, maximum: 100 }
```

**Cross-field validations** enforced by `dqctl lint`, because a YAML schema alone cannot express them:

| Rule | Rationale |
|---|---|
| Every `allowedIntents` entry exists in `catalog/intents.yaml` | Keeps the address key closed |
| `eventTimeField` is present if `thresholds.timeliness` is set | Timeliness is unmeasurable without it |
| `sensitivity: RESTRICTED` requires at least one `x-dq-sensitive` field in the schema | Classification must reach the contract, not stop at metadata |

**Durations are ISO 8601** — `P7Y`, `P90D`, `P18M`. `M` means months; minutes are not permitted, which removes the month-or-minute ambiguity of a bare `7m` and makes the value machine-comparable without a parser convention.

### 2.2 Quality pack

`catalog/schemas/quality-pack.schema.yaml`. Required structure, abbreviated to the constraints that matter.

```yaml
$schema: https://json-schema.org/draft/2020-12/schema
$id: https://registry.internal/dq/schemas/quality-pack.schema.yaml
type: object
required: [apiVersion, kind, metadata, spec]
properties:
  apiVersion: { const: dq/v1 }
  kind:       { const: QualityPack }
  metadata:
    required: [name, version, owner]
    properties:
      name:    { type: string, pattern: '^object\.[a-z][a-z0-9]*$|^intent\.[a-z][a-z0-9]*$' }
      version: { type: string, pattern: '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$' }
      owner:   { type: string }
  spec:
    required: [object]
    properties:
      object:
        required: [name, schemaVersion]
      requires: { type: array, items: { type: string } }
      syntax:   { type: object }        # fromSchema plus optional extras
      semantic: { type: array, items: { $ref: '#/$defs/rule' } }
      stateful: { type: array, items: { $ref: '#/$defs/rule' } }
      tests:    { type: array, minItems: 1, items: { $ref: '#/$defs/fixture' } }
$defs:
  rule:
    required: [id, assert, severity, dimension]
    properties:
      id:        { type: string, pattern: '^(SYN|SEM|STA)-[A-Z]{3}-[0-9]{3}$' }
      when:      { type: string }                 # CEL; optional guard
      assert:    { type: string }                 # CEL; must evaluate to bool
      message:   { type: string, maxLength: 200 }
      severity:  { enum: [CRITICAL, MAJOR, MINOR] }
      dimension: { enum: [completeness, validity, accuracy, consistency, uniqueness, timeliness] }
      needs:     { type: array, items: { enum: [valueset, refdata, history, delta] } }
      costLimit: { type: integer, minimum: 100 }
      lifecycle: { enum: [active, retired] }
      successor: { type: string }                 # rule id; required when retired
  fixture:
    required: [rule, expect, input]
    properties:
      rule:   { type: string }
      expect: { enum: [pass, fail, edge] }
      input:  { type: object }
```

**Versioning.** Every `version` field is SemVer 2.0.0. The pattern above admits pre-release and build metadata, and `dqctl` applies strict SemVer precedence when ordering versions — so `1.0.0-rc.1` sorts before `1.0.0`, and `1.0.0+build.7` is equal in precedence to `1.0.0`.

**Rule-ID allocation.** The prefix encodes the tier (`SYN`, `SEM`, `STA`), the middle segment is the object's three-letter code, the last is a three-digit sequence allocated by `dqctl scaffold` from the highest existing value. IDs are **never reused**, including after retirement — that is what makes S3 and S6 hold together.

**Compiler-enforced constraints** beyond the schema:

| Constraint | Why |
|---|---|
| Every `semantic` and `stateful` rule has at least one `pass` and one `fail` fixture | Without both, L5 has a hole and nobody notices |
| `retired` requires a `successor` that exists and is `active` | Prevents retiring a rule into a gap |
| `needs` must be satisfied by providers declared in the bundle manifest | Otherwise the bundle is unbootable by design |
| `message` must not interpolate any `x-dq-sensitive` field | The redaction rule in Architecture §3 must be machine-checkable |

### 2.3 Intent pack

`catalog/schemas/intent-pack.schema.yaml`. Deliberately narrower than the object pack — an intent may add obligations, never subtract them.

```yaml
$schema: https://json-schema.org/draft/2020-12/schema
$id: https://registry.internal/dq/schemas/intent-pack.schema.yaml
type: object
required: [apiVersion, kind, metadata, spec]
properties:
  apiVersion: { const: dq/v1 }
  kind:       { const: IntentPack }
  metadata:
    required: [name, version, owner]
    properties:
      name:    { type: string, pattern: '^intent\.[a-z][a-z0-9]*$' }
      version: { type: string, pattern: '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$' }
      owner:   { type: string }
  spec:
    additionalProperties: false       # no `syntax`, no `fromSchema` — the object owns structure
    required: [intent, rules]
    properties:
      intent:    { type: string, pattern: '^[a-z][a-z0-9]*$' }
      appliesTo: { type: array, minItems: 1, items: { type: string } }
      rules:     { type: array, minItems: 1, items: { $ref: 'quality-pack.schema.yaml#/$defs/rule' } }
      profile:
        properties:
          schemaUses:         { enum: [request, response] }
          expectPrecondition: { type: boolean }   # e.g. If-Match on amend
      relax:                                   # the only widening mechanism
        type: array
        items:
          required: [rule, justification, approvedBy, expiresAt]
```

### 2.4 QualityBinding CRD

The only new Kubernetes surface. Specified here so admission behaviour is designed rather than discovered mid-build. The resource is shown in **three blocks that must be applied as one object** — they are split only for readability. **Block 1 of 3 — the resource surface.**

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: qualitybindings.dq.example.com
spec:
  group: dq.example.com
  scope: Namespaced
  names: { kind: QualityBinding, plural: qualitybindings, singular: qualitybinding, shortNames: [qb] }
  versions:
    - name: v1
      served: true
      storage: true
      subresources:
        status: {}
      additionalPrinterColumns:
        - { name: Mode,     type: string, jsonPath: .spec.mode }
        - { name: Digest,   type: string, jsonPath: .spec.bundle }
        - { name: Observed, type: string, jsonPath: .status.observedDigest }
        - { name: Age,      type: date,   jsonPath: .metadata.creationTimestamp }
      schema:
        openAPIV3Schema:
          type: object
          required: [spec]
          properties:
            spec:
              type: object
              additionalProperties: false
              required: [bundle, mode]
              properties:
                bundle:
                  type: string
                  pattern: '^[^@\s]+@sha256:[a-f0-9]{64}$'    # OCI reference; digest, never a tag
                fallbackDigest: { type: string, pattern: '^sha256:[a-f0-9]{64}$' }
                mode:           { enum: [OFF, SHADOW, WARN, ENFORCE] }
                onBundleFailure:
                  enum: [fail-open, fail-closed]
                  default: fail-open
                retention: { type: string, pattern: '^P[0-9]+(D|M|Y)$' }
                suppress:
                  type: array
                  maxItems: 20
                  x-kubernetes-list-type: map
                  x-kubernetes-list-map-keys: [rule]
                  items:
                    type: object
                    additionalProperties: false
                    required: [rule, until, ticket, approver, reason]
                    properties:
                      rule:     { type: string, pattern: '^(SYN|SEM|STA)-[A-Z]{3}-[0-9]{3}$' }
                      until:
                        type: string
                        pattern: '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$'
                      ticket:   { type: string, minLength: 3 }
                      approver: { type: string, minLength: 3 }
                      reason:   { type: string, maxLength: 200 }
                channels:
                  type: object
                  additionalProperties: false
                  properties:
                    http:
                      type: object
                      additionalProperties: false
                      properties:
                        adapter:     { enum: [wasm, ext-proc, library] }
                        paths:       { type: array, items: { type: string } }
                        onViolation:
                          type: object
                          additionalProperties: false
                          properties:
                            CRITICAL: { enum: [reject-422, log] }
                            MAJOR:    { enum: [reject-422, log] }
                            MINOR:    { enum: [reject-422, log] }
                    kafka:
                      type: object
                      additionalProperties: false
                      properties:
                        adapter: { enum: [stream-gate, interceptor] }
                    file:
                      type: object
                      additionalProperties: false
                      properties:
                        adapter: { enum: [gate-job] }
                        policy:  { enum: [reject-rows, fail-file] }
```

**Block 2 of 3 — admission rules**, appended under `spec.versions[0].schema.openAPIV3Schema.properties.spec`, so the API server rejects an invalid binding before any controller sees it:

```yaml
            x-kubernetes-validations:
              - rule: "self.mode != 'ENFORCE' || has(self.fallbackDigest)"
                message: "ENFORCE requires a declared fallbackDigest"
              - rule: "!has(self.suppress) || self.suppress.all(s, s.until != '' && s.approver != '')"
                message: "every suppression requires an expiry and a named approver"
              - rule: "!has(self.suppress) || self.suppress.all(s, timestamp(s.until) > now && timestamp(s.until) <= now + duration('720h'))"
                message: "a suppression must expire in the future and no more than 30 days ahead"
              - rule: "self.mode == 'OFF' || has(self.channels)"
                message: "a bound workload must declare at least one channel"
```

**Block 3 of 3 — status schema**, a sibling of `spec` under `spec.versions[0].schema.openAPIV3Schema.properties`, so `kubectl get qb` answers "is it live?" without a dashboard:

```yaml
            status:
              type: object
              properties:
                observedDigest:  { type: string }
                bundleSource:    { enum: [primary, fallback] }
                adoptedAt:       { type: string, format: date-time }
                suppressedRules: { type: array, items: { type: string } }
                conditions:
                  type: array
                  items:
                    type: object
                    required: [type, status, lastTransitionTime]
                    properties:
                      type:               { type: string }
                      status:             { enum: ["True", "False", "Unknown"] }
                      reason:             { type: string }
                      message:            { type: string }
                      observedGeneration: { type: integer, format: int64 }
                      lastTransitionTime: { type: string, format: date-time }
```

| Condition type | Meaning when `True` |
|---|---|
| `Ready` | The requested digest is loaded and enforcing as declared |
| `BundleVerified` | Signature and digest verified; `False` means the workload is on its fallback |
| `DigestDrift` | Observed digest differs from `spec.bundle` — rollout incomplete |
| `SuppressionExpiring` | A suppression expires within 7 days |

**Operator responsibilities** — deliberately small, and explicitly closed:

| Responsibility | Detail |
|---|---|
| Write `status.observedDigest` and `bundleSource` | Reflect what workloads actually loaded, never what the spec requested |
| Emit the four conditions | One per table row above |
| Do **not** mutate `spec` | The operator never changes what a human declared |
| No finalizer in v1 | Bundle cleanup is retention-driven, not deletion-driven; deleting a binding must not delete evidence |

**OCI reference form.** The canonical reference is scheme-less — `ghcr.io/org/dq-bundle@sha256:<64 hex>` — because `oci://` is a transport convention used by Helm and `oras`, not an IANA-registered scheme. `dqctl` accepts a leading `oci://` and normalises it, so references copied from those tools work unchanged. The CRD pattern is deliberately loose about host and path (the registry validates those) and strict about the digest, which is the only part that must be immutable.

### 2.5 File delivery manifest

The contract between an upstream extract and the gate job. Referenced eight times in the architecture and previously specified nowhere.

`<intent>-<object>-<yyyyMMdd>.manifest.json`, delivered alongside the data file.

```jsonc
{
  "schemaVersion": "1.0",
  "intent": "create",                       // must exist in the intent registry
  "object": "customer",
  "objectVersion": "1.4.0",
  "deliveryId": "01J8Z6Q4T7",               // ULID; used as correlationId for every violation
  "producedAt": "2026-10-24T01:40:00Z",
  "source":  { "system": "crm-extract", "run": "2026-10-24T01:30Z" },
  "format":  {
    "type": "jsonl",                        // jsonl | csv | parquet
    "encoding": "utf-8",
    "recordSeparator": "\n",
    "csv": { "delimiter": ",", "quote": "\"", "header": true }   // when type is csv
  },
  "recordCount": 120000,
  "checksum": { "algorithm": "sha256", "value": "9f2c…" },
  "retention": "P7Y"
}
```

| Rule | Enforcement |
|---|---|
| `intent` and `object` resolve in the registry, and the pair is in the object's `allowedIntents` | Gate job refuses the file before reading a record |
| `checksum` matches the data file | Gate job; mismatch fails the file, never a partial read |
| `recordCount` matches the accepted + rejected count | Manifest closure check at the end of the pass |
| `format.type` is supported by the deployed gate-job version | Gate job reports `UNSUPPORTED_FORMAT` and fails closed |

**Output contract** written by the gate job into `accepted/`:

| Artefact | Content |
|---|---|
| `accepted/<original-name>` | Records that passed, byte-identical to the input format |
| `rejects.csv` | `row, ruleId, severity, path, message` — one row per violation, never the field value |
| `result.manifest.json` | Input manifest echoed, plus `acceptedCount`, `rejectedCount`, `packDigest`, `evaluatedAt`, `gateJobVersion` |
| `_SUCCESS` | Written last, only on success — downstream triggers on this, never on the raw drop |

### 2.6 Violation object

`catalog/schemas/violation.schema.yaml` — the single object from which all three channel renderings are produced.

```jsonc
{
  "schemaVersion": "1.0",
  "violations": [
    {
      "ruleId": "SEM-CUS-001",
      "severity": "CRITICAL",
      "path": "$.dateOfBirth",
      "dimension": "accuracy",
      "intent": "create",
      "object": "customer",
      "objectVersion": "1.4.0",
      "packDigest": "sha256:9f2c…",
      "correlationId": "01J8Z6Q4T7",
      "message": "Verified customers must be 18 or older",
      "retryable": false
    }
  ]
}
```

| Field | Constraint | Why |
|---|---|---|
| `ruleId` | `^(SYN|SEM|STA)-[A-Z]{3}-[0-9]{3}$` | Joins to the registry and to a Git commit |
| `severity` | `CRITICAL / MAJOR / MINOR` | Selects channel behaviour |
| `path` | `^\$(\.[A-Za-z_][A-Za-z0-9_]*|\[[0-9]+\])*$` | Machine-actionable, and safe to log |
| `dimension` | Six-value enum | Rolls into the scorecard |
| `packDigest` | `^sha256:[a-f0-9]{64}$` | Proves which rules ran (S6) |
| `correlationId` | ULID | Joins HTTP, Kafka and file evidence for one record |
| `message` | ≤ 200 chars, no interpolated values | Redaction is enforced by the compiler |
| **`retryable`** | boolean | **Distinguishes "the data is bad" from "we could not tell"** |

`retryable: true` is set when the failure cause was a provider outage or a skipped dependency rather than the record itself. It is the field that lets a downstream consumer decide between quarantining and retrying, and it is the field most often missing from home-grown validation contracts.

**HTTP rendering** — `422 application/problem+json` per RFC 9457:

```jsonc
{
  "type": "https://errors.example.com/dq/violation",
  "title": "Data quality validation failed",
  "status": 422,
  "detail": "3 rule violations for intent create on customer@1.4.0",
  "instance": "/create-customer",
  "correlationId": "01J8Z6Q4T7",
  "packDigest": "sha256:9f2c…",
  "violations": [ /* as above */ ]
}
```

**Failure codes — not violations.** These are platform failures and must be separable in metrics, because they reduce *coverage* rather than quality:

| Code | HTTP | Meaning | Counts against |
|---|---|---|---|
| `DQ_BUNDLE_UNVERIFIED` | 503 | Digest or signature verification failed | Coverage SLI |
| `DQ_PAYLOAD_TOO_LARGE` | 413 | Exceeds `maxRequestBytes` | Coverage SLI |
| `DQ_UNSUPPORTED_MEDIA_TYPE` | 415 | Body is not `application/json` | Coverage SLI |
| `DQ_INTENT_UNKNOWN` | 400 | Address key not resolvable in this binding | Coverage SLI |
| `DQ_RULE_ERROR` | 500 | CEL evaluation or schema-matcher fault | Availability SLI |
| `DQ_UNSUPPORTED_FORMAT` | n/a | File format not supported by this gate-job version | Availability SLI |

---
## 3. `dqctl` command contract

Thirteen verbs. The three properties that matter to a pipeline builder: **exit codes are semantically distinct**, **`--json` output is a single stable document on stdout**, and **read-only verbs never mutate anything**.

| Verb | Purpose | Key arguments | Mutates | Exit codes |
|---|---|---|---|---|
| `lint` | Validate artefact shapes and the cross-field rules of §2 | `--changed-only` | no | 0, 1, 3 |
| `align` | L1/L3/L4 contract-to-schema alignment | `--check`, `--paths` | no | 0, 1, 3 |
| `generate` | Write derived artefacts (`x-dq-rules`, fixtures, catalogue) | `--check` | yes / check only | 0, 1, 3 |
| `test` | Run rule fixtures | `--rule`, `--fixtures` | no | 0, 1, 3 |
| `bundle` | Compile and package | `--out`, `--object` | yes | 0, 3 |
| `publish` | Push the bundle as an OCI artefact and print its digest reference; signing is a separate CI step | `--bundle`, `--registry` | yes (registry) | 0, 3, 4 |
| `verify` | Verify signature, digest and content | `--bundle` | no | 0, 3 |
| `status` | What is live, and since when | `--object`, `--rule`, `--binding`, `--history` | no | 0, 1, 3 |
| `impact` | Consumers of an object version | `object@version` | no | 0, 1 |
| `diff` | What changes between two digests for a workload | `D1 D2 --binding` | no | 0, 3 |
| `explain` | Why a specific record passed or failed | `--record`, `--intent`, `--object` | no | 0, 1 |
| `scaffold` | Create object, rule or PR skeleton | `--object`, `--rule`, `--intent` | yes | 0, 2 |
| `replay` | Re-produce DLQ records | `--topic`, `--dlq`, `--limit` | yes (topic) | 0, 1, 4 |

### Exit-code registry

| Code | Meaning | Who acts |
|---|---|---|
| `0` | Success | — |
| `1` | **Check failed**: drift, violation or failed fixture — the content is wrong | The author changes content |
| `2` | Usage error: unknown flag, missing argument | The pipeline or the author's command |
| `3` | **Unbuildable input**: invalid artefact, malformed bundle, unsatisfiable provider | The author or the toolchain owner |
| `4` | Infrastructure fault: registry unreachable, signature service down, OOM | Retry; escalate to platform |

The `1` versus `3` distinction is deliberate and load-bearing: **`1` means the developer must change something, `3` means the toolchain or config is broken.** Pipelines should fail fast on `3` and annotate on `1`; conflating them produces retry storms on content errors.

### Output contract

`--json` writes exactly one JSON document to **stdout**; human-readable progress goes to **stderr**. No log lines on stdout, ever — that is what makes the output parseable.

```jsonc
{
  "schemaVersion": "1",
  "ok": false,
  "command": "align",
  "version": "0.4.1",
  "durationMs": 1840,
  "findings": [
    {
      "code": "DQ_L3_CONTRACT_LOOSER",
      "severity": "error",
      "path": "spec/openapi/customer-api.yaml#/paths/~1create-customer",
      "message": "required field 'countryOfResidence' dropped from request schema",
      "ruleId": null
    }
  ],
  "artifacts": { "bundle": null }
}
```

| Field | Contract |
|---|---|
| `schemaVersion` | Output-contract major version. Incremented only for a breaking change to this document's shape, never for a new finding code |
| `ok` | `true` only when every finding has severity below the threshold |
| `findings[].code` | Stable identifier from the code registry; never renumbered |
| `findings[].severity` | `error`, `warn`, `info` |
| `durationMs` | Wall clock, for CI budget tracking |

### Reproducibility

`dqctl bundle` must be **deterministic**: identical inputs produce an identical digest. That requires sorted map keys, normalised line endings, and timestamps taken from `SOURCE_DATE_EPOCH` rather than the wall clock. Without this, every build produces a new digest, every binding bump looks like a change, and the audit story in S6 collapses into noise.

### Pipeline wiring

```bash
set -euo pipefail
dqctl lint   --changed-only --json > lint.json
dqctl align  --check        --json > align.json
dqctl test                  --json > test.json          # exit 1 = content, 3 = toolchain

# compile, then publish as an OCI artefact; publish prints the digest reference
dqctl bundle --out dist/dq-bundle.tar.gz
ref=$(dqctl publish --bundle dist/dq-bundle.tar.gz --registry ghcr.io/org/dq-bundle)
#   → ghcr.io/org/dq-bundle@sha256:<64 hex>

# signing is the pipeline's concern: the identity belongs to CI, not to dqctl
cosign sign --yes "$ref"
syft "$ref" -o cyclonedx-json > sbom.cdx.json
cosign attest --yes --predicate sbom.cdx.json --type cyclonedx "$ref"
cosign verify "$ref" \
  --certificate-identity-regexp "$CI_IDENTITY" \
  --certificate-oidc-issuer "$CI_ISSUER"
```

---
## 4. Environments and promotion

Four environments. The rule artefact is built **once**; it is adopted per environment. Only the engine image is environment-specific, and only by version.

| | **DEV** | **SIT** | **UAT** | **PROD** |
|---|---|---|---|---|
| Purpose | Author and unit-test rules | Integration across channels | Business acceptance and shadow evidence | Live enforcement |
| Who deploys | Author, self-service | Pipeline on merge | Pipeline on release cut | Pipeline on change approval |
| Data | Synthetic fixtures only | Synthetic, generated from fixtures | Masked production extract | Production |
| Default rule mode | `SHADOW` | `SHADOW` | `SHADOW` then `WARN` | `ENFORCE` |
| Binding owner | Author's namespace | Platform | Platform | Platform |
| Promotion gate to enter | `dqctl test` green | `dqctl align --check` green on merge | One shadow cycle, no blocking violation | ARB change approval + G2 passed |
| Registry | `ghcr.io/<org>/dq-bundle-dev` | shared | shared | `ghcr.io/<org>/dq-bundle` |
| Cosign identity | Developer OIDC | CI workload identity | CI workload identity | CI workload identity, verified at admission |

**Two decisions inside that table are load-bearing and should be stated plainly.**

*Build once, adopt everywhere.* A rule change is compiled in CI and the **same digest** is adopted in SIT, UAT and PROD. Rebuilding per environment defeats the alignment guarantee — the artefact that was tested is the artefact that runs. Environment differences live in bindings and overlays, never in a rebuild.

*PROD is the only environment permitted to use `ENFORCE` on write traffic.* DEV and SIT run `SHADOW` regardless of what the binding says, enforced by an overlay rather than by convention. This is what makes it safe for an author to set `ENFORCE` in a test binding and forget.

### 4.1 Rule promotion path

```
author PR ──► CI (lint/align/test/bundle) ──► publish + sign Dn
                                                     │
   DEV  adopt Dn (SHADOW) ──► SIT adopt Dn (SHADOW) ──► UAT adopt Dn (SHADOW→WARN)
                                                              │
                                                     shadow evidence + gate G2
                                                              ▼
                                                     PROD adopt Dn (WARN→ENFORCE)
```

Each arrow is **one binding change and the same digest**. No rebuild, no re-sign, no "promote the artefact" step that could produce a different artefact. Rollback at any stage is the inverse binding change.

### 4.2 Engine and adapter release

The engine is a versioned product in its own right, with its own compatibility obligations.

| Artefact | Versioning | Compatibility rule |
|---|---|---|
| Engine image (sidecar, library, gate job) | SemVer | Minor versions must load bundles produced by any earlier minor within the same major |
| WASM filter module | SemVer, built per engine release | Pinned to an Envoy minor version; see matrix below |
| `dqctl` | SemVer | Must be able to compile every artefact schema major version it claims |
| Bundle format (`bundle.yaml`) | `bundleFormat: 2` | Engine refuses a bundle format it does not implement, rather than guessing. 2 adds `valueSets` and `intents`, so a bundle cannot carry governed enumerations no manifest describes |

**Envoy compatibility matrix** — maintained per engine release, because this is where silent breakage lives:

| Engine | Envoy 1.30 | 1.31 | 1.32 | 1.33 |
|---|---|---|---|---|
| 0.x (M3) | ✓ | ✓ | ✓ | test-only |
| Later | declared at release | | | |

An Envoy upgrade is treated as a **rule-change-class event**: the M0 micro-benchmark is re-run and the latency SLO re-confirmed before the new proxy version reaches PROD. Upgrading a proxy under a validation filter without re-measuring is how a 100 µs budget becomes a 2 ms incident.

### 4.3 Environment overlays

An overlay is Helm values, applied after the binding and never instead of it. Permitted overlay keys only:

| Key | Effect | PROD allowed |
|---|---|---|
| `mode` | Downgrade only (`ENFORCE` → `WARN` → `SHADOW` → `OFF`) | yes, via break-glass |
| `logLevel` | Engine verbosity | yes |
| `maxRequestBytes` | Body ceiling | yes, upward only |
| `resources` | Requests and limits | yes |
| `channels.*.paths` | Narrow the bound path set | yes, narrowing only |

The overlay **cannot** add rules, change a digest, or enable a channel the binding did not declare. That restriction is what keeps "it works in SIT" meaningful.

### 4.4 Secrets and identity

| Secret | Scope | Storage | Rotation |
|---|---|---|---|
| Registry pull credential | Per cluster | `imagePullSecrets`, external secret operator | 90 days |
| Cosign verification key or OIDC trust root | Cluster-wide | Admission policy config | On CA rotation |
| Provider credentials (live `RefDataLookup`) | Per namespace | External secret operator | 90 days |
| Breaking-glass approval token | Human | IdP group membership | With joiner/leaver |

No secret is ever placed in a binding. The binding is configuration under review; secrets are references resolved at runtime.

---
## 5. Configuration precedence

Five layers. Later wins, with the exceptions below stated explicitly because they are where two implementers diverge.

| # | Layer | Source | Scope of authority |
|---|---|---|---|
| 1 | Built-in defaults | Engine binary | Lowest |
| 2 | Bundle manifest | `bundle.yaml` inside the digest | Rules, providers, `relax` blocks, bundle format |
| 3 | Object metadata | `*.meta.yaml`, compiled into the bundle | Thresholds, retention, allowed intents |
| 4 | Binding spec | `QualityBinding.spec` | Mode, channels, suppression, `fallbackDigest`, `onBundleFailure` |
| 5 | Environment overlay | Helm values | Narrow or downgrade only |

| Situation | Resolution | Why |
|---|---|---|
| `mode: OFF` with a `suppress` entry | `OFF` wins; suppression is inert | Nothing is enforced, so a suppression means nothing |
| Suppression names a rule not in the resolved plan | `dqctl lint` error, not a silent no-op | A suppression that protects nothing is worse than none — it reads as safety |
| Suppression past its `until` date | Rule enforces again; `SuppressionExpiring` goes `False`; `dq.rule.suppressionExpired` emitted once | Expiry is a safety property, not a data field |
| Primary `bundle` fails fetch or verification | Try `fallbackDigest`; if absent or mismatched, apply `onBundleFailure` | Deterministic, and visible through `bundleSource` |
| Bundle declares a provider the binding does not supply | Bundle refuses to boot — CI-time error, then start-up error | Fail where it is cheap |
| Overlay attempts `ENFORCE` in DEV or SIT | Rejected by admission policy | Prevents "it was enforcing in test" surprises |
| Two bindings match one workload | Namespace-scoped binding wins, warning emitted on both | Ambiguity must be visible, never silently resolved |

## 6. Coverage of the delivery gaps

Against the ten cross-cutting gaps and six specification defects identified in the delivery assessment.

| Ref | Gap | Status | Where |
|---|---|---|---|
| D1 | M3 depends on an undefined environment | **Closed** | §4 |
| D2 | `fallbackDigest` mechanism unrealised | **Closed** | §2.4, §5, §7 R-01/R-02 |
| D3 | `manifest.json` undefined | **Closed** | §2.5 |
| D4 | No CRD specification | **Closed** | §2.4 |
| D5 | Precedence undefined | **Closed** | §5 |
| D6 | `DQ-Pack-Digest` not checked in the contract | **Closed** | §7 R-05 |
| 1 | Environment and promotion model | **Closed** | §4 |
| 2 | Test strategy (levels, data, regression) | **Closed** | §8 |
| 3 | Acceptance criteria with load profile | **Closed** | §8.1 |
| 4 | Cutover and decommission per object | **Closed** | [Ops Guide §8](data-plane-operations-guide.md#8-cutover-and-decommission-per-object) |
| 5 | Interface catalogue | **Partial** — the three wire contracts are specified here; the inventory of operations, topics and consumers is not | §2.5, §2.6 |
| 6 | Metric, alert and dashboard catalogue | **Closed** | [Ops Guide §2](data-plane-operations-guide.md#2-metric-catalogue), [§3](data-plane-operations-guide.md#3-alert-catalogue), [§9](data-plane-operations-guide.md#9-dashboards) |
| 7 | Retention and records schedule | **Closed** | [Ops Guide §6](data-plane-operations-guide.md#6-retention-and-records-schedule) |
| 8 | Support model and runbooks | **Closed** | [Ops Guide §4](data-plane-operations-guide.md#4-runbooks), [§7](data-plane-operations-guide.md#7-support-model) |
| 9 | Requirements traceability matrix | **Closed** | Appendix C |
| 10 | Capacity, cost and sizing model | **Closed** | [Ops Guide §5](data-plane-operations-guide.md#5-capacity-and-sizing-model) |

**Fifteen of sixteen closed, one deliberately partial.** The remaining item — the interface-catalogue inventory — is deliberately not a document: it is generated by `dqctl impact`, because hand-maintaining an inventory of operations, topics and consumers would create exactly the second source of truth that invariant S1 exists to prevent.

---
## 7. Resolutions of open specification questions

Seven questions the architecture deliberately left to build time. Each is a **specification resolution**, not a new decision; where one materially changes a shipped contract it is marked and the decision-log entry is updated at the next gate.

| ID | Question | Resolution | Refines |
|---|---|---|---|
| **R-01** | Which image carries the "baked" bundle? | **Not upstream Envoy.** The DQ engine image (sidecar, library host, gate job) and the built WASM module each embed a default bundle at build time, recorded in the image manifest as `compiledBundleDigest`. A binding may set `fallbackDigest` **only** to a digest present in that image's manifest — so the fallback is always a digest that actually exists locally. | D-007 |
| **R-02** | What happens when the primary bundle cannot be verified? | Declared per binding. `onBundleFailure: fail-open` (default) serves traffic unvalidated and raises `BundleVerified=False`; `fail-closed` rejects with `DQ_BUNDLE_UNVERIFIED`. The CRD demands `fallbackDigest` whenever `mode: ENFORCE`, so the common case never reaches this branch. | D-007 |
| **R-03** | Does `suppress` override `mode`? | No. `mode` resolves last and wins; suppression is a filter over the plan and is inert at `OFF`. Stated because the intuitive reading is the opposite. | D-010 |
| **R-04** | Is `retryable` part of the violation contract? | Yes — a new field. It separates "the data is bad" from "we could not tell", which is exactly the decision a DLQ consumer must make. | D-006 |
| **R-05** | How is `DQ-Pack-Digest` kept inside the contract? | L2 lint gains a rule: every operation declaring `x-dq-object` must also declare the `DQ-Pack-Digest` header on its 2xx and 422 responses. The header stops being an undocumented side effect. | D-006, L2 |
| **R-06** | Are intents global or per object? | Global registry in `catalog/intents.yaml`, referenced per object by `allowedIntents`. Without this the address key is only closed by convention. | D-005 |
| **R-07** | Who signs the bundle, and with what identity? | The **pipeline**, using `cosign sign` on the OCI artefact, with SBOM and provenance attached as attestations. `dqctl` holds no signing identity and does not sign: the keys stay in CI where they are already governed, and the digest that is signed is the digest a binding pins. | D-007 |

---

## 8. Test strategy

Seven levels. Each proves a different claim, and a level that proves nothing is removed rather than tolerated.

| Level | What it proves | Runs where | Blocking gate | Owner |
|---|---|---|---|---|
| **T0 · Static** | Artefacts are well-formed and aligned | CI, seconds | Every PR (G1) | Author |
| **T1 · Rule unit** | Each rule accepts and rejects what it claims | CI, seconds, no cluster | Every PR (G1) | Domain architect |
| **T2 · Engine conformance** | The engine implements the tiers correctly | CI against the engine package | Every engine change | Platform |
| **T3 · Channel integration** | Each adapter behaves as specified | SIT | Merge to main | Platform |
| **T4 · Determinism** | The same record yields identical verdicts on every channel | SIT | Merge to main | Platform |
| **T5 · Non-functional** | Latency, throughput, degradation, security | SIT + PERF environment | Before each `ENFORCE` (G2) | SRE + Security |
| **T6 · Acceptance** | The business agrees the verdicts are right | UAT, shadow evidence | Before cutover (G3) | Steward + business |

### 8.1 Acceptance criteria and load profile

The NFR register in the architecture names a target and a "proven by" wave. This is the missing load profile that makes each one testable.

| NFR | Test | Load profile | Pass criterion |
|---|---|---|---|
| Added latency, HTTP | Benchmark with and without the filter | 500 rps sustained, payload p50 4 KB / p99 64 KB, 20-rule plan | p99 delta ≤ 1 ms **and** p99.9 ≤ 2 ms |
| Added latency, Kafka | Single-partition throughput test | 5,000 records/s, 2 KB records | p99 per-record ≤ 150 µs |
| File throughput | Large-file gate | 1,000,000-record JSONL, 40 columns | ≥ 10,000 records/s; memory flat within ±10% across the run |
| Determinism | Golden corpus through HTTP then Kafka | 1,000 records including boundary, null, unicode and malformed cases | Byte-identical violation sets, same `packDigest` |
| Coverage visibility | Fault injection: provider outage, cost limit, oversized payload | Normal traffic | Coverage falls **visibly**; no silent pass; `dq.rule.skipped` fires |
| No data loss on gate failure | Kill the job mid-file | 500k-record file, SIGKILL at 60% | No `_SUCCESS`; downstream not triggered; re-run is idempotent |
| Rule integrity | Deploy an unsigned bundle | SIT | Admission or start-up refuses; workload stays on its previous digest |
| Payload exposure | Penetration test and log review | — | No field values in violations, logs or rejects |
| Degrade ladder | Game day: each step down and back up | PROD-like | Every step is configuration-only, ≤ 5 minutes, measurable in D2 |

**No target is accepted from a vendor claim or a design estimate.** Each row's evidence is a recorded benchmark, and the M0 benchmark is what converts the architecture's estimates into commitments.

### 8.2 Test data

| Data class | Where | Source | Rule |
|---|---|---|---|
| Rule fixtures | In the pack (`tests:`) | Author | Must contain one pass and one fail per semantic rule |
| Golden corpus | `catalog/corpus/<object>/` | Generated plus curated edge cases | 1,000 records per object; versioned with the schema |
| Masked production extract | UAT only | Masked pipeline | Never reaches DEV or SIT |
| Production data | PROD only | Live | Never used as a test input outside shadow |

**The violation-to-fixture rule.** Every production violation that turns out to be a genuine defect becomes a permanent fixture in the object's corpus. This is the cheapest quality mechanism in the whole programme: it converts each incident into a regression test, and it costs the steward five minutes at the end of the review rather than a separate test-design exercise.

### 8.3 What each gate asserts

| Gate | Minimum evidence |
|---|---|
| **G1** (end M1) | T0 and T1 green on the wave-1 object; a deliberately loosened contract fails CI |
| **G2** (end M3) | T3, T4 and T5 green; game day executed; coverage SLI observed on real traffic in shadow |
| **G3** (end M6) | T6 signed off per object with S2 dual-run evidence; decommission plan approved |

---
## Appendix A — A complete minimal object


The smallest object that satisfies every contract in this pack. Four files, one pull request.

```
catalog/
  intents.yaml                          # §2.0 — unchanged, intents are global
  business-objects/
    account.v1.json                     # canonical schema
    account.meta.yaml                   # §2.1
  schemas/                              # the meta-schemas from §2, versioned
rules/object/
  account.v1.pack.yaml                  # §2.2
bindings/
  account-service.yaml                  # §2.4, as a CRD instance
```

```yaml
# catalog/business-objects/account.meta.yaml
object: account
version: 1.0.0
owner: customer-domain
steward: { name: A. Patel, email: a.patel@example.com }
criticality: P2
sensitivity: CONFIDENTIAL
retention: P7Y
allowedIntents: [create, amend, query]
thresholds: { completeness: 99.5, validity: 99.9, accuracy: 99 }
```

```yaml
# binding: one workload, one digest, two channels
apiVersion: dq.example.com/v1
kind: QualityBinding
metadata: { name: account-service, namespace: app-customer }
spec:
  bundle: ghcr.io/org/dq-bundle@sha256:abc123…
  fallbackDigest: sha256:def456…
  mode: SHADOW
  onBundleFailure: fail-open
  channels:
    http:
      adapter: wasm
      paths: ["/create-account", "/amend-account"]
    kafka:
      adapter: stream-gate
```

Expected pipeline output on a clean PR:

```console
$ dqctl lint --changed-only --json | jq '.ok, (.findings|length)'
true
0
$ dqctl align --check --json | jq -r '.findings[]?.code'   # no output = aligned
$ dqctl test --json | jq -r '.findings[]? | "\(.code) \(.path)"'
DQ_FIXTURE_MISSING rules/object/account.v1.pack.yaml#SEM-ACC-001
```

The third command is the one that most often fails on a first attempt, and it is the one that matters: a rule without a fail fixture is a rule nobody has proved.

---

## Appendix B — What this pack does not cover

One item remains, and it is deliberately not a document.

| Item | Why it matters | Wave | Owner |
|---|---|---|---|
| **Interface-catalogue inventory** | The three wire contracts are specified (§2.5, §2.6); the inventory of HTTP operations, topics, manifests and their consumers is not. It is **generated** by `dqctl impact` rather than hand-maintained, because a manually curated inventory of operations, topics and consumers would recreate the second source of truth that invariant S1 exists to prevent | M2 | Solution architect |

Everything else this pack originally left open is now closed, and closed in the right place: operational content in the [Data Plane Operations Guide](data-plane-operations-guide.md), requirements evidence in [Appendix C](#appendix-c--requirements-traceability-matrix), build content here. The split follows the same rule as the documents themselves — a procedure belongs with the people who execute it.

---

## Appendix C — Requirements traceability matrix

The evidence the gates ask for, in one place: every requirement, the design element that satisfies it, where it is specified, and the test that proves it. Maintained at each gate; a row with no test is a requirement nobody has met.

### C.1 Business requirements

| ID | Requirement | Design element | Specified in | Proven by | Wave |
|---|---|---|---|---|---|
| **R-01** | Minimal and non-intrusive | C6 in-process; C7a WASM; only new surfaces are an OCI artefact and a CRD | Architecture §1, KD-1 | T3 + an enforced service with zero application change (G2) | M3 |
| **R-02** | HTTP contracts come from OpenAPI | L1 `$ref`; L2 lint | Architecture §7.2 | T0 | M1 |
| **R-03** | `/{intent}-{businessObject}` addressing | Global intent registry; `x-dq-intent`/`x-dq-object`; `dq.intent` headers | Architecture §1; this pack §2.0 | T0, T3 | M1 |
| **R-04** | Objects and rules centrally managed | C1, C2; invariants S1–S5 | Architecture §7.1 | T0 | M1 |
| **R-05** | Contract and rules must align | L1–L5; Fig 9 gate | Architecture §7.2 | T0 — a deliberately loosened contract fails CI (G1) | M1 |
| **R-06** | 2026-relevant technology | JSON Schema 2020-12, CEL, OCI + Sigstore, WASM/`ext_proc` | Architecture exec summary, KD-1–3 | T2; engine/Envoy compatibility matrix | M0–M3 |
| **R-07** | Syntax **and** semantic validation | Tiers 0 / 1 / 2 | Architecture, validation model | T1 per-tier fixtures | M2 |
| **R-08** | Rules composable by business intent | Composition model; narrow-only default | Architecture, composition model | T0 conflict detection, T1 | M2 |
| **R-09** | Rules common across HTTP, Kafka, file | One engine C6; violation contract with three renderings | Architecture §1 and violation contract | **T4 determinism** — identical verdicts across channels | M3–M5 |
| **R-10** | Sidecar is an option | C7b `ext_proc`; KD-1 option B kept as fallback | Architecture §2.4, KD-1 | T3 | M3 |
| **R-11** | Single source of truth | S1–S7; the artefact graph | Architecture §7 | T0 | M1 |
| **R-12** | Every decision traceable to rules | S6; `packDigest` in every violation | Architecture §7.1 | T1 | M2 |
| **R-13** | Publishing is not adopting | S7; §7.8; Operating Model §1 | Architecture §7.8 | T3 adoption verification | M3 |
| **R-14** | Coverage measurable apart from availability | Coverage SLI; `SKIPPED` never counted as pass | Architecture §5.1 | T5 fault injection | M2 |
| **R-15** | Safe degradation | Degrade ladder; quality circuit breaker | Architecture §5 | T5 game day | M3, M7 |
| **R-16** | No new single point of failure | In-process engine; declared `fallbackDigest` | Architecture §5 | T5 pod-kill | M3 |
| **R-17** | Quality model with scorecards | Six dimensions with thresholds | Architecture §7.6 | T6 scorecard review | M6 |
| **R-18** | Per-rule suppression and break-glass | D-010; CRD `suppress`; Operating Model §6, §9 | This pack §2.4; Operating Model | T3 (CRD admission rejects a bad suppression) | M6 |
| **R-19** | Retention and evidence | Schedule; bundles never purged | Ops Guide §6 | Test export from the bundle alone | M3 |
| **R-20** | Cutover and decommission | Stages S0–S4 | Ops Guide §8 | T6 + S2 dual-run evidence | M3–M6 |
| **R-21** | Support model | L1/L2/L3, escalation, hypercare | Ops Guide §7 | Handover checklist, item 7 | M5 |
| **R-22** | Capacity model | Derivation formulas | Ops Guide §5 | T5 performance test | M2 |

---
### C.2 Non-functional requirements

| ID | NFR | Target | Design response | Test and load profile | Wave |
|---|---|---|---|---|---|
| **N-01** | Added latency, HTTP | ≤ 1 ms p99 | In-process WASM (C7a), no hop | 500 rps, p50 4 KB / p99 64 KB, 20-rule plan; p99 delta ≤ 1 ms **and** p99.9 ≤ 2 ms | M0, M2 |
| **N-02** | Added latency, Kafka | ≤ 150 µs per record | Tiers 0–1 at the gate; tier 2 windowed | 5,000 records/s, single partition, 2 KB records | M2 |
| **N-03** | File throughput | ≥ 10,000 records/s | Streaming reader, constant memory (C9) | 1M-record JSONL, 40 columns; memory flat ±10% | M5 |
| **N-04** | Availability | ≥ 99.95% | No runtime dependency in the request path | Pod-kill during load; error rate unchanged | M3 |
| **N-05** | Evaluation coverage | ≥ 99.9% of eligible requests | `SKIPPED` published, never counted as pass | Fault injection: provider outage, cost limit, oversized payload | M2 |
| **N-06** | No unverified acceptance | Zero records | Atomic `_SUCCESS` marker | SIGKILL at 60% of a 500k-record file; downstream not triggered | M5 |
| **N-07** | Payload exposure | None beyond the owning pod | Per-service placement (§2.3) | Penetration test plus log review | M7 |
| **N-08** | Rule integrity | No unsigned bundle executes | cosign verification and digest pinning | Deploy an unsigned bundle; admission or start-up refuses | M7 |
| **N-09** | Maintainability | One engine, one rule language | C6 is the sole owner of semantics | Code review gate; no second evaluator exists | Continuous |
| **N-10** | Operability | Revert enforcement without a deployment | Binding `mode` field | Game day: each degrade step and back | M7 |
| **N-11** | Portability | Any Envoy-lineage proxy | WASM, no vendor API | M3 staging on the target proxy version | M3 |
| **N-12** | Cost | No licences; under 1% CI increase | Open-source components only | Finance review at G2 | M2 |

### C.3 How this matrix is maintained

| Rule | Reason |
|---|---|
| Updated **at each gate**, not continuously | A matrix edited weekly becomes stale; one edited at gates is reviewed |
| A requirement with no test escalates **at the gate** | Otherwise it is discovered during acceptance testing, when it is expensive |
| R-01 to R-10 trace to the original business brief; R-11 onward are derived | A reviewer must be able to tell contractual requirements from architectural ones |
| Adding or changing a requirement needs a decision-log entry | The matrix records commitments, so it is not editable at will |
| Deleting a requirement requires the gate owner's signature | Requirements do not quietly disappear when they become inconvenient |

**The two rows most likely to be challenged at G2** are N-01 and N-09. N-01 because it is an estimate until M0 measures it, and N-09 because "one engine" is an assertion that only a code review can verify. Both are stated with their evidence method precisely so the challenge lands on evidence rather than opinion.










