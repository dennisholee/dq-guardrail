package engine

// FileManifest is the contract between an upstream extract and the gate job. It is the
// document file-manifest.schema.yaml describes; the gate refuses a file before reading a
// record if the address key does not resolve or the checksum does not match, because a
// partial read of an unverified file is worse than no read at all.
type FileManifest struct {
	SchemaVersion string `yaml:"schemaVersion" json:"schemaVersion"`
	Intent        string `yaml:"intent" json:"intent"`
	Object        string `yaml:"object" json:"object"`
	ObjectVersion string `yaml:"objectVersion" json:"objectVersion"`
	DeliveryID    string `yaml:"deliveryId" json:"deliveryId"`
	ProducedAt    string `yaml:"producedAt" json:"producedAt"`
	Source        struct {
		System string `yaml:"system" json:"system"`
		Run    string `yaml:"run" json:"run"`
	} `yaml:"source" json:"source"`
	Format struct {
		Type      string `yaml:"type" json:"type"`
		Encoding  string `yaml:"encoding" json:"encoding"`
		RecordSep string `yaml:"recordSeparator" json:"recordSeparator"`
		CSV       *struct {
			Delimiter string `yaml:"delimiter" json:"delimiter"`
			Quote     string `yaml:"quote" json:"quote"`
			Header    bool   `yaml:"header" json:"header"`
		} `yaml:"csv" json:"csv"`
	} `yaml:"format" json:"format"`
	RecordCount int `yaml:"recordCount" json:"recordCount"`
	Checksum    struct {
		Algorithm string `yaml:"algorithm" json:"algorithm"`
		Value     string `yaml:"value" json:"value"`
	} `yaml:"checksum" json:"checksum"`
	Retention string `yaml:"retention" json:"retention"`
}
