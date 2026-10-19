package payload

import (
	"encoding/json"
	"testing"

	"github.com/Braidplane/cli/pkg/model"
	"github.com/stretchr/testify/require"
)

func TestContractConjunctionRetainsIndependentPromises(t *testing.T) {
	contract := func(kind model.ContractFieldType, required bool, v *ContractFieldValidations) *Contract {
		return &Contract{Representation: model.ContractRepresentationRows, Fields: []ContractField{{Name: "id", Type: kind, Required: required, Validations: v}}}
	}
	for _, tc := range []struct {
		name  string
		a, b  *Contract
		valid bool
	}{
		{"stronger type", contract(model.ContractFieldTypeValueNumber, true, nil), contract(model.ContractFieldTypeValueInteger, true, nil), true},
		{"contradictory type", contract(model.ContractFieldTypeValueString, true, nil), contract(model.ContractFieldTypeValueInteger, true, nil), false},
		{"optional absent field", contract(model.ContractFieldTypeValueString, false, nil), contract(model.ContractFieldTypeValueInteger, false, nil), true},
		{"disjoint bounds", contract(model.ContractFieldTypeValueNumber, true, &ContractFieldValidations{Min: json.Number("2")}), contract(model.ContractFieldTypeValueInteger, true, &ContractFieldValidations{Max: json.Number("1")}), false},
		{"empty enum intersection", contract(model.ContractFieldTypeValueString, true, &ContractFieldValidations{Enum: []any{"private-one"}}), contract(model.ContractFieldTypeValueString, true, &ContractFieldValidations{Enum: []any{"private-two"}}), false},
		{"enum excludes integer domain", contract(model.ContractFieldTypeValueInteger, true, nil), contract(model.ContractFieldTypeValueNumber, true, &ContractFieldValidations{Enum: []any{json.Number("0.5")}}), false},
		{"decimal enum includes integer domain", contract(model.ContractFieldTypeValueInteger, true, nil), contract(model.ContractFieldTypeValueDecimal, true, &ContractFieldValidations{Enum: []any{"1.0"}}), true},
		{"decimal enum includes number domain", contract(model.ContractFieldTypeValueDecimal, true, &ContractFieldValidations{Enum: []any{"0.5"}}), contract(model.ContractFieldTypeValueNumber, true, nil), true},
		{"disjoint formats", contract(model.ContractFieldTypeValueString, true, &ContractFieldValidations{Format: "uuid"}), contract(model.ContractFieldTypeValueString, true, &ContractFieldValidations{Format: "email"}), false},
		{"enum against range", contract(model.ContractFieldTypeValueInteger, true, &ContractFieldValidations{Enum: []any{1}}), contract(model.ContractFieldTypeValueInteger, true, &ContractFieldValidations{Min: 2}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			beforeA, _ := json.Marshal(tc.a)
			beforeB, _ := json.Marshal(tc.b)
			err := ValidateContractConjunction(tc.a, tc.b)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "private-")
			}
			afterA, _ := json.Marshal(tc.a)
			afterB, _ := json.Marshal(tc.b)
			require.Equal(t, beforeA, afterA)
			require.Equal(t, beforeB, afterB)
		})
	}
}
