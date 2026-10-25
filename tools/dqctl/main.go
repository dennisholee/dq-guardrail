// Command dqctl is the alignment, test and publication tool for the DQ guardrail
// artefacts. See docs/architecture/delivery-design-pack.md §3 for its contract.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dennisholee/dq-guardrail/engine"
	dqfile "github.com/dennisholee/dq-guardrail/engine/adapter/file"
	"gopkg.in/yaml.v3"
)

const version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return engine.ExitToolchain
	}
	switch args[0] {
	case "version":
		return cmdVersion(args[1:])
	case "validate":
		return cmdValidate(args[1:])
	case "lint":
		return cmdLint(args[1:])
	case "align":
		return cmdAlign(args[1:])
	case "bundle":
		return cmdBundle(args[1:])
	case "test":
		return cmdTest(args[1:])
	case "generate":
		return cmdGenerate(args[1:])
	case "publish":
		return cmdPublish(args[1:])
	case "verify":
		return cmdVerify(args[1:])
	case "explain":
		return cmdExplain(args[1:])
	case "gate":
		return cmdGate(args[1:])
	case "crd":
		return cmdCRD(args[1:])
	case "scorecard":
		return cmdScorecard(args[1:])
	case "help", "-h", "--help":
		usage()
		return engine.ExitOK
	default:
		fmt.Fprintf(os.Stderr, "dqctl: unknown command %q\n\n", args[0])
		usage()
		return engine.ExitToolchain
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `dqctl — data quality artefact tool

Usage:
  dqctl version  [--json]
  dqctl validate [--root DIR] [--schemas DIR] [--json]
  dqctl lint     [--root DIR] [--json]
  dqctl align    [--root DIR] [--check] [--json]
  dqctl bundle   [--root DIR] [--out FILE] [--manifest FILE] [--check] [--epoch N] [--json]
  dqctl test     [--root DIR] [--json]
  dqctl generate [--root DIR] [--check] [--json]
  dqctl publish  --bundle FILE [--layout DIR] [--registry REF] [--json]
  dqctl verify   [--layout DIR] [--json]
  dqctl explain  --intent NAME --object NAME --input FILE [--out FILE] [--json]
  dqctl gate     --manifest FILE --input FILE [--out DIR] [--json]
  dqctl crd      [--schemas DIR] [--out FILE] [--json]
  dqctl scorecard [--root DIR] [--check] [--json]

validate  parse every document, validate it against catalog/schemas, check the chain
          from contract to verdict and recompute every digest in the evidence
lint      enforce invariant S2: no rule may restate a governed enumeration
align     L1 the API must $ref the canonical contract, not restate it
          L3 a request must not shadow a field the contract already defines
          L4 every rule must name a field the contract declares
bundle    build the deterministic archive, or with --check verify that the committed
          manifest still describes the tree
test      evaluate every rule fixture; a rule whose fixtures cannot fail is not a rule
generate  write the derived rule index (which rules run on each endpoint), or with --check
          verify the committed copy is fresh
publish   write an OCI image layout for a bundle and print the digest a binding pins.
          Pushing to a registry is NOT implemented: --registry writes the layout and exits 3
verify    check a layout read back from disk: the header, the index, and every digest in the
          chain against the bytes that are actually there. A digest nobody verifies is a comment
explain   decide one record and, with --out, write the verdict. This is how a committed
          verdict is regenerated and how a squad sees why a payload was rejected
gate      run the file channel over one delivery: verify the checksum, then stream every
          record to accepted.jsonl or rejects.csv; _SUCCESS only when nothing was rejected
crd       render the QualityBinding CustomResourceDefinition from its schema, with the
          conditional constraints carried as CEL because a structural schema cannot hold if/then
scorecard write the readiness scorecard, or with --check verify the committed one is current

--json writes exactly one JSON document to stdout; human output goes to stderr.
Exit codes: 0 ok, 1 findings, 3 toolchain failure.
`)
}

func cmdVersion(args []string) int {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "write the JSON report")
	if err := fs.Parse(args); err != nil {
		return engine.ExitToolchain
	}
	rep := engine.NewReport("version", version)
	doc := rep.Document()
	if *jsonOut {
		if err := doc.WriteJSON(os.Stdout); err != nil {
			return engine.ExitToolchain
		}
		return doc.ExitCode()
	}
	fmt.Printf("dqctl %s (output schema %s)\n", version, engine.OutputSchemaVersion)
	return engine.ExitOK
}

