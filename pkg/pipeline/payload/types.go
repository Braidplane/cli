package payload

import (
	"bytes"
	"encoding/json"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Braidplane/cli/pkg/model"
	filepayload "github.com/Braidplane/cli/pkg/payload/file"
	formpayload "github.com/Braidplane/cli/pkg/payload/form"
	"github.com/Braidplane/cli/pkg/xerror"
)

//go:generate -command enumer go tool enumer
//go:generate enumer -type=VerificationStatus,Provenance -json -text -linecomment -output=payload_enumer.go

type VerificationStatus int

const (
	VerificationStatusUnknown              VerificationStatus = iota // unknown
	VerificationStatusVerified                                       // verified
	VerificationStatusTransitivelyVerified                           // transitively_verified
	VerificationStatusStaleIntrospection                             // stale_introspection
	VerificationStatusIncompatible                                   // incompatible
)

type Provenance int

const (
	ProvenanceUnknown         Provenance = iota // unknown
	ProvenanceExecutionResult                   // execution_result
	ProvenanceTransformOutput                   // transform_output
	ProvenanceRunInput                          // run_input
)

type Contract struct {
	Name string `json:"name" yaml:"name" validate:"required"`

	Representation model.ContractRepresentation `json:"representation,omitempty" yaml:"representation,omitempty" validate:"omitempty,contractrepresentation"`
	RootShape      model.ContractRootShape      `json:"root_shape,omitempty"     yaml:"root_shape,omitempty"     validate:"omitempty,contractrootshape"`
	Fields         []ContractField              `json:"fields,omitempty"          yaml:"fields,omitempty"          validate:"omitempty,dive"`
}

type ContractField struct {
	Name        string                    `json:"name"                  yaml:"name"                  validate:"required"`
	Type        model.ContractFieldType   `json:"type"                  yaml:"type"                  validate:"required,contractfieldtype"`
	Required    bool                      `json:"required"              yaml:"required"`
	Nullable    bool                      `json:"nullable,omitempty"    yaml:"nullable,omitempty"`
	Description string                    `json:"description,omitempty" yaml:"description,omitempty"`
	Validations *ContractFieldValidations `json:"validations,omitempty" yaml:"validations,omitempty"`
}

type ContractFieldValidations struct {
	Format    string `json:"format,omitempty"     yaml:"format,omitempty"`
	Enum      []any  `json:"enum,omitempty"       yaml:"enum,omitempty"`
	Min       any    `json:"min,omitempty"        yaml:"min,omitempty"`
	Max       any    `json:"max,omitempty"        yaml:"max,omitempty"`
	MinLength *int   `json:"min_length,omitempty" yaml:"min_length,omitempty"`
	MaxLength *int   `json:"max_length,omitempty" yaml:"max_length,omitempty"`
	Unique    *bool  `json:"unique,omitempty"     yaml:"unique,omitempty"`
}

// UnmarshalJSON preserves numeric validation literals exactly while leaving
// non-contract dynamic pipeline values on the default JSON decoding path.
func (v *ContractFieldValidations) UnmarshalJSON(data []byte) error {
	type contractFieldValidations ContractFieldValidations

	if err := rejectNullJSONValidationRules(data); err != nil {
		return err
	}

	var decoded contractFieldValidations
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}

	*v = ContractFieldValidations(decoded)
	return nil
}

// UnmarshalYAML rejects malformed validation rule declarations before zero
// values can make null rules indistinguishable from omitted rules.
func (v *ContractFieldValidations) UnmarshalYAML(value *yaml.Node) error {
	type contractFieldValidations ContractFieldValidations

	if err := rejectNullYAMLValidationRules(value); err != nil {
		return err
	}

	var decoded contractFieldValidations
	if err := value.Decode(&decoded); err != nil {
		return err
	}

	*v = ContractFieldValidations(decoded)
	return nil
}

func rejectNullJSONValidationRules(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	for rule, value := range raw {
		if !isKnownValidationRule(rule) {
			continue
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return xerror.InvalidInput(xerror.Pipeline).Detailf("validation %q must not be null", rule)
		}
	}
	return nil
}

func rejectNullYAMLValidationRules(value *yaml.Node) error {
	if value == nil || value.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i+1 < len(value.Content); i += 2 {
		key := value.Content[i]
		val := value.Content[i+1]
		if !isKnownValidationRule(key.Value) {
			return xerror.InvalidInput(xerror.Pipeline).Detailf("field %s not found in type payload.contractFieldValidations", key.Value)
		}
		if val.Kind == yaml.ScalarNode && val.Tag == "!!null" {
			return xerror.InvalidInput(xerror.Pipeline).Detailf("validation %q must not be null", key.Value)
		}
	}
	return nil
}

