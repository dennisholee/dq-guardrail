# dq-guardrail

Minimal, non-intrusive data-quality controls for HTTP APIs, Kafka and file
processing on Kubernetes. One canonical business-object contract and one
versioned rule pack produce one signed bundle, consumed by interchangeable
adapters (Envoy WASM, Kafka stream gate, file gate job, library).

### Documentation set

| Document | Answers | Normative for |
|---|---|---|
| [`docs/architecture/solution-architecture.md`](docs/architecture/solution-architecture.md) | What did we decide, why, what does it cost? | Decisions, invariants, NFRs |
| [`docs/architecture/control-plane-operating-model.md`](docs/architecture/control-plane-operating-model.md) | How are objects and rules managed and disseminated? | Lifecycle, propagation, suppression, break-glass |
| [`docs/architecture/delivery-design-pack.md`](docs/architecture/delivery-design-pack.md) | What does a squad code against? | Artefact schemas, CLI contract, environments, test strategy |
| [`docs/architecture/data-plane-operations-guide.md`](docs/architecture/data-plane-operations-guide.md) | What do I do when it breaks? | Metrics, alerts, runbooks, retention, support, capacity |
| [`docs/steward-guide.md`](docs/steward-guide.md) | How do I write and test a rule? | Authoring workflow |
| [`docs/architecture/README.md`](docs/architecture/README.md) | Which document governs what? | The documentation register |

Diagrams are code: `docs/architecture/puml/` compiles to `docs/architecture/figures/`.

```bash
make -C docs/architecture check    # render SVG, XML-validate, resolve all cross-document links
```

- **CI alignment gate:** [`ci/spectral.yaml`](ci/spectral.yaml), [`ci/dqctl-align.yml`](ci/dqctl-align.yml) — 18 steps, green locally; the registry push and cosign signing are deferred until `dqctl publish` gains a registry client. What a consumer *can* do today is `dqctl verify`, which reads a layout back from disk and re-hashes every blob against the digest that names it

### Implementation

| Path | What | Status |
|---|---|---|
| [`engine/`](engine/) | Artefact model, schema registry, checks, S2 lint, L1/L3/L4 alignment, CEL evaluation, tier-0 validation, HTTP / Kafka / file adapters, deterministic bundling | Working (M1.1–M1.3, M2, M4 gate logic, M5 file) |
| [`tools/dqctl/`](tools/dqctl/) | `validate`, `lint`, `align`, `bundle`, `test`, `generate`, `publish`, `verify`, `explain`, `gate`, `crd`, `scorecard`, `version` | Working; only the registry push in `publish` is unimplemented |
| [`catalog/schemas/`](catalog/schemas/) | 11 JSON Schema 2020-12 formats, all with a consumer | v1.2.0 |
| [`examples/party-model/`](examples/party-model/) | Worked sample, used as the conformance suite | 33 documents, 44 fixtures, 0 findings |

```bash
go build -o bin/dqctl ./tools/dqctl        # Go 1.23+
./bin/dqctl validate  --root examples/party-model
./bin/dqctl lint      --root examples/party-model
./bin/dqctl align     --root examples/party-model
./bin/dqctl test      --root examples/party-model
./bin/dqctl generate  --root examples/party-model --check
./bin/dqctl bundle    --root examples/party-model --check
./bin/dqctl crd                                  # QualityBinding CRD into dist/
go test ./engine/                          # defect cases + fixture suite
```

Status: architecture baseline (v1.1, revision 3 — aligned to steering-committee
documentation conventions: action titles, value and investment case, risk
register, operating model, NFR register, glossary, decision log).
`dqctl` is partially built (M1.1 of §9); `engine/eval` — the tier 0–2 evaluator that makes
`dqctl test` possible — and the adapters are M2–M5.