func cmdValidate(args []string) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	root := fs.String("root", ".", "artefact tree to validate")
	schemas := fs.String("schemas", "catalog/schemas", "schema registry directory")
	jsonOut := fs.Bool("json", false, "write the JSON report")
	if err := fs.Parse(args); err != nil {
		return engine.ExitToolchain
	}

	rep := engine.NewReport("validate", version)
	reg, err := engine.LoadSchemaRegistry(*schemas)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dqctl: %v\n", err)
		rep.Findings().Error(engine.CodeRegistryMissing, *schemas, err.Error())
		return finish(rep.Document(), *jsonOut)
	}
	m, err := engine.Load(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dqctl: %v\n", err)
		rep.Findings().Error(engine.CodeParse, *root, err.Error())
		return finish(rep.Document(), *jsonOut)
	}

	found := engine.ValidateTree(m, reg)
	merge(rep.Findings(), found)
	summarise(m, rep.Findings())
	return finish(rep.Document(), *jsonOut)
}

func cmdLint(args []string) int {
	fs := flag.NewFlagSet("lint", flag.ContinueOnError)
	root := fs.String("root", ".", "artefact tree to lint")
	jsonOut := fs.Bool("json", false, "write the JSON report")
	if err := fs.Parse(args); err != nil {
		return engine.ExitToolchain
	}

	rep := engine.NewReport("lint", version)
	m, err := engine.Load(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dqctl: %v\n", err)
		rep.Findings().Error(engine.CodeParse, *root, err.Error())
		return finish(rep.Document(), *jsonOut)
	}
	for _, pe := range m.ParseErrors {
		rep.Findings().Error(engine.CodeParse, pe.Path, "does not parse: "+pe.Message)
	}
	engine.LintS2(m, rep.Findings())
	summarise(m, rep.Findings())
	return finish(rep.Document(), *jsonOut)
}

// cmdAlign runs the L1/L3/L4 alignment gates. With --check, a failure is a non-zero exit,
// which is what makes contract drift a CI failure rather than a review comment.
func cmdAlign(args []string) int {
	fs := flag.NewFlagSet("align", flag.ContinueOnError)
	root := fs.String("root", ".", "artefact tree to align")
	check := fs.Bool("check", true, "fail the run when a finding is an error")
	jsonOut := fs.Bool("json", false, "write the JSON report")
	if err := fs.Parse(args); err != nil {
		return engine.ExitToolchain
	}

	rep := engine.NewReport("align", version)
	m, err := engine.Load(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dqctl: %v\n", err)
		rep.Findings().Error(engine.CodeParse, *root, err.Error())
		return finish(rep.Document(), *jsonOut)
	}
	merge(rep.Findings(), engine.AlignTree(m))
	summarise(m, rep.Findings())

	doc := rep.Document()
	if !*check {
		doc.OK = true
	}
	return finish(doc, *jsonOut)
}

