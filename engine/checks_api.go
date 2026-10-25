package engine

import (
	"fmt"
	"regexp"
)

// addressKeyRe is the shape of an address key: /<intent>-<object>, both hyphen-free.
var addressKeyRe = regexp.MustCompile(`^/[a-z][a-z0-9]*(-[a-z0-9]+)+$`)

// validateAPI checks that every endpoint is a well-formed address key whose declared intent
// and object agree with the path. This is what makes the address key mean something rather
// than being a naming convention.
func validateAPI(m *Model, f *Findings) {
	intents := m.IntentByName()

	for _, a := range m.APIs {
		for path, ops := range a.Paths {
			if !addressKeyRe.MatchString(path) {
				f.Error(CodeAPIKey, a.Path+"#"+path,
					"path is not an address key of the form /<intent>-<object>")
				continue
			}
			intent, object := addressKey(path)
			if _, ok := intents[intent]; !ok {
				f.Error(CodeAPIKey, a.Path+"#"+path, "intent "+intent+" is not in the registry")
			}
			c, ok := m.Contracts[object]
			if !ok {
				f.Error(CodeAPIKey, a.Path+"#"+path, "object "+object+" has no contract")
			}

			for verb, op := range ops {
				where := fmt.Sprintf("%s#%s %s", a.Path, verb, path)
				if op.Intent != intent {
					f.Error(CodeAPIKey, where,
						fmt.Sprintf("x-dq-intent is %q, the path says %q", op.Intent, intent))
				}
				if op.Object != object {
					f.Error(CodeAPIKey, where,
						fmt.Sprintf("x-dq-object is %q, the path says %q", op.Object, object))
				}
				if op.ObjectVersion != "" && ok && op.ObjectVersion != c.Version {
					f.Error(CodeVersionDrift, where,
						fmt.Sprintf("x-dq-object-version %s != contract %s", op.ObjectVersion, c.Version))
				}
			}
		}
	}
}
