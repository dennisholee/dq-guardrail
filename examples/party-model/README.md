# Party model — a worked sample

A customer data management domain in a bank, carried from business intent through to API
contract, with the quality rules that hold each step. Everything here validates against
the shared schema registry in `../../catalog/schemas/`; nothing here restates a format.

Run it, from the repository root:

```bash
go build -o bin/dqctl ./tools/dqctl
./bin/dqctl validate --root examples/party-model
./bin/dqctl lint     --root examples/party-model
./bin/dqctl align    --root examples/party-model
./bin/dqctl test     --root examples/party-model
./bin/dqctl generate --root examples/party-model --check
./bin/dqctl bundle   --root examples/party-model --check
```

`validate` parses every document, validates it against the formats in `catalog/schemas/`,
checks the chain from contract to verdict, and recomputes every digest in `evidence/`.
`lint` enforces invariant S2. `align` runs L1/L3/L4: the API must reference the canonical
contract rather than restate it, a request must not shadow a field the contract defines, and
every rule must name a field that exists. `bundle --check` regenerates the manifest from the
tree and fails when the committed one disagrees. All four exit 0 on this sample.

`bundle` without `--check` writes a gzipped tar of 18 entries — the manifest, three
contracts, six value sets, the intent registry, two plans and five packs — and prints two
digests: the archive's and the manifest's. Two builds of the same tree produce **identical
bytes**, which is what makes a binding pin meaningful: if every build produced a fresh
digest, every deployment would look like a change.

`generate` writes `spec/openapi/generated/x-dq-rules.yaml` — which rules run on each
endpoint, with the digest of the plan they were derived from — and `--check` fails when that
copy is stale. Two deliberate choices there: it is a sidecar rather than an `x-dq-rules`
block inside each operation, because the available YAML emitter collapses folded blocks and
reindents the whole contract file (a 121-line diff, repeated on every hand edit); and it pins
the plan digest rather than only the rule IDs, because an edited `assert` leaves the ID list
unchanged and a check comparing IDs alone would pass.

`publish` turns the archive into an **OCI image layout** — `oci-layout`, `index.json`, and
one blob each for the config, the manifest and the bundle — and prints the manifest digest a
binding pins:

```bash
./bin/dqctl bundle  --root examples/party-model --out dist/dq-bundle.tar.gz
./bin/dqctl publish --bundle dist/dq-bundle.tar.gz --layout dist/oci
#   layer    sha256:9470…   (identical to the archive digest)
#   manifest sha256:4849…   (what a binding pins)
```

**Pushing to a registry is not implemented.** Passing `--registry` still writes the complete
layout, prints the reference, and then exits 3 saying so — a publish step that reports success
without pushing would be worse than one that fails. The layout is the artefact a registry
would accept, and writing it is the half that can be verified offline.

`explain` decides one stored request and writes the verdict, which is how the four documents
in `evidence/verdicts/` are produced rather than drawn:

```bash
./bin/dqctl explain \
  --root examples/party-model \
  --input examples/party-model/evidence/scenarios/change-lagos-postcode.json \
  --out examples/party-model/evidence/verdicts/change-lagos-postcode.json
#   /change-contactpoint rejected: 1 violation(s), 5 rules ran
```

Each scenario carries the intent, the object, the correlation id and the payload, so a
regeneration needs no other arguments. `go test ./engine/` replays every scenario and fails if
the committed verdict differs — the verdicts are engine output, not illustration.
`change-within-cooling-off` is skipped by that test because `SEM-CHG-003` reads the clock: its
verdict is real, but not reproducible on an arbitrary future date.

`gate` runs the third channel over a delivery. It verifies the checksum **before reading a
record**, then streams every row:

```bash
./bin/dqctl gate \
  --root examples/party-model \
  --manifest examples/party-model/evidence/deliveries/onboard-party-20261020.manifest.json \
  --input    examples/party-model/evidence/deliveries/onboard-party-20261020.jsonl \
  --out dist/gate
#   3 records — 2 accepted, 1 rejected
```