// cmdBundle builds the deterministic archive, or with --check verifies that the committed
// manifest still describes the tree. The check is what makes the evidence falsifiable.
func cmdBundle(args []string) int {
	fs := flag.NewFlagSet("bundle", flag.ContinueOnError)
	root := fs.String("root", ".", "artefact tree")
	out := fs.String("out", "dist/dq-bundle.tar.gz", "archive path")
	manifestOut := fs.String("manifest", "", "write the manifest on its own instead of an archive")
	check := fs.Bool("check", false, "verify the committed manifest instead of writing an archive")
	commit := fs.String("commit", "", "git commit (default: HEAD)")
	ref := fs.String("ref", "main", "git ref")
	epoch := fs.Int64("epoch", 0, "SOURCE_DATE_EPOCH in seconds (default: from the environment)")
	jsonOut := fs.Bool("json", false, "write the JSON report")
	if err := fs.Parse(args); err != nil {
		return engine.ExitToolchain
	}

	rep := engine.NewReport("bundle", version)
	m, err := engine.Load(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dqctl: %v\n", err)
		rep.Findings().Error(engine.CodeParse, *root, err.Error())
		return finish(rep.Document(), *jsonOut)
	}

	stamp := *epoch
	if stamp == 0 {
		if v := os.Getenv("SOURCE_DATE_EPOCH"); v != "" {
			stamp, _ = strconv.ParseInt(v, 10, 64)
		}
	}

	from := engine.BuiltFrom{Commit: *commit, Ref: *ref, Epoch: stamp, Dqctl: version}
	if *check {
		from = engine.BuiltFrom{
			Commit: m.BundleCommit(),
			Ref:    m.BundleRef(),
			Epoch:  m.BundleEpoch(),
			Dqctl:  m.BundleDqctl(),
		}
	} else if from.Commit == "" {
		from.Commit = gitCommit(*root)
	}

	fresh, err := engine.BuildManifest(m, from)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dqctl: %v\n", err)
		rep.Findings().Error(engine.CodeChainUnknown, *root, err.Error())
		return finish(rep.Document(), *jsonOut)
	}

	if *check {
		if m.Bundle == nil {
			rep.Findings().Error(engine.CodeChainUnknown, *root, "no committed manifest at evidence/bundle.yaml")
			return finish(rep.Document(), *jsonOut)
		}
		for _, msg := range engine.CompareManifests(fresh, m.Bundle) {
			rep.Findings().Error(engine.CodeDigestMismatch, m.Bundle.Path,
				msg+" — the manifest was not regenerated after the tree changed")
		}
		return finish(rep.Document(), *jsonOut)
	}

	manifest, err := engine.MarshalManifest(fresh)
	if err != nil {
		rep.Findings().Error(engine.CodeChainUnknown, *root, err.Error())
		return finish(rep.Document(), *jsonOut)
	}

	// --manifest writes the manifest on its own. The committed evidence/bundle.yaml used to be
	// hand-authored because there was no way to generate it; a hand-edited manifest is exactly
	// the artefact this toolchain exists to argue against.
	if *manifestOut != "" {
		if err := os.MkdirAll(filepath.Dir(*manifestOut), 0o755); err != nil {
			rep.Findings().Error(engine.CodeChainUnknown, *manifestOut, err.Error())
			return finish(rep.Document(), *jsonOut)
		}
		if err := os.WriteFile(*manifestOut, manifest, 0o644); err != nil {
			rep.Findings().Error(engine.CodeChainUnknown, *manifestOut, err.Error())
			return finish(rep.Document(), *jsonOut)
		}
		rep.Artifact("bundle-manifest", *manifestOut)
		if !*jsonOut {
			fmt.Fprintf(os.Stderr, "%s: %d objects, %d value sets, %d packs, %d plans\n",
				*manifestOut, len(fresh.Objects), len(fresh.ValueSets), len(fresh.Packs), len(fresh.Plans))
		}
		return finish(rep.Document(), *jsonOut)
	}

	files, err := engine.BundleFiles(m, manifest)
	if err != nil {
		rep.Findings().Error(engine.CodeChainUnknown, *root, err.Error())
		return finish(rep.Document(), *jsonOut)
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		rep.Findings().Error(engine.CodeChainUnknown, *out, err.Error())
		return finish(rep.Document(), *jsonOut)
	}
	f, err := os.Create(*out)
	if err != nil {
		rep.Findings().Error(engine.CodeChainUnknown, *out, err.Error())
		return finish(rep.Document(), *jsonOut)
	}
	writeErr := engine.WriteArchive(f, files, stamp)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		rep.Findings().Error(engine.CodeChainUnknown, *out, "cannot write archive")
		return finish(rep.Document(), *jsonOut)
	}

	archive, err := os.ReadFile(*out)
	if err != nil {
		rep.Findings().Error(engine.CodeChainUnknown, *out, err.Error())
		return finish(rep.Document(), *jsonOut)
	}
	rep.Artifact("bundle", *out)
	fmt.Fprintf(os.Stderr, "bundle %s\n  archive  %s\n  manifest %s\n",
		*out, engine.DigestBytes(archive), engine.DigestBytes(manifest))
	return finish(rep.Document(), *jsonOut)
}

