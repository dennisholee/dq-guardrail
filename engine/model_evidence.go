package engine

// BundleObject is one object entry in the manifest.
type BundleObject struct {
	Name           string `yaml:"name"`
	Version        string `yaml:"version"`
	ContractDigest string `yaml:"contractDigest"`
	Criticality    string `yaml:"criticality"`
	Sensitivity    string `yaml:"sensitivity"`
}

// BundlePack is one pack entry in the manifest.
type BundlePack struct {
	Name      string `yaml:"name"`
	Version   string `yaml:"version"`
	Digest    string `yaml:"digest"`
	RuleCount *int   `yaml:"ruleCount"`
}

// BundlePlan points at a compiled plan and pins its digest.
type BundlePlan struct {
	Plan          string `yaml:"plan"`
	Channel       string `yaml:"channel"`
	Intent        string `yaml:"intent"`
	Object        string `yaml:"object"`
	ObjectVersion string `yaml:"objectVersion"`
	Digest        string `yaml:"digest"`
}

// Bundle is the audit entry point: evidence/bundle.yaml inside the artefact.
type Bundle struct {
	BundleFormat int `yaml:"bundleFormat"`
	BuiltFrom    struct {
		Commit string `yaml:"commit"`
		Ref    string `yaml:"ref"`
		Epoch  int64  `yaml:"epoch"`
		Dqctl  string `yaml:"dqctl"`
	} `yaml:"builtFrom"`
	Objects   []BundleObject   `yaml:"objects"`
	ValueSets []BundleValueSet `yaml:"valueSets"`
	Intents   *BundleIntents   `yaml:"intents,omitempty"`
	Packs     []BundlePack     `yaml:"packs"`
	Providers []string         `yaml:"providers"`
	Relax     []BundleRelax    `yaml:"relax"`
	Plans     []BundlePlan     `yaml:"plans"`
	Path      string           `yaml:"-"`
	Digest    string           `yaml:"-"`
}

// BundleRelax is a waiver as the manifest records it. It deliberately has no Justification field:
// the rationale stays in the pack where the author wrote it, and a manifest cannot carry it by
// accident because the type has nowhere to put it.
type BundleRelax struct {
	Rule       string   `yaml:"rule"`
	Layer      string   `yaml:"layer"`
	ApprovedBy []string `yaml:"approvedBy"`
	ExpiresAt  string   `yaml:"expiresAt"`
}

// BundleValueSet is one governed enumeration the bundle carries. The path travels with the
// digest so the claim is checkable by location as well as by hash: a digest alone cannot tell a
// renamed file from the file the author published.
type BundleValueSet struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	Path    string `yaml:"path"`
	Digest  string `yaml:"digest"`
}

// BundleIntents records the intent registry the bundle carries.
type BundleIntents struct {
	Path   string `yaml:"path"`
	Digest string `yaml:"digest"`
}

// Suppress is one time-boxed suppression in a binding.
type Suppress struct {
	Rule     string `yaml:"rule"`
	Until    string `yaml:"until"`
	Ticket   string `yaml:"ticket"`
	Approver string `yaml:"approver"`
	Reason   string `yaml:"reason"`
}

// HTTPChannel is the HTTP enforcement channel.
type HTTPChannel struct {
	Adapter     string            `yaml:"adapter"`
	Paths       []string          `yaml:"paths"`
	OnViolation map[string]string `yaml:"onViolation"`
}

// BindingChannels is the union of the adapters this slice models.
type BindingChannels struct {
	HTTP *HTTPChannel `yaml:"http"`
}

// Binding is the QualityBinding as committed to Git.
type Binding struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"metadata"`
	Spec struct {
		Bundle          string          `yaml:"bundle"`
		FallbackDigest  string          `yaml:"fallbackDigest"`
		Mode            string          `yaml:"mode"`
		OnBundleFailure string          `yaml:"onBundleFailure"`
		Retention       string          `yaml:"retention"`
		Suppress        []Suppress      `yaml:"suppress"`
		Channels        BindingChannels `yaml:"channels"`
	} `yaml:"spec"`
	Path string `yaml:"-"`
}

// Bundle accessors keep the nil case in one place: a tree may have no evidence yet.
func (m *Model) BundleCommit() string {
	if m.Bundle == nil {
		return ""
	}
	return m.Bundle.BuiltFrom.Commit
}

func (m *Model) BundleRef() string {
	if m.Bundle == nil {
		return ""
	}
	return m.Bundle.BuiltFrom.Ref
}

func (m *Model) BundleEpoch() int64 {
	if m.Bundle == nil {
		return 0
	}
	return m.Bundle.BuiltFrom.Epoch
}

func (m *Model) BundleDqctl() string {
	if m.Bundle == nil {
		return ""
	}
	return m.Bundle.BuiltFrom.Dqctl
}

// Violation is one entry in a verdict.
type Violation struct {
	RuleID        string `yaml:"ruleId" json:"ruleId"`
	Severity      string `yaml:"severity" json:"severity"`
	Path          string `yaml:"path" json:"path"`
	Dimension     string `yaml:"dimension" json:"dimension"`
	Intent        string `yaml:"intent" json:"intent"`
	Object        string `yaml:"object" json:"object"`
	ObjectVersion string `yaml:"objectVersion" json:"objectVersion"`
	PackDigest    string `yaml:"packDigest" json:"packDigest"`
	CorrelationID string `yaml:"correlationId" json:"correlationId"`
	Message       string `yaml:"message" json:"message"`
	Retryable     bool   `yaml:"retryable" json:"retryable"`
}

// Verdict is the RFC 9457 problem document a caller receives. Both tags are present because
// the document is authored and read as YAML and emitted as JSON.
type Verdict struct {
	Type          string      `yaml:"type" json:"type"`
	Title         string      `yaml:"title" json:"title"`
	Status        int         `yaml:"status" json:"status"`
	Detail        string      `yaml:"detail" json:"detail"`
	Instance      string      `yaml:"instance" json:"instance"`
	CorrelationID string      `yaml:"correlationId" json:"correlationId"`
	PackDigest    string      `yaml:"packDigest" json:"packDigest"`
	Violations    []Violation `yaml:"violations" json:"violations"`
	Path          string      `yaml:"-" json:"-"`
}