`accepted.jsonl` holds the rows that passed, `rejects.csv` names the row, rule and message for
those that did not, and `_SUCCESS` is written **only** when every record passed and the count
matches the manifest — a downstream job waits on that marker, so a partial accept must never
produce one.

## The three channel renderings

All three adapters are implemented and tested against the renderings in
`solution-architecture.md` §6.4 — one violation object, three behaviours:

| Channel | On a violation | Where it lives |
|---|---|---|
| HTTP | `422 application/problem+json`, `DQ-Pack-Digest` response header; **shadow** and **warn** pass through (warn annotates) | `engine/adapter/http` |
| Kafka | record routed to `<topic>.dlq` with `dq.violation.count`, `dq.violations` (base64 JSON, capped at 8 KB) and `dq.pack-digest`; **warn** sets `dq.warn=1` and forwards | `engine/adapter/kafka` |
| File | `rejects.csv` (`row, ruleId, severity, path, message`) and no `_SUCCESS` | `engine/adapter/file` |

`DQ-Pack-Digest` is set on a **successful** write too. Knowing which rules let a write through
is what makes "why was this accepted?" answerable three months later.

## The supply chain

| # | Step | Where | What it fixes |
|---|---|---|---|
| S1 | Business intent | `catalog/intents.yaml` | The three-letter code intent rules are named after |
| S2 | Governed vocabulary | `catalog/value-sets/` | Enumerations referenced by `$id`, never restated |
| S3 | Canonical object | `catalog/business-objects/*.json` | Structure, field names, patterns — tier 0 derives from this |
| S4 | Object stewardship | `catalog/business-objects/*.meta.yaml` | Owner, sensitivity, retention, thresholds, permitted intents |
| S5 | Object rules | `rules/object/*.pack.yaml` | Semantics JSON Schema cannot express, with fixtures |
| S6 | Intent rules | `rules/intent/*.pack.yaml` | Obligations of the intent; can add, never subtract |
| S7 | API contract | `spec/openapi/party-api.yaml` | Endpoints that reach these objects, by `$ref` only |
| S8 | Enforcement | `bindings/party-service.yaml` | Pins one bundle by digest, sets the mode, names the channels, carries one suppression |
| S8 | Provenance | `evidence/` | Bundle manifest, 2 compiled plans, 4 verdicts, readiness scorecard, CODEOWNERS |

## Business label to technical name

The address key is `<intent>-<object>` and splits on the first hyphen, so neither part
may contain one. That is why the objects read `partyrelationship` and `contactpoint`.

| Business label | Endpoint | Intent (code) | Primary object |
|---|---|---|---|
| Onboard customer | `POST /onboard-party` | `onboard` (`ONB`) | `party` |
| Change address | `POST /change-contactpoint` | `change` (`CHG`) | `contactpoint` |

`/change-contactpoint` is the visible cost of the hyphen rule. It is legal, it is ugly,
and it is recorded here so that the next person relaxes the pattern deliberately rather
than by accident.

`/onboard-party` is a **composite**: `party`, `relationship` and `contactPoints[]` are
each a canonical object, and the envelope around them is a contract-level request shape.
The envelope is deliberately *not* in the catalog, because the catalog holds business
objects, not request bodies. `party` is named as the primary object because that is the
object the intent is about; `partyrelationship` and `contactpoint` are governed by the
same intent but are not the address.

## What the rules actually are