// gitCommit returns HEAD for the tree, or "" when git is unavailable — a manifest without a
// commit is still reproducible, it just cannot say which revision produced it.
func gitCommit(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// cmdPublish writes an OCI image layout for a bundle archive and prints the manifest digest a
// binding pins. A registry push is not implemented, and this command says so and exits 3
// rather than reporting a success that did not happen.
func cmdPublish(args []string) int {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	bundlePath := fs.String("bundle", "", "bundle archive produced by `dqctl bundle --out`")
	layout := fs.String("layout", "dist/oci", "OCI image layout directory to write")
	registry := fs.String("registry", "", "registry repository to push to (not implemented)")
	epoch := fs.Int64("epoch", 0, "SOURCE_DATE_EPOCH in seconds; omitted keeps the digest stable")
	jsonOut := fs.Bool("json", false, "write the JSON report")
	if err := fs.Parse(args); err != nil {
		return engine.ExitToolchain
	}
	if *bundlePath == "" {
		fmt.Fprintln(os.Stderr, "dqctl: publish requires --bundle")
		return engine.ExitToolchain
	}

	rep := engine.NewReport("publish", version)

	stamp := *epoch
	if stamp == 0 {
		if v := os.Getenv("SOURCE_DATE_EPOCH"); v != "" {
			stamp, _ = strconv.ParseInt(v, 10, 64)
		}
	}
	var created time.Time
	if stamp > 0 {
		created = time.Unix(stamp, 0)
	}

	published, err := engine.Publish(*bundlePath, *layout, created)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dqctl: %v\n", err)
		rep.Findings().Error(engine.CodeChainUnknown, *bundlePath, err.Error())
		return finish(rep.Document(), *jsonOut)
	}
	rep.Artifact("oci-layout", published.Dir)

	ref := published.Reference(*registry)
	if !*jsonOut {
		fmt.Fprintf(os.Stderr, "layout %s\n  layer    %s\n  manifest %s\n  reference %s\n",
			published.Dir, published.LayerDigest, published.ManifestDigest, ref)
	}
	doc := rep.Document()
	if *jsonOut {
		if err := doc.WriteJSON(os.Stdout); err != nil {
			return engine.ExitToolchain
		}
	}

	if *registry != "" {
		fmt.Fprintf(os.Stderr,
			"dqctl: pushing to %s is not implemented. The layout above is complete and the reference is\n"+
				"       %s; pushing it needs a registry client this tool does not have.\n",
			*registry, ref)
		return engine.ExitToolchain
	}
	return doc.ExitCode()
}

// cmdVerify checks an OCI layout read back from disk. Publishing writes a layout; a binding pins
// a digest; but until something reads the layout and hashes the bytes, the pin is an assertion
// nobody has tested. This is the consumer's side of it.
func cmdVerify(args []string) int {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	layout := fs.String("layout", "dist/oci", "OCI image layout directory to verify")
	contents := fs.Bool("contents", false, "also open the bundle inside the layout and check its manifest against its files")
	jsonOut := fs.Bool("json", false, "write the JSON report")
	if err := fs.Parse(args); err != nil {
		return engine.ExitToolchain
	}

	rep := engine.NewReport("verify", version)

	verified, err := engine.VerifyLayout(*layout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dqctl: %v\n", err)
		rep.Findings().Error(engine.CodeChainMismatch, *layout, err.Error())
		return finish(rep.Document(), *jsonOut)
	}
	rep.Artifact("oci-layout", verified.Dir)

	var inside engine.ArchiveContents
	if *contents {
		inside, err = engine.VerifyBundleArchive(verified.Layer)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dqctl: %v\n", err)
			rep.Findings().Error(engine.CodeChainMismatch, verified.LayerDigest, err.Error())
			return finish(rep.Document(), *jsonOut)
		}
		for _, name := range inside.Undeclared {
			rep.Findings().Add(engine.CodeChainUnknown, engine.SeverityInfo, name,
				"carried by the bundle but not described by its manifest; bundleFormat 1 has no field for it")
		}
	}

	if !*jsonOut {
		fmt.Fprintf(os.Stderr, "layout %s verified\n  manifest %s\n  layer    %s\n  blobs    %d\n",
			verified.Dir, verified.ManifestDigest, verified.LayerDigest, verified.Blobs)
		if *contents {
			fmt.Fprintf(os.Stderr, "bundle inside the layer\n  files    %d\n  objects  %d\n  packs    %d\n  plans    %d\n  undescribed %d\n",
				inside.Files, inside.Objects, inside.Packs, inside.Plans, len(inside.Undeclared))
		}
	}
	doc := rep.Document()
	if *jsonOut {
		if err := doc.WriteJSON(os.Stdout); err != nil {
			return engine.ExitToolchain
		}
	}
	return doc.ExitCode()
}

