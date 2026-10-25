package engine

// PackMetadata is the identity of a rule pack.
type PackMetadata struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	Owner   string `yaml:"owner"`
	Alias   string `yaml:"alias"`
}

// PackObject names the object a pack governs and the contract version it targets.
type PackObject struct {
	Name          string `yaml:"name"`
	SchemaVersion string `yaml:"schemaVersion"`
}

// PackSyntax carries the tier-0 derivation and any structural rule the schema cannot state.
type PackSyntax struct {
	FromSchema struct {
		Ref          string `yaml:"ref"`
		AssertFormat bool   `yaml:"assertFormat"`
	} `yaml:"fromSchema"`
	Extra []Rule `yaml:"extra"`
}

// PackProfile is the intent-level expectations block.
type PackProfile struct {
	SchemaUses         string `yaml:"schemaUses"`
	ExpectPrecondition bool   `yaml:"expectPrecondition"`
	Idempotency        string `yaml:"idempotency"`
}

// PackSpec is the body of an object or intent pack. Fields that do not apply to the kind
// are empty; the schema registry decides which combinations are legal.
type PackSpec struct {
	Object    PackObject  `yaml:"object"`
	Requires  []string    `yaml:"requires"`
	Syntax    *PackSyntax `yaml:"syntax"`
	Semantic  []Rule      `yaml:"semantic"`
	Stateful  []Rule      `yaml:"stateful"`
	Tests     []Fixture   `yaml:"tests"`
	Intent    string      `yaml:"intent"`
	AppliesTo []string    `yaml:"appliesTo"`
	Rules     []Rule      `yaml:"rules"`
	Profile   PackProfile `yaml:"profile"`
	Relax     []Relax     `yaml:"relax"`
}

// Pack is one authored rule pack.
type Pack struct {
	APIVersion string       `yaml:"apiVersion"`
	Kind       string       `yaml:"kind"`
	Metadata   PackMetadata `yaml:"metadata"`
	Spec       PackSpec     `yaml:"spec"`
	Path       string       `yaml:"-"`
	Digest     string       `yaml:"-"`
}

func (p *Pack) IsIntent() bool { return p.Kind == "IntentPack" }

// Rules returns every authored rule with its provenance filled in, in tier order.
func (p *Pack) Rules() []Rule {
	out := []Rule{}
	add := func(rs []Rule, source string) {
		for _, r := range rs {
			r.PackName = p.Metadata.Name
			r.PackPath = p.Path
			r.Source = source
			out = append(out, r)
		}
	}
	if p.Spec.Syntax != nil {
		add(p.Spec.Syntax.Extra, "syntax.extra")
	}
	add(p.Spec.Semantic, "semantic")
	add(p.Spec.Stateful, "stateful")
	add(p.Spec.Rules, "intent")
	return out
}

// PlanRule is one composed rule inside a compiled plan.
type PlanRule struct {
	ID        string   `yaml:"id"`
	Tier      int      `yaml:"tier"`
	Severity  string   `yaml:"severity"`
	Dimension string   `yaml:"dimension"`
	When      string   `yaml:"when"`
	Assert    string   `yaml:"assert"`
	Needs     []string `yaml:"needs"`
	CostLimit *int     `yaml:"costLimit"`
	Origin    string   `yaml:"origin"`
}

// PlanRelax is the plan's copy of an applied waiver.
type PlanRelax struct {
	Rule      string `yaml:"rule"`
	ExpiresAt string `yaml:"expiresAt"`
}

// Plan is one resolved plan per (channel, intent, object). Internal format, planFormat 1.
type Plan struct {
	PlanFormat    int    `yaml:"planFormat"`
	Channel       string `yaml:"channel"`
	Intent        string `yaml:"intent"`
	Object        string `yaml:"object"`
	ObjectVersion string `yaml:"objectVersion"`
	SchemaMatcher struct {
		Ref    string `yaml:"ref"`
		Digest string `yaml:"digest"`
	} `yaml:"schemaMatcher"`
	Providers []string    `yaml:"providers"`
	Rules     []PlanRule  `yaml:"rules"`
	Relax     []PlanRelax `yaml:"relax"`
	Path      string      `yaml:"-"`
	Digest    string      `yaml:"-"`
}