| Rule | Statement | Why it is not structural |
|---|---|---|
| `SYN-PTY-001` | One name component per type | JSON Schema 2020-12 has no unique-by-key for arrays |
| `SEM-PTY-001` | Exactly one of individual or organisation | Combinators are banned in contracts; a rule message names the branch |
| `SEM-PTY-002` | Individual needs a FIRST and a LAST | Cross-array-membership |
| `SEM-PTY-003` | Organisation needs a registration number | Conditional on a sibling branch; waivable by relax |
| `SEM-PTY-004` | Name must match the registration record | Accuracy is external evidence, not a self-check |
| `STA-PTY-001` | `partyId` never changes | Needs the prior version |
| `SEM-PTR-001` | CUSTOMER needs a line of business | Conditional required-ness |
| `SEM-PTR-002` | CONNECTED_PARTY must not state one | The mirror of the above; the pair partitions the domain |
| `SEM-PTR-003` | The party reference must resolve | Referential integrity across objects |
| `SEM-PTR-004` | One CUSTOMER relationship per party | Duplicate CIF detection |
| `STA-PTR-001` | CUSTOMER is never demoted | Needs history |
| `SYN-CPT-001` | One contact point per type | Unique-by-key |
| `SEM-CPT-001` | Postcode grammar follows the country | A pattern cannot depend on another field |
| `SEM-CPT-002` | US and CA need an administrative area | Conditional on country |
| `SEM-CPT-003` | City must match the postal file's locality | Accuracy against a reference file |
| `SEM-CPT-004` | Same postcode, same city across types | Internal contradiction within one record |
| `SEM-ONB-001` | Onboarding establishes a CUSTOMER relationship | Intent-level |
| `SEM-ONB-002` | Onboarding needs a CORRESPONDENCE address | Intent-level |
| `SEM-ONB-003` | No placeholder name components | Intent-level, and `UNKNOWN` is not a name |
| `SEM-ONB-004` | Organisation name must be new | Needs `refdata` |
| `SEM-CHG-001` | Country plus street together is a relocation | Cross-field, and a policy decision |
| `SEM-CHG-002` | No backdating beyond 30 days | Needs a clock |
| `SEM-CHG-003` | No second change within 90 days | Fraud control; needs the prior change |

Every object rule carries a passing **and** a failing fixture, and the validator fails
the build if either is missing. A rule with no failing fixture is a rule nobody has
proven can fail.

## The three deliberate failures

These are in the fixtures on purpose, and each one is a defect a real bank actually
produces:

| Fixture | Defect | Caught by |
|---|---|---|
| `party.pack.yaml` | An LLP with no registration number | `SEM-PTY-003` |
| `party.pack.yaml` | An individual captured with a surname only | `SEM-PTY-002` |
| `contactpoint.pack.yaml` | A London postcode on a Lagos address | `SEM-CPT-001` |

## What `dqctl validate` proves, and what it does not

`dqctl` checks six things: every document parses; every document is validated against its
format in `catalog/schemas/` by a real 2020-12 validator; every `$ref`, `$id` and value-set
URI resolves; the chain holds end to end — pack → object → intent → address key → endpoint,
with rule and fixture coverage and no orphan rules; every digest in the evidence is
recomputed; and each plan agrees field-by-field with the packs it was compiled from.

It **does** evaluate rules against payloads now: `dqctl test` compiles each rule's guard and
assertion with cel-go and runs every fixture. It found two rules that could never fail —
`size(xs) == size(xs.map(…))` is true even with duplicates, so `SYN-PTY-001` and
`SYN-CPT-001` were passing everything. Both were rewritten with `filter(...).size() > 1` and
now fail correctly.

What it still does not do: evaluate intent-level rules, because those need a clock. The
evaluator deliberately has no wall clock, so a rule that reads `now()` fails to compile and
is reported — a fixture run must be reproducible. `SEM-CHG-002` and `SEM-CHG-003` are
therefore runtime-only, and that boundary is tested in
`TestEvalRejectsTimeDependentRules`.

Defect classes are checked by `go test ./...`, in four groups:

*Structure* — a dangling `$ref`; a version drift between pack and contract; a removed
failing fixture; an illegal `oneOf`; a keyword outside the vocabulary; a mismatched intent
code; an `x-dq-intent` that disagrees with its path; an unquoted date; a wildcard rule path;
a fixture missing its provider data; a registry that cannot be reached; and a contract
`pattern` that is unanchored or nests a quantifier inside another (`(a+)+`), because a pattern
runs against every payload and a catastrophic one does not return a wrong answer — it returns
none, at whatever rate traffic arrives.

*Alignment* — a component that copies an object instead of `$ref`-ing it; a request body
that stops referencing the contract; a wrapper that redeclares a contract field; and a rule
that names a field the contract does not declare.

*Provenance* — a tampered pack digest; a plan whose `assert` or tier drifted from its pack;
a contract edited after the build; a binding repinned to the wrong digest; a verdict citing
the wrong pack; a suppression running past 30 days; a suppression naming a rule that does
not exist; and a plan the bundle does not list.

