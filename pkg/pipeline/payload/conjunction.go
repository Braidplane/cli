package payload

import (
	"encoding/json"
	"fmt"

	"github.com/Braidplane/cli/pkg/decimal"
	"github.com/Braidplane/cli/pkg/model"
	"github.com/Braidplane/cli/pkg/xerror"
)

// ValidateContractConjunction rejects provably disjoint declarations. It never
// merges promises: each declaration still requires its own runtime evaluation.
func ValidateContractConjunction(a, b *Contract) error {
	if a == nil || b == nil {
		return nil
	}
	fail := func(field string) error {
		return xerror.InvalidInput(xerror.Pipeline).Detail(fmt.Sprintf("contract obligations contradict at %q", field))
	}
	if a.Representation != model.ContractRepresentationUnknown && b.Representation != model.ContractRepresentationUnknown && a.Representation != b.Representation {
		return fail("representation")
	}
	if a.RootShape != model.ContractRootShapeUnknown && b.RootShape != model.ContractRootShapeUnknown && a.RootShape != b.RootShape {
		return fail("root_shape")
	}
	rightFields := make(map[string]ContractField, len(b.Fields))
	for _, right := range b.Fields {
		rightFields[right.Name] = right
	}
	for _, left := range a.Fields {
		right, exists := rightFields[left.Name]
		if !exists {
			continue
		}
		if (!left.Required && !right.Required) || (left.Nullable && right.Nullable) {
			continue
		}
		numeric := func(t model.ContractFieldType) bool {
			return t == model.ContractFieldTypeValueInteger || t == model.ContractFieldTypeValueNumber || t == model.ContractFieldTypeValueDecimal
		}
		if left.Type != right.Type && (!numeric(left.Type) || !numeric(right.Type)) {
			return fail(left.Name)
		}
		if validationsDisjoint(left, right) {
			return fail(left.Name)
		}
	}
	return nil
}

func validationsDisjoint(a, b ContractField) bool {
	av, bv := ContractFieldValidations{}, ContractFieldValidations{}
	if a.Validations != nil {
		av = *a.Validations
	}
	if b.Validations != nil {
		bv = *b.Validations
	}
	if av.Format != "" && bv.Format != "" && av.Format != bv.Format {
		return true
	}
	for _, pair := range [][2]any{{av.Min, bv.Max}, {bv.Min, av.Max}} {
		if pair[0] == nil || pair[1] == nil {
			continue
		}
		if a.Type == model.ContractFieldTypeValueDatetime && b.Type == a.Type {
			lo, lok, _ := validationBound(pair[0], a.Type, ContractValidationRuleMin)
			hi, hik, _ := validationBound(pair[1], a.Type, ContractValidationRuleMax)
			if lok && hik && compareComparable(lo, hi) > 0 {
				return true
			}
		} else {
			lo, le := decimal.From(pair[0])
			hi, he := decimal.From(pair[1])
			if le == nil && he == nil && lo.Cmp(hi) > 0 {
				return true
			}
		}
	}
	if av.MinLength != nil && bv.MaxLength != nil && *av.MinLength > *bv.MaxLength || bv.MinLength != nil && av.MaxLength != nil && *bv.MinLength > *av.MaxLength {
		return true
	}
	// A finite enum lets the existing value checker prove an empty intersection,
	// including enum-versus-range and enum-versus-format contradictions.
	for _, enum := range [][]any{av.Enum, bv.Enum} {
		if len(enum) == 0 {
			continue
		}
		possible := false
		for _, value := range enum {
			// Decimal enums may spell numeric values as strings; compare a shared
			// numeric candidate rather than rejecting their serialized spelling.
			if a.Type == model.ContractFieldTypeValueInteger || a.Type == model.ContractFieldTypeValueNumber || a.Type == model.ContractFieldTypeValueDecimal {
				if number, err := decimal.From(value); err == nil {
					value = json.Number(number.String())
					if exact := number.Rat(); exact.IsInt() {
						value = json.Number(exact.Num().String())
					}
				}
			}
			left, right := a, b
			left.Name, right.Name = "value", "value"
			rows := []map[string]any{{"value": value}}
			if validationLiteralMatchesFieldType(value, left.Type) && validationLiteralMatchesFieldType(value, right.Type) && ValidateContractFieldValues(&Contract{Fields: []ContractField{left}}, rows) == nil && ValidateContractFieldValues(&Contract{Fields: []ContractField{right}}, rows) == nil {
				possible = true
				break
			}
		}
		if !possible {
			return true
		}
	}
	return false
}
