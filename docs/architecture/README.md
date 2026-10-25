# Architecture documentation — start here

Three documents, deliberately separate. Use this table to find the one that answers your question; they must not restate one another.

| If your question is… | Read | Normative for | Owner | Status |
|---|---|---|---|---|
| What did we decide, why, and what does it cost? | [Solution Architecture](solution-architecture.md) | Decisions, invariants (S1–S7), NFRs, component boundaries | Domain architect | v1.2, frozen at gates |
| How are objects and rules run — lifecycle, propagation, suppression, break-glass? | [Control Plane Operating Model](control-plane-operating-model.md) | Procedures, states, timings, queries | Platform lead | v0.1, living |
| What do I code against — schemas, CLI, environments? | [Delivery Design Pack](delivery-design-pack.md) | Build contracts | Delivery lead | v0.1, living |
| What do I do when it breaks — metrics, alerts, runbooks? | [Data Plane Operations Guide](data-plane-operations-guide.md) | Operations, runbooks, retention, support | Platform lead | v0.1, living |
| How do I write and test a rule this afternoon? | [Steward quickstart](../steward-guide.md) | Authoring workflow | Domain architect | v0.1, living |
| How do I regenerate the diagrams? | [Appendix A](solution-architecture.md#appendix-a--regenerating-the-figures) and the `Makefile` | Diagram tooling | Platform | — |

## Rules of the set

These six rules are what keep a split documentation set from becoming two half-truths. They are the same discipline the architecture applies to rules.

1. **One-way dependency.** The operating model cites the architecture; the architecture never cites the operating model for a fact it should own. If the operating model needs a decision changed, that is a decision-log entry first (D-011).
2. **Identifiers are defined once.** `C1–C12`, `S1–S7`, `KD-1..3`, `D-001..D-011`, `M0–M7` are defined in the Solution Architecture and cited everywhere else.
3. **One glossary.** [Appendix D](solution-architecture.md#appendix-d--glossary) is the only copy of the vocabulary. Other documents link to it rather than restating definitions.
4. **No fact stated twice.** Where repetition is unavoidable, the second mention carries "see X for the normative statement".
5. **Normative-source header.** Every document opens by stating what it is normative for and what it merely cites.
6. **Anchors are verified.** `make links` resolves every cross-document reference; a renamed heading fails the build rather than silently rotting a link.

## Adding a further document

A topic earns its own document only if all three are true: **different audience**, **different update cadence**, **different owner**. If it fails any one, it belongs as a section of an existing document. The NFR register fails all three and correctly stays in the architecture; the lifecycle procedures pass all three and correctly left; the build contracts pass all three and became the Delivery Design Pack.

## Register

| Document | Version | Last reviewed | Next review |
|---|---|---|---|
| `solution-architecture.md` | 1.2 | Revision 4 | At gate G1 |
| `control-plane-operating-model.md` | 0.1 | Initial | Monthly at Platform Ops |
| `delivery-design-pack.md` | 0.1 | Initial | At each wave gate |
| `data-plane-operations-guide.md` | 0.1 | Initial | Weekly at Platform Ops |
| `../steward-guide.md` | 0.1 | Initial | Quarterly |

## Validation

```bash
make -C . figures validate links
```

`validate` XML-checks every rendered SVG; `links` resolves every internal and cross-document reference in this folder.
