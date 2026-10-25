# Schema registry

Every format in this programme, as machine-readable JSON Schema 2020-12. These are the
files `dqctl`, the CI gates, the validator and the authoring skills load — the pack's §2
tables are the human narrative of the same contracts.

| Schema | Validates | Pack ref |
|---|---|---|
| `object-contract.profile.schema.yaml` | A canonical business-object contract (schema-for-schemas) | Architecture §7.1, App C.1 |
| `object-metadata.schema.yaml` | `*.meta.yaml` | §2.1 |
| `value-set.schema.yaml` | `catalog/value-sets/*.yaml` | Architecture §7.1 (S2) |
| `intent-registry.schema.yaml` | `catalog/intents.yaml` | §2.0 |
| `quality-pack.schema.yaml` | `rules/object/*.pack.yaml` | §2.2 |
| `intent-pack.schema.yaml` | `rules/intent/*.pack.yaml` | §2.3 |
| `quality-binding.schema.yaml` | `bindings/*.yaml` (file projection) | §2.4 |
| `file-manifest.schema.yaml` | `<intent>-<object>-<yyyyMMdd>.manifest.json` | §2.5 |
| `violation.schema.yaml` | The violation object and its RFC 9457 envelope | §2.6 |
| `bundle-manifest.schema.yaml` | `bundle.yaml` inside the artefact | §4.2, `bundleFormat: 2` |
| `plan.schema.yaml` | `plans/*.plan.json` (internal, `planFormat: 1`) | App C.4 |

## Conventions these schemas encode

| Convention | Rule | Why |
|---|---|---|
| **Object names** | `^[a-z][a-z0-9]*$` — **no hyphens** | The address key splits on the first hyphen, so `party-relationship` would be ambiguous. Objects are `partyrelationship`, `contactpoint` |
| **Intent names** | `^[a-z][a-z0-9]*$` — no hyphens, same reason | `onboard`, `change` |
| **Intent codes** | Three uppercase letters, from the intent registry (`ONB`, `CHG`) | Lets intent rules use `SEM-ONB-001` and satisfy `^(SYN\|SEM\|STA)-[A-Z]{3}-[0-9]{3}$` **without widening the pattern** |
| **Object codes** | Three uppercase letters in rule IDs (`CUS`, `PTY`) | Conventionally the first three letters; see open items |
| **Durations** | ISO 8601: `P7Y`, `P90D` | `M` means months; minutes are not permitted |
| **Timestamps** | RFC 3339, second precision, `Z` | `format` is annotation-only in 2020-12, so patterns are used where rejection matters |
| **Digests** | `sha256:` + 64 lowercase hex | OCI convention; never a tag |
| **Correlation IDs** | 26-character Crockford base32 (ULID) | Survives a batch job with no trace context |

## Authoring and publication

Authored as **YAML** for reviewability; CI emits JSON twins to `dist/schemas/` for tooling
that requires JSON. YAML **1.2 only** — 1.1 parses `on`, `yes` and `no` as booleans, which
has already caused one defect in this repository.

## Gates

```bash
ruby catalog/schemas/check.rb     # every schema parses, declares a dialect, and every $ref resolves
```

Each schema also carries a valid and an invalid fixture once the sample objects exist;
the invalid fixture must fail for the *intended* reason, not incidentally.

## Open items

| # | Item | Consequence today |
|---|---|---|
| 1 | **Object rule-ID code is not declared.** `meta.yaml` is closed to unknown keys, so the three-letter object code exists only as a convention | `dqctl lint` cannot verify that `SEM-PTY-001` belongs to `party` |
| 2 | **`$id` carries no version segment** (it matches the pack's printed form) | A breaking schema change needs a new file name or a path change, not just a SemVer bump; CHANGELOG is the record |
| 3 | **Stricter than the pack in three places** — `additionalProperties: false` on pack `spec`, on each rule, and on binding `spec` | Deliberate: the pack says its fragments are "abbreviated", and a typo in a rule key is a rule that silently does not run |
| 4 | **Two binding constraints cannot be schema-expressed** — the 30-day suppression window (needs a clock) and rule-not-in-plan (needs the compiled bundle) | Enforced by `dqctl lint`, stated in the schema description |
| 5 | **Combinators are forbidden in object contracts** | `oneOf` for individual/organisation is expressed as a tier-1 rule, which yields a better violation message |