func isKnownValidationRule(rule string) bool {
	parsed, err := ContractValidationRuleString(rule)
	return err == nil && parsed != ContractValidationRuleUnknown
}

type Payload struct {
	Representation     model.ContractRepresentation `json:"representation"`
	RootShape          model.ContractRootShape      `json:"root_shape,omitempty"`
	Contract           *Contract                    `json:"contract,omitempty"`
	VerificationStatus VerificationStatus           `json:"verification_status,omitempty"`
	Provenance         Provenance                   `json:"provenance,omitempty"`

	ColumnNames []string                         `json:"column_names,omitempty"`
	RowCount    int                              `json:"row_count,omitempty"`
	Rows        []map[string]any                 `json:"rows,omitempty"`
	Document    json.RawMessage                  `json:"document,omitempty"`
	CSV         string                           `json:"csv,omitempty"`
	File        *filepayload.FilePayloadMetadata `json:"file,omitempty"`
	FileBytes   []byte                           `json:"-" yaml:"-"`
	Form        *formpayload.FormData            `json:"form,omitempty"`
}

type Output struct {
	OutputKind    model.TransformOutputKind `json:"output_kind"`
	RowCount      int                       `json:"row_count"`
	IsHeadlessCSV bool                      `json:"is_headless_csv,omitempty"`

	ColumnNames []string         `json:"column_names,omitempty"`
	Rows        []map[string]any `json:"rows,omitempty"`
	Document    json.RawMessage  `json:"document,omitempty"`
	CSV         string           `json:"csv,omitempty"`
}

func (p *Payload) Clone() *Payload {
	if p == nil {
		return nil
	}

	rows := make([]map[string]any, len(p.Rows))
	for i, row := range p.Rows {
		rows[i] = cloneMapAny(row)
	}

	return &Payload{
		Representation:     p.Representation,
		RootShape:          p.RootShape,
		Contract:           CloneContract(p.Contract),
		VerificationStatus: p.VerificationStatus,
		Provenance:         p.Provenance,
		ColumnNames:        append([]string(nil), p.ColumnNames...),
		RowCount:           p.RowCount,
		Rows:               rows,
		Document:           append([]byte(nil), p.Document...),
		CSV:                p.CSV,
		File:               filepayload.CloneFilePayloadMetadata(p.File),
		FileBytes:          append([]byte(nil), p.FileBytes...),
		Form:               p.Form.Clone(),
	}
}

func CloneContract(contract *Contract) *Contract {
	if contract == nil {
		return nil
	}

	fields := make([]ContractField, len(contract.Fields))
	for i := range contract.Fields {
		fields[i] = cloneContractField(contract.Fields[i])
	}

	return &Contract{
		Name:           contract.Name,
		Representation: contract.Representation,
		RootShape:      contract.RootShape,
		Fields:         fields,
	}
}

func cloneContractField(field ContractField) ContractField {
	field.Validations = CloneContractFieldValidations(field.Validations)
	return field
}

func CloneContractFieldValidations(
	validations *ContractFieldValidations,
) *ContractFieldValidations {
	if validations == nil {
		return nil
	}

	clonedEnum := []any(nil)
	if validations.Enum != nil {
		clonedEnum = make([]any, len(validations.Enum))
		for i := range validations.Enum {
			clonedEnum[i] = cloneValidationLiteral(validations.Enum[i])
		}
	}

	return &ContractFieldValidations{
		Format:    validations.Format,
		Enum:      clonedEnum,
		Min:       cloneValidationLiteral(validations.Min),
		Max:       cloneValidationLiteral(validations.Max),
		MinLength: cloneIntPtr(validations.MinLength),
		MaxLength: cloneIntPtr(validations.MaxLength),
		Unique:    cloneBoolPtr(validations.Unique),
	}
}

func cloneValidationLiteral(value any) any {
	switch typed := value.(type) {
	case json.Number:
		raw := typed.String()
		if strings.ContainsAny(raw, ".eE") {
			if parsed, err := typed.Float64(); err == nil {
				return parsed
			}
			return raw
		}
		if parsed, err := typed.Int64(); err == nil {
			return parsed
		}
		if parsed, err := typed.Float64(); err == nil {
			return parsed
		}
		return raw
	case []any:
		cloned := make([]any, len(typed))
		for i := range typed {
			cloned[i] = cloneValidationLiteral(typed[i])
		}
		return cloned
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, item := range typed {
			cloned[key] = cloneValidationLiteral(item)
		}
		return cloned
	default:
		return value
	}
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneMapAny(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}

	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}

	return output
}