*Redaction* — a value echoed into a violation. A message names the path and the constraint,
never the value, so a document number that fails its pattern is reported at `$.partyId` as
`fails the pattern constraint` and nowhere else.

*Runtime* — a record that fails the contract (rejected by tier 0 with the reserved `000` rule
id); a record that breaks an authored rule; a valid record that must still report that rules
ran; a waived rule that is recorded rather than enforced; a rule that cannot be evaluated
against a record that is structurally valid (it fails closed, naming the rule, rather than
becoming a 500); a record that exceeds the gate's size limit (refused with `413` in enforce
mode, passed through byte-for-byte in shadow and warn); and the HTTP adapter's four behaviours
(enforce rejects, shadow and warn pass through, warn annotates, unparseable bodies and unrouted
paths are left to the service).

Five of those were **not** injected. The wildcard `path` check found three real violations
that had been in the rules since they were written. `L3` found that the change-address
request restated `partyId` and the whole address shape instead of referencing
`contactpoint.json` — two definitions of one field, which is the drift this repository
exists to prevent; the request now composes from `#/properties/partyId` and `#/$defs/contact`.
And the first run of a real validator found that `object-contract.profile.schema.yaml`
rejected **every** value-set `$ref` in **every** contract: the pattern's first alternative
ended at the slash, so it matched only the bare prefix. The earlier hand-written checker
passed that same input, because it tested `HasPrefix` — a weaker claim than the pattern
makes. That is the whole argument for using a validator instead of writing one.

The redaction check was written as an invariant, not a fault injection — fill every leaf with
a marker, decide, and fail if the marker appears in the verdict — and it failed on its first
run. The JSON Schema validator's error text embeds the offending value (`'MARKER7f3a9c' does
not match pattern …`), so **every tier-0 violation was quoting the field it rejected**, and a
document number would have travelled into problem details, logs and the reject file. The
message is now built from the failing keyword instead of the validator's prose. Nothing had
noticed, because every fixture that reached tier 0 was checking structure with valid values.

The concurrency check was written the same way — eight goroutines, one engine, `-race` — and it
did not report a race. It killed the process: `fatal error: concurrent map writes`. The CEL
program cache was a plain map, written lazily from the request path, and the HTTP adapter serves
requests concurrently by design. Every sequential test in this repository passed against it,
because a race needs two requests at once to exist. That is why the CI workflow now runs
`go test -race ./...` as its own step, and why the engine compiles every expression at
construction rather than on the first request that reaches it.

## What the sample changed in the registry

Authoring the nested `individual` / `organisation` branches exposed a real defect in
`object-contract.profile.schema.yaml`: it would accept `$defs` but `$ref` only accepted
catalogue URIs, and an object-typed field had no way to declare its own properties. Every
nested structure was inexpressible, which would have forced the sample to flatten itself
to fit the schema. That is the wrong direction, so the profile went to **1.1.0** —
`properties`, `required` and `additionalProperties` on a field, plus internal
`#/$defs/<name>` references. See `../../catalog/schemas/CHANGELOG.md`.

This is the value of building the sample after the schemas rather than before: the sample
tested the schemas instead of silently conforming to their mistakes.

## Judgement calls, recorded

| Decision | Reasoning |
|---|---|
| **No `partyType` field** | It would be derivable from which branch is present, and redundant state drifts. Which branch is present *is* the type, and `SEM-PTY-001` makes "exactly one" explicit. |
| **Address is componentised, not a free-text block** | ISO 19160-4 components (`street1`, `city`, `administrativeArea`, `postalCode`, `country`). A free-text line cannot be validated against a country's postcode grammar, which is exactly what `SEM-CPT-001` needs. |
| **Value sets are referenced by virtual URI** | `https://registry.internal/dq/valuesets/<name>` is not a retrievable JSON Schema; `dqctl` inlines the enum at expansion. This keeps invariant S2 — never restate an enumeration — while staying inside the profile's bounded vocabulary. |
| **Postal code is not pattern-constrained in the contract** | Any pattern would have to know the country. Grammar lives in `SEM-CPT-001`, where it can branch. |
| **No dates on `partyrelationship`** | Not asked for, and adding `effectiveFrom` without also modelling closure invites an open-ended relationship that is never closed. Better absent than half-modelled. |
| **`relax` used once, on `SEM-PTY-003`** | To show the escape hatch is real and expensive: two named approvers, a written justification, an expiry date. Not to demonstrate convenience. |
| **`now()` in `SEM-CHG-002`/`003`** | CEL's `now()` builtin; requires cel-go ≥ 0.13. Noted because a runtime that lacks it would fail these rules open, not closed. |

