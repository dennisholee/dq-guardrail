# Steward quickstart — writing and testing a validation rule

| | |
|---|---|
| **Audience** | Domain stewards |
| **Time** | 1–2 hours for a first rule |
| **Normative for** | The authoring workflow only |
| **Cites** | [Solution Architecture](architecture/solution-architecture.md) · [Control Plane Operating Model](architecture/control-plane-operating-model.md) |

## The five-minute mental model

- **You own what "valid" means** for your object — the schema, and the rules that follow from it.
- **You never deploy.** You open a pull request; CI proves it; a platform engineer decides when it goes live.
- **Publish is not adopt.** Your change is harmless until a workload's binding adopts it, and that is per workload. "Merged but not live yet" is normal.

## Steps

1. **Find your object.** `catalog/business-objects/customer.v1.json` holds the schema; `customer.meta.yaml` holds owner, thresholds and the allowed-intent list.
2. **Choose the tier.** Structural concerns belong in the schema — the syntax tier is derived from it automatically, so never hand-write a tier-0 rule that duplicates a schema constraint. Business meaning goes in tier 1 as CEL.
3. **Write the rule and its fixtures** in `rules/object/customer.v1.pack.yaml`, or in `rules/intent/<intent>.pack.yaml` if it only applies to one intent.

```yaml
  semantic:
    - id: SEM-CUS-005
      when: "_.kycStatus == 'VERIFIED'"
      assert: "has(_.verifiedOn) && timestamp(_['verifiedOn']) <= clock.now()"
      severity: MAJOR
      dimension: accuracy

  tests:
    - { rule: SEM-CUS-005, expect: pass, input: { kycStatus: VERIFIED, verifiedOn: '2026-01-04T00:00:00Z' } }
    - { rule: SEM-CUS-005, expect: fail, input: { kycStatus: VERIFIED } }
```

4. **Test it locally.** `dqctl test` runs the fixtures and prints an explanation for each verdict. No cluster required.
5. **Check the blast radius.** `dqctl impact customer` lists every API operation, topic and batch manifest that writes this object, so you know who will see the change.
6. **Open the pull request.** CI runs, in order: lint, align, test, `generate --check`, bundle, sign.
7. **Understand what happens next.** Merge → *published*. A platform engineer adopts it on one workload in `SHADOW` for one review cycle → `WARN` → `ENFORCE`. You will see the shadow evidence at the DQ Council.

## Rules of thumb

- **Every rule needs a pass and a fail fixture.** Without both, the conformance gate has a hole and nobody will notice.
- **One precise rule beats three vague ones.** Forty rules per object is the working budget.
- **If you cannot state it in one sentence** with a subject and a predicate, it is probably two rules.
- **Never interpolate `x-dq-sensitive` fields** into a message; the compiler will reject it, but do not plan around it.
- **Object pack versus intent pack:** if the rule is only true for one intent, it belongs in the intent pack.

## When your rule causes false positives

Suppress it — see [Operating Model §6](architecture/control-plane-operating-model.md#6-per-rule-suppression) — then fix it, publish, adopt. Do **not** relax the object invariant to accommodate bad data; that converts a data problem into an invisible permanent exception.

## Where to look things up

| Need | Go to |
|---|---|
| A term you have not seen | [Glossary](architecture/solution-architecture.md#appendix-d--glossary) |
| How much ceremony a change needs | [Operating Model §5](architecture/control-plane-operating-model.md#5-change-classes-and-their-ceremony) |
| What "published" and "adopted" mean | [Architecture §7.8](architecture/solution-architecture.md#78-lifecycle-publish-adopt-retire) |
| Why the rule you want is not allowed | [Composition model](architecture/solution-architecture.md#composition-model-layering-by-intent) |