// cmdScorecard writes the readiness scorecard, or with --check verifies it is current. It is a
// generated artefact for the same reason the rule index is: the hand-written version went stale
// every time a rule changed.
func cmdScorecard(args []string) int {
	fs := flag.NewFlagSet("scorecard", flag.ContinueOnError)
	root := fs.String("root", ".", "artefact tree")
	out := fs.String("out", "evidence/scorecard.md", "scorecard path, relative to the tree")
	check := fs.Bool("check", false, "verify the committed scorecard instead of writing it")
	jsonOut := fs.Bool("json", false, "write the JSON report")
	if err := fs.Parse(args); err != nil {
		return engine.ExitToolchain
	}

	rep := engine.NewReport("scorecard", version)
	m, err := engine.Load(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dqctl: %v\n", err)
		rep.Findings().Error(engine.CodeParse, *root, err.Error())
		return finish(rep.Document(), *jsonOut)
	}
	fresh, err := engine.GenerateScorecard(m)
	if err != nil {
		rep.Findings().Error(engine.CodeGeneratedStale, *out, err.Error())
		return finish(rep.Document(), *jsonOut)
	}

	path := filepath.Join(*root, filepath.FromSlash(*out))
	if *check {
		committed, readErr := os.ReadFile(path)
		if readErr != nil {
			rep.Findings().Error(engine.CodeGeneratedStale, *out,
				"no committed scorecard; run `dqctl scorecard`")
			return finish(rep.Document(), *jsonOut)
		}
		if !bytes.Equal(committed, fresh) {
			rep.Findings().Error(engine.CodeGeneratedStale, *out,
				"the committed scorecard does not match the tree; run `dqctl scorecard`")
		}
		if !*jsonOut {
			state := map[bool]string{true: "fresh", false: "stale"}[len(rep.Findings().Items()) == 0]
			fmt.Fprintf(os.Stderr, "%s: scorecard %s\n", m.Root, state)
		}
		return finish(rep.Document(), *jsonOut)
	}

	if err := os.WriteFile(path, fresh, 0o644); err != nil {
		rep.Findings().Error(engine.CodeGeneratedStale, *out, err.Error())
		return finish(rep.Document(), *jsonOut)
	}
	rep.Artifact("scorecard", path)
	if !*jsonOut {
		fmt.Fprintf(os.Stderr, "wrote %s\n", path)
	}
	return finish(rep.Document(), *jsonOut)
}

// cmdCRD renders the QualityBinding CustomResourceDefinition from the binding schema.
func cmdCRD(args []string) int {
	fs := flag.NewFlagSet("crd", flag.ContinueOnError)
	schemas := fs.String("schemas", "catalog/schemas", "schema registry directory")
	out := fs.String("out", "dist/qualitybinding-crd.yaml", "where to write the CRD")
	jsonOut := fs.Bool("json", false, "write the JSON report")
	if err := fs.Parse(args); err != nil {
		return engine.ExitToolchain
	}

	rep := engine.NewReport("crd", version)
	raw, err := os.ReadFile(filepath.Join(*schemas, "quality-binding.schema.yaml"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "dqctl: %v\n", err)
		rep.Findings().Error(engine.CodeRegistryMissing, *schemas, err.Error())
		return finish(rep.Document(), *jsonOut)
	}
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		rep.Findings().Error(engine.CodeParse, *schemas, err.Error())
		return finish(rep.Document(), *jsonOut)
	}

	crd, err := engine.GenerateCRD(doc)
	if err != nil {
		rep.Findings().Error(engine.CodeRegistryMissing, *schemas,
			"the binding schema cannot be projected onto a CRD: "+err.Error())
		return finish(rep.Document(), *jsonOut)
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		rep.Findings().Error(engine.CodeChainUnknown, *out, err.Error())
		return finish(rep.Document(), *jsonOut)
	}
	if err := os.WriteFile(*out, crd, 0o644); err != nil {
		rep.Findings().Error(engine.CodeChainUnknown, *out, err.Error())
		return finish(rep.Document(), *jsonOut)
	}
	rep.Artifact("crd", *out)

	if !*jsonOut {
		fmt.Fprintf(os.Stderr, "wrote %s (%s)\n", *out, engine.DigestBytes(crd))
	}
	return finish(rep.Document(), *jsonOut)
}

