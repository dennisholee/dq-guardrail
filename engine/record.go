package engine

import (
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// StructureProblem is one way a payload fails the canonical contract.
type StructureProblem struct {
	Path    string
	Message string
}

// RecordValidator validates payloads against the canonical contracts, with value sets already
// expanded. One compiler instance per model: contracts carry absolute $ids, and re-registering
// the same $id twice in one compiler is an error.
type RecordValidator struct {
	schemas map[string]*jsonschema.Schema
}

// NewRecordValidator expands and compiles every contract in the model.
func NewRecordValidator(m *Model) (*RecordValidator, error) {
	v := &RecordValidator{schemas: map[string]*jsonschema.Schema{}}
	compiler := jsonschema.NewCompiler()

	for name, c := range m.Contracts {
		doc, err := ExpandContract(c, m.ValueSets)
		if err != nil {
			return nil, err
		}
		value, err := jsonValue(doc)
		if err != nil {
			return nil, err
		}
		// Register under a synthetic location so two versions of one object cannot collide.
		loc := fmt.Sprintf("https://registry.internal/dq/compiled/%s/%s", name, c.Version)
		if err := compiler.AddResource(loc, value); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		schema, err := compiler.Compile(loc)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		v.schemas[name] = schema
	}
	return v, nil
}

// Validate reports every structural problem in a payload. An unknown object is a programming
// error, not a data error, so it returns one.
func (v *RecordValidator) Validate(object string, payload any) ([]StructureProblem, error) {
	schema, ok := v.schemas[object]
	if !ok {
		return nil, fmt.Errorf("no compiled contract for object %q", object)
	}
	value, err := jsonValue(payload)
	if err != nil {
		return nil, err
	}
	if err := schema.Validate(value); err != nil {
		return flattenSchemaError(err), nil
	}
	return nil, nil
}

// flattenSchemaError turns the validator's tree into one problem per leaf, so a caller gets a
// list of paths rather than one paragraph.
//
// The message is built from the failing keyword, never from the validator's own text: that text
// embeds the offending value, and a value echoed into a verdict is exactly what the design
// forbids. A path plus a constraint name is enough for a caller to act on.
func flattenSchemaError(err error) []StructureProblem {
	problems := []StructureProblem{}
	var walk func(any)
	walk = func(node any) {
		if node == nil {
			return
		}
		if ve, ok := node.(*jsonschema.ValidationError); ok {
			if len(ve.Causes) == 0 {
				var b strings.Builder
				for _, seg := range ve.InstanceLocation {
					b.WriteString("/")
					b.WriteString(seg)
				}
				path := b.String()
				if path == "" {
					path = "/"
				}
				problems = append(problems, StructureProblem{
					Path:    path,
					Message: constraintMessage(ve.ErrorKind),
				})
				return
			}
			for _, cause := range ve.Causes {
				walk(cause)
			}
		}
	}
	walk(err)
	if len(problems) == 0 {
		problems = append(problems, StructureProblem{Path: "/", Message: "does not match the canonical contract"})
	}
	return problems
}

// constraintMessage names the constraint that failed, without the value that failed it.
func constraintMessage(kind jsonschema.ErrorKind) string {
	if kind == nil {
		return "does not match the canonical contract"
	}
	keyword := kind.KeywordPath()
	if len(keyword) == 0 {
		return "does not match the canonical contract"
	}
	return "fails the " + strings.Join(keyword, "/") + " constraint"
}
