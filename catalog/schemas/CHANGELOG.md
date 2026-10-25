# Schema changelog

Grouped by schema, newest first. A schema change that would reject a document which
previously validated is **breaking** and needs a new file name or path, because `$id`
carries no version segment.

## 2026-10-25 · 1.3.0 — `bundle-manifest` describes what it was carrying

Found by writing the consumer-side check (`dqctl verify --contents`), not by reading the
manifest. Reading a published bundle back and comparing its manifest against the files it
carried reported **7 of 18 files undescribed**: six value sets and `catalog/intents.yaml`.
`bundleFormat: 1` had no field for either.

Nothing was unverified — the layer digest a binding pins covers every byte — but the
per-artefact claim that existed for contracts, packs and plans did not exist for the
enumerations the rules are written against. A consumer could see that *some* file had the
digest of `country-code-iso3166-1.yaml` and could not see a claim that it was the one that
had been reviewed, which is the claim the manifest exists to make.

`bundleFormat` moves to **2** rather than staying at 1 with an optional field, because a
reader that does not understand `valueSets` must refuse the bundle rather than silently
ignore its provenance. `valueSets[].path` is carried as well as the digest, so the claim is
checkable by location and by hash: a digest alone cannot tell a renamed file from the file
the author published.

## 2026-10-24 · 1.2.0 — `object-contract.profile` `$ref` pattern repaired

Found the first time a real 2020-12 validator was pointed at the registry.

```yaml
# 1.1.0 — only the bare prefix matched, because the first alternative ends at the slash
'^(https://registry\.internal/dq/(schemas|valuesets)/|#/\$defs/[a-z][a-zA-Z0-9]*)$'

# 1.2.0 — the tail after the prefix is consumed
'^(https://registry\.internal/dq/(schemas|valuesets)/[a-z][a-zA-Z0-9/._-]*|#/\$defs/[a-z][a-zA-Z0-9]*)$'
```

Every canonical contract in the sample was therefore **invalid** against the schema it
claims to conform to, while the sample's own Ruby checker passed it — that checker tested
`strings.HasPrefix`, which is a weaker claim than the pattern makes. Nineteen hand-written
probes could not catch this; ten lines of `jsonschema.Validate` did.

The lesson is recorded rather than the fix alone: a hand-written check of a schema is not a
substitute for a schema validator, because the two implement different predicates.

## 2026-10-24 · 1.1.0 — `object-contract.profile` admits nested shapes

Found while authoring the party-model sample: a contract could declare `$defs` but
`$ref` accepted only catalogue URIs, and a `type: object` field had no way to declare
its own properties. Every nested structure in a real payload was therefore
inexpressible, which would have forced the sample to flatten itself to fit the schema —
the wrong direction.

| Change | Effect |
|---|---|
| `field.properties` added | An object-typed field may carry its own properties |
| `field.required` added | …and its own required list |
| `field.additionalProperties` added | …and declare itself open or closed |
| `$ref` pattern widened | `#/$defs/<name>` now accepted alongside catalogue URIs |
| `propertyNames` on both `properties` maps | Field names are camelCase, so `required` entries can be checked against them |

The vocabulary is still bounded and still combinator-free; this adds no new expressive
power beyond nesting, and no change to any document that validated under 1.0.0.

## 2026-10-24 · 1.1.0 — `intent-pack` accepts `spec.tests`

Intent rules had no way to carry a fixture, so none were ever executed. That is not a
theoretical gap: the first time an intent rule ran for real, `SEM-ONB-002` was wrong — it read
`contactPoints[].type`, but a contact point carries `contacts[].type`. A rule that cannot be
tested is a rule that will be wrong.

`spec.tests` now takes the same fixture shape as an object pack; the input is the composite
request, which is still a mapping. Rules that read the clock cannot have a deterministic
fixture and are expected to omit one.

## 2026-10-24 · 1.0.0 — initial set

| Schema | Version | Note |
|---|---|---|
| `object-contract.profile` | 1.0.0 | New. Bounded keyword vocabulary; combinators excluded by design |
| `object-metadata` | 1.0.0 | Matches Delivery Design Pack §2.1; adds `dependencies` for timeliness |
| `value-set` | 1.0.0 | New. Previously referenced by `$id` but never specified |
| `intent-registry` | 1.0.0 | New. Adds the three-letter `code` that intent rule IDs use |
| `quality-pack` | 1.0.0 | Matches §2.2; closed `spec` and rule objects |
| `intent-pack` | 1.0.0 | Matches §2.3; adds `metadata.alias` and `profile.idempotency` |
| `quality-binding` | 1.0.0 | File projection of §2.4; `ENFORCE ⇒ fallbackDigest` as if/then rather than CEL |
| `file-manifest` | 1.0.0 | New. §2.5 was an example with no schema |
| `violation` | 1.0.0 | §2.6 as schema, plus the RFC 9457 envelope |
| `bundle-manifest` | 1.0.0 | New. `bundleFormat: 1` had a version and no format |
| `plan` | 1.0.0 | New. Internal `planFormat: 1` |

### Known deviations from the printed pack

Recorded rather than silently corrected, so the two can be reconciled deliberately:

1. `additionalProperties: false` added to pack `spec`, rule objects and binding `spec` (pack §2 says "abbreviated").
2. Object and intent names remain hyphen-free, so `/change-contactpoint` — while the pack's path pattern would accept `/change-contact-point`. This is a live inconsistency in the pack, not in these schemas.
3. Intent rule IDs resolve through the intent registry's `code`, with no change to the rule-ID pattern.
