package engine

// The typed view of the artefact tree. Only the fields this tool reasons about are
// modelled; the full structure is enforced by the schema registry, not by these structs.
// That division matters: adding a field to a format must not require editing the engine.

// Doc is a decoded document, kept raw so it can be validated against its schema.
type Doc struct {
	Path string
	Raw  any
}

// Contract is a canonical business-object contract. ID is the $id, which carries the
// version; Object and Version are derived from it rather than restated anywhere.
type Contract struct {
	ID      string `yaml:"$id"`
	Object  string `yaml:"-"`
	Version string `yaml:"-"`
	Path    string `yaml:"-"`
	Digest  string `yaml:"-"`
	Raw     any    `yaml:"-"`
}

// Steward is the person accountable for an object's definition.
type Steward struct {
	Name  string `yaml:"name"`
	Email string `yaml:"email"`
}

// Meta is <object>.meta.yaml.
type Meta struct {
	Object         string             `yaml:"object"`
	Version        string             `yaml:"version"`
	Owner          string             `yaml:"owner"`
	Steward        Steward            `yaml:"steward"`
	Criticality    string             `yaml:"criticality"`
	Sensitivity    string             `yaml:"sensitivity"`
	Retention      string             `yaml:"retention"`
	AllowedIntents []string           `yaml:"allowedIntents"`
	EventTimeField string             `yaml:"eventTimeField"`
	Thresholds     map[string]float64 `yaml:"thresholds"`
	Path           string             `yaml:"-"`
}

// ValueSetValue is one governed code.
type ValueSetValue struct {
	Code       string `yaml:"code"`
	Label      string `yaml:"label"`
	ValidFrom  string `yaml:"validFrom"`
	ValidTo    string `yaml:"validTo"`
	Deprecated bool   `yaml:"deprecated"`
}

// ValueSet is a governed enumeration, referenced by $id and never restated in a rule.
type ValueSet struct {
	APIVersion    string          `yaml:"apiVersion"`
	Kind          string          `yaml:"kind"`
	ValueSet      string          `yaml:"valueSet"`
	Version       string          `yaml:"version"`
	Title         string          `yaml:"title"`
	Owner         string          `yaml:"owner"`
	UpdatedOn     string          `yaml:"updatedOn"`
	CaseSensitive bool            `yaml:"caseSensitive"`
	Values        []ValueSetValue `yaml:"values"`
	Path          string          `yaml:"-"`
}

// Codes returns the set of governed codes.
func (v *ValueSet) Codes() []string {
	out := make([]string, 0, len(v.Values))
	for _, i := range v.Values {
		out = append(out, i.Code)
	}
	return out
}

// Intent is one entry in the governed intent registry.
type Intent struct {
	Name         string `yaml:"name"`
	Code         string `yaml:"code"`
	Alias        string `yaml:"alias"`
	Description  string `yaml:"description"`
	Owner        string `yaml:"owner"`
	ApprovedOn   string `yaml:"approvedOn"`
	WriteChannel bool   `yaml:"writeChannel"`
}

// IntentRegistry is catalog/intents.yaml.
type IntentRegistry struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Intents    []Intent `yaml:"intents"`
	Path       string   `yaml:"-"`
}

// Rule is one authored rule. Its provenance is recorded so a finding can name the pack.
type Rule struct {
	ID         string   `yaml:"id"`
	When       string   `yaml:"when"`
	Assert     string   `yaml:"assert"`
	Message    string   `yaml:"message"`
	Severity   string   `yaml:"severity"`
	Dimension  string   `yaml:"dimension"`
	Needs      []string `yaml:"needs"`
	CostLimit  *int     `yaml:"costLimit"`
	JSONPath   string   `yaml:"path"`
	Normalizer []string `yaml:"normalizer"`
	Lifecycle  string   `yaml:"lifecycle"`
	Successor  string   `yaml:"successor"`

	PackName string `yaml:"-"`
	PackPath string `yaml:"-"`
	Source   string `yaml:"-"` // syntax.extra, semantic, stateful or intent
}

// Fixture is one test case for one rule.
type Fixture struct {
	Rule   string         `yaml:"rule"`
	Expect string         `yaml:"expect"`
	Input  map[string]any `yaml:"input"`
	Note   string         `yaml:"note"`
}

// Relax is a time-boxed waiver.
type Relax struct {
	Rule          string   `yaml:"rule"`
	Layer         string   `yaml:"layer"`
	Justification string   `yaml:"justification"`
	ApprovedBy    []string `yaml:"approvedBy"`
	ExpiresAt     string   `yaml:"expiresAt"`
}