// cmdGate runs the file-channel gate over one delivery: verify, stream, accept or reject.
func cmdGate(args []string) int {
	fs := flag.NewFlagSet("gate", flag.ContinueOnError)
	root := fs.String("root", ".", "artefact tree")
	manifest := fs.String("manifest", "", "delivery manifest")
	input := fs.String("input", "", "data file")
	out := fs.String("out", "dist/gate", "directory for accepted.jsonl, rejects.csv and _SUCCESS")
	jsonOut := fs.Bool("json", false, "write the JSON report")
	if err := fs.Parse(args); err != nil {
		return engine.ExitToolchain
	}
	if *manifest == "" || *input == "" {
		fmt.Fprintln(os.Stderr, "dqctl: gate requires --manifest and --input")
		return engine.ExitToolchain
	}

	rep := engine.NewReport("gate", version)
	m, err := engine.Load(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dqctl: %v\n", err)
		rep.Findings().Error(engine.CodeParse, *root, err.Error())
		return finish(rep.Document(), *jsonOut)
	}
	e, err := engine.NewEngineWithClock(m, time.Now)
	if err != nil {
		rep.Findings().Error(engine.CodeChainUnknown, *root, err.Error())
		return finish(rep.Document(), *jsonOut)
	}

	result, err := dqfile.Run(m, e, *manifest, *input, *out)
	if err != nil {
		rep.Findings().Error(engine.CodeChainUnknown, *manifest, err.Error())
		return finish(rep.Document(), *jsonOut)
	}

	if result.Refused != "" {
		rep.Findings().Error(engine.CodeGateRefused, *manifest, result.Refused)
		return finish(rep.Document(), *jsonOut)
	}
	for _, r := range result.Rejects {
		for _, v := range r.Violations {
			rep.Findings().AddRule(engine.CodeGateRejected, engine.SeverityError,
				fmt.Sprintf("%s#record %d", *input, r.Row),
				fmt.Sprintf("record %d: %s — %s", r.Row, v.RuleID, v.Message), v.RuleID)
		}
	}
	rep.Artifact("accepted", filepath.Join(*out, "accepted.jsonl"))
	rep.Artifact("rejects", filepath.Join(*out, "rejects.csv"))

	if !*jsonOut {
		fmt.Fprintf(os.Stderr, "%s: %d records — %d accepted, %d rejected\n",
			*input, result.Records, result.Accepted, result.Rejected)
	}
	return finish(rep.Document(), *jsonOut)
}

// cmdExplain decides one record and writes the verdict. It is how a committed verdict is
// regenerated, and how a squad works out why a payload was rejected without deploying.
func cmdExplain(args []string) int {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	root := fs.String("root", ".", "artefact tree")
	intent := fs.String("intent", "", "intent name, e.g. onboard")
	object := fs.String("object", "", "object name, e.g. party")
	input := fs.String("input", "", "JSON file holding the request body")
	correlation := fs.String("correlation-id", "", "correlation id to stamp on the verdict")
	out := fs.String("out", "", "write the verdict document here")
	jsonOut := fs.Bool("json", false, "write the JSON report")
	if err := fs.Parse(args); err != nil {
		return engine.ExitToolchain
	}
	if *input == "" {
		fmt.Fprintln(os.Stderr, "dqctl: explain requires --input (and --intent/--object unless the file is a scenario)")
		return engine.ExitToolchain
	}

	rep := engine.NewReport("explain", version)
	m, err := engine.Load(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dqctl: %v\n", err)
		rep.Findings().Error(engine.CodeParse, *root, err.Error())
		return finish(rep.Document(), *jsonOut)
	}
	raw, err := os.ReadFile(*input)
	if err != nil {
		rep.Findings().Error(engine.CodeParse, *input, err.Error())
		return finish(rep.Document(), *jsonOut)
	}

	// A scenario file carries the intent, the object, the correlation id and the payload, so
	// regenerating a verdict is one command with no repeated arguments.
	var scenario struct {
		Intent        string         `json:"intent"`
		Object        string         `json:"object"`
		CorrelationID string         `json:"correlationId"`
		Payload       map[string]any `json:"payload"`
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &scenario); err == nil && scenario.Payload != nil {
		payload = scenario.Payload
		if *intent == "" {
			*intent = scenario.Intent
		}
		if *object == "" {
			*object = scenario.Object
		}
		if *correlation == "" {
			*correlation = scenario.CorrelationID
		}
	} else if err := json.Unmarshal(raw, &payload); err != nil {
		rep.Findings().Error(engine.CodeParse, *input, "the request body is not a JSON object: "+err.Error())
		return finish(rep.Document(), *jsonOut)
	}

	plan := m.PlanFor("http", *intent, *object)
	if plan == nil {
		rep.Findings().Error(engine.CodeChainUnknown, *root,
			fmt.Sprintf("no compiled plan for http/%s/%s", *intent, *object))
		return finish(rep.Document(), *jsonOut)
	}

	// A runtime decides with a clock; the conformance runner does not. explain is the former.
	e, err := engine.NewEngineWithClock(m, time.Now)
	if err != nil {
		rep.Findings().Error(engine.CodeChainUnknown, *root, err.Error())
		return finish(rep.Document(), *jsonOut)
	}

	decision, err := e.Decide(plan, engine.Request{Payload: payload, CorrelationID: *correlation})
	if err != nil {
		rep.Findings().Error(engine.CodeChainUnknown, *root, err.Error())
		return finish(rep.Document(), *jsonOut)
	}

	instance := "/" + *intent + "-" + *object
	problem := engine.ProblemFromDecision(decision, instance, *correlation)
	if problem == nil {
		fmt.Fprintf(os.Stderr, "%s accepted: %d rules ran\n", instance, decision.RulesRun)
		return finish(rep.Document(), *jsonOut)
	}

	if *out != "" {
		body, err := json.MarshalIndent(problem, "", "  ")
		if err != nil {
			rep.Findings().Error(engine.CodeChainUnknown, *out, err.Error())
			return finish(rep.Document(), *jsonOut)
		}
		if err := os.WriteFile(*out, append(body, '\n'), 0o644); err != nil {
			rep.Findings().Error(engine.CodeChainUnknown, *out, err.Error())
			return finish(rep.Document(), *jsonOut)
		}
		rep.Artifact("verdict", *out)
	}

	fmt.Fprintf(os.Stderr, "%s rejected: %d violation(s), %d rules ran\n",
		instance, len(decision.Violations), decision.RulesRun)
	return finish(rep.Document(), *jsonOut)
}