## What the evidence chain proves

`evidence/bundle.yaml` is the audit entry point, and every digest in it is a real sha256 of
a real file in this sample — recomputed on every validation run, not copied by hand.

| Claim | Verified against |
|---|---|
| 3 object `contractDigest`s | the contract files |
| 5 pack `digest`s and `ruleCount`s | the pack files |
| 2 plan `digest`s | the plan files |
| each plan's `schemaMatcher.digest` | the contract it validates |
| the binding's pinned `bundle` digest | `evidence/bundle.yaml` |
| 4 verdict `packDigest`s | the pack owning each rule |
| 3 of 4 verdicts, field by field | re-deciding their stored scenario with the engine |

### What `dqctl verify` found, and what it changed

Verifying a published layout (`dqctl verify --layout dist/oci --contents`) reads the archive back
and checks the manifest inside it against the files it carries. On its first run it failed:
**7 of the bundle's 18 files were carried but undescribed** — six value sets and
`catalog/intents.yaml`. `bundleFormat: 1` had no field for either, so a consumer could see that
*some* file had the digest of `country-code-iso3166-1.yaml` and could not see a claim that it was
the one that had been reviewed.

That is now `bundleFormat: 2` (`bundle-manifest.schema.yaml` 1.3.0): every value set is described
by name, version, path **and** digest, and so is the intent registry. The path is carried as well
as the hash because a digest alone cannot tell a renamed file from the published one. The format
number moved rather than gaining an optional field, so a reader that does not understand the new
section refuses the bundle instead of quietly ignoring its provenance. `dqctl verify` now reports
`undescribed 0`, and a test asserts it stays that way.

Two further defects surfaced while making that change, both invisible until the manifest was
generated rather than hand-written:

- `BuildManifest` emitted `justification: ""` inside every `relax` entry — a field
  `bundle-manifest.schema.yaml` forbids, because the rationale belongs in the pack. The hand-
  authored manifest had simply never included it, so `dqctl bundle` was producing a manifest its
  own schema would reject. Fixed by giving the manifest its own `BundleRelax` type, which has
  nowhere to put the field.
- `CompareManifests` compared objects, packs, plans, providers and relax entries, and nothing
  else — so a stale value-set digest would have passed `bundle --check`. Now compared, with a
  test that corrupts one and expects the failure.

`evidence/bundle.yaml` is now generated (`dqctl bundle --manifest`), not hand-written. Its
`builtFrom.dqctl` reads `0.1.0` rather than the `0.9.0` a human had typed; the tool's version is
the honest record of which tool built it.



Two consequences worth stating plainly:

**The bundle digest is a stand-in.** A real OCI manifest digest covers a config blob and
layer digests that this sample has no artefact for. Here it is the sha256 of
`evidence/bundle.yaml`, so the chain is locally closeable. The substitution is deliberate
and labelled, not hidden.

**`fallbackDigest` equals `bundle`.** On a first deployment the last known good bundle *is*
the current one. The schema requires a `fallbackDigest` in `ENFORCE` mode and this sample
satisfies it honestly rather than inventing a previous release.

The **readiness scorecard** (`evidence/scorecard.md`) is generated by `dqctl scorecard` and
guarded by `--check`, because the hand-written version went stale every time a rule changed.
It carries the counts and the declared-versus-enforced table; what it deliberately does not
claim is that any threshold is met, that provider shapes are modelled, or that a runtime
measurement exists. Reproduce it with `dqctl scorecard --root examples/party-model`.