// cmdGenerate writes the derived rule index, or with --check verifies it is fresh. A derived
// artefact that is edited by hand is a second source of truth, so the check exists.
func cmdGenerate(args []string) int {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	root := fs.String("root", ".", "artefact tree")
	check := fs.Bool("check", false, "verify the committed artefact instead of writing it")
	jsonOut := fs.Bool("json", false, "write the JSON report")
	if err := fs.Parse(args); err != nil {
		return engine.ExitToolchain
	}

	rep := engine.NewReport("generate", version)
	m, err := engine.Load(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dqctl: %v\n", err)
		rep.Findings().Error(engine.CodeParse, *root, err.Error())
		return finish(rep.Document(), *jsonOut)
	}

	if *check {
		committed, readErr := engine.ReadRuleIndex(*root)
		if readErr != nil {
			rep.Findings().Error(engine.CodeGeneratedStale, engine.RuleIndexPath,
				"no committed rule index; run `dqctl generate`")
			return finish(rep.Document(), *jsonOut)
		}
		problems, checkErr := engine.CheckRuleIndex(m, committed)
		if checkErr != nil {
			rep.Findings().Error(engine.CodeGeneratedStale, engine.RuleIndexPath, checkErr.Error())
			return finish(rep.Document(), *jsonOut)
		}
		for _, p := range problems {
			rep.Findings().Error(engine.CodeGeneratedStale, engine.RuleIndexPath, p)
		}
		if !*jsonOut {
			fmt.Printf("%s: rule index %s\n", m.Root, map[bool]string{true: "stale", false: "fresh"}[len(problems) > 0])
		}
		return finish(rep.Document(), *jsonOut)
	}

	data, err := engine.GenerateRuleIndex(m)
	if err != nil {
		rep.Findings().Error(engine.CodeGeneratedStale, engine.RuleIndexPath, err.Error())
		return finish(rep.Document(), *jsonOut)
	}
	path, err := engine.WriteRuleIndex(*root, data)
	if err != nil {
		rep.Findings().Error(engine.CodeGeneratedStale, engine.RuleIndexPath, err.Error())
		return finish(rep.Document(), *jsonOut)
	}
	rep.Artifact("rules-index", path)
	if !*jsonOut {
		fmt.Fprintf(os.Stderr, "wrote %s\n", path)
	}
	return finish(rep.Document(), *jsonOut)
}

// cmdTest executes every rule fixture. This is the gate that proves a rule can fail: a rule
// whose fixtures only ever pass is a rule nobody has exercised.
func cmdTest(args []string) int {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	root := fs.String("root", ".", "artefact tree")
	jsonOut := fs.Bool("json", false, "write the JSON report")
	if err := fs.Parse(args); err != nil {
		return engine.ExitToolchain
	}

	rep := engine.NewReport("test", version)
	m, err := engine.Load(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dqctl: %v\n", err)
		rep.Findings().Error(engine.CodeParse, *root, err.Error())
		return finish(rep.Document(), *jsonOut)
	}
	for _, pe := range m.ParseErrors {
		rep.Findings().Error(engine.CodeParse, pe.Path, "does not parse: "+pe.Message)
	}

	results, err := engine.RunFixtures(m)
	if err != nil {
		rep.Findings().Error(engine.CodeFixtureFailed, *root, err.Error())
		return finish(rep.Document(), *jsonOut)
	}

	passed, failed, edge := 0, 0, 0
	if len(results) == 0 {
		// A gate that passes because it tested nothing is worse than no gate.
		rep.Findings().Error(engine.CodeFixtureFailed, *root,
			"no fixtures were found: the rule language is untested in this tree")
	}
	for _, r := range results {
		switch {
		case r.Err != nil:
			failed++
			rep.Findings().AddRule(engine.CodeFixtureFailed, engine.SeverityError, r.PackPath,
				fmt.Sprintf("fixture for %s (expect %s) could not run: %v", r.Rule, r.Expect, r.Err), r.Rule)
		case r.Mismatch:
			failed++
			rep.Findings().AddRule(engine.CodeFixtureFailed, engine.SeverityError, r.PackPath,
				fmt.Sprintf("fixture for %s expected %s, but the rule %s", r.Rule, r.Expect, outcome(r)), r.Rule)
		case r.Expect == "edge":
			edge++
		default:
			passed++
		}
	}

	if !*jsonOut {
		fmt.Printf("%s: %d fixtures — %d passed, %d failed, %d edge\n",
			m.Root, len(results), passed, failed, edge)
	}
	return finish(rep.Document(), *jsonOut)
}

// outcome describes what a rule actually did, for the failure message.
func outcome(r engine.FixtureResult) string {
	if !r.Ran {
		return "never applied (its when guard was false)"
	}
	if r.Passed {
		return "passed"
	}
	return "failed"
}

// merge copies findings from a check run into the report's accumulator.
func merge(dst *engine.Findings, src *engine.Findings) {
	for _, f := range src.Items() {
		if f.RuleID != nil {
			dst.AddRule(f.Code, f.Severity, f.Path, f.Message, *f.RuleID)
			continue
		}
		dst.Add(f.Code, f.Severity, f.Path, f.Message)
	}
}

// summarise writes the human-readable view. It goes to stderr, always: the CLI contract says
// stdout carries exactly one JSON document and nothing else, so human output must not appear
// there even when --json was not requested but stdout is being piped.
func summarise(m *engine.Model, f *engine.Findings) {
	w := os.Stderr
	counts := f.Count()
	fmt.Fprintf(w, "%s: %d documents, %d contracts, %d object rules, %d intent rules\n",
		m.Root, len(m.Docs), len(m.Contracts),
		countRuleIDs(m, false), countRuleIDs(m, true))

	for _, c := range engine.CoverageReport(m) {
		state := "complete"
		if len(c.Gaps) > 0 {
			state = "GAP: no rule for " + join(c.Gaps)
		}
		fmt.Fprintf(w, "  coverage %-18s declared %-2d covered %-2d  %s\n",
			c.Object, len(c.Declared), len(c.Covered), state)
	}

	fmt.Fprintf(w, "%d error(s), %d warning(s), %d note(s)\n",
		counts[engine.SeverityError], counts[engine.SeverityWarn], counts[engine.SeverityInfo])
}

func finish(doc *engine.Report, jsonOut bool) int {
	if jsonOut {
		if err := doc.WriteJSON(os.Stdout); err != nil {
			return engine.ExitToolchain
		}
		return doc.ExitCode()
	}
	for _, f := range doc.Findings {
		fmt.Printf("  %-5s %-22s %s\n        %s\n", f.Severity, f.Code, f.Path, f.Message)
	}
	return doc.ExitCode()
}

func countRuleIDs(m *engine.Model, intent bool) int {
	n := 0
	for _, p := range m.AllPacks() {
		if p.IsIntent() == intent {
			n += len(p.Rules())
		}
	}
	return n
}

func join(in []string) string {
	out := ""
	for i, s := range in {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}
