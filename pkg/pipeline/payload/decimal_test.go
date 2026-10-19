package payload_test

import (
	"encoding/json"
	"testing"

	"github.com/Braidplane/cli/pkg/decimal"
	"github.com/Braidplane/cli/pkg/model"
	"github.com/Braidplane/cli/pkg/pipeline/payload"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func decimalField(name string, precision, scale int) payload.ContractField {
	return payload.ContractField{Name: name, Type: model.ContractFieldTypeValueDecimal, Precision: &precision, Scale: &scale, Required: true}
}

func TestDecimalContractDeclarationAndValues(t *testing.T) {
	field := decimalField("price", 5, 2)
	contract := &payload.Contract{Fields: []payload.ContractField{field}}
	require.NoError(t, payload.ValidateContractFieldDeclarations(contract))
	cloned := payload.CloneContract(contract)
	*cloned.Fields[0].Precision = 6
	require.Equal(t, 5, *contract.Fields[0].Precision)
	for _, input := range []any{"2.675", "1000.00", "$1.00", float64(0.1)} {
		_, err := payload.DecimalFieldValue(input, field)
		require.ErrorIs(t, err, decimal.ErrConversion)
	}
	value, err := payload.DecimalFieldValue(json.Number("2.67"), field)
	require.NoError(t, err)
	require.Equal(t, "2.67", value.String())
	for _, bad := range []payload.ContractField{{Name: "v", Type: model.ContractFieldTypeValueDecimal}, decimalField("v", 39, 2), decimalField("v", 2, 3), {Name: "v", Type: model.ContractFieldTypeValueNumber, Precision: field.Precision}} {
		require.Error(t, payload.ValidateContractFieldDeclarations(&payload.Contract{Fields: []payload.ContractField{bad}}))
	}
	require.True(t, payload.DecimalFieldCompatible(decimalField("v", 5, 2), decimalField("v", 7, 3)))
	require.False(t, payload.DecimalFieldCompatible(decimalField("v", 5, 2), decimalField("v", 5, 1)))
	require.False(t, payload.DecimalFieldCompatible(decimalField("v", 5, 2), decimalField("v", 5, 3)))
}

func TestDecimalNestedBindingAndValidations(t *testing.T) {
	field := decimalField("items[].price", 38, 2)
	field.Nullable = true
	field.Validations = &payload.ContractFieldValidations{Min: json.Number("0.10"), Max: json.Number("9007199254740993.10")}
	contract := &payload.Contract{Fields: []payload.ContractField{field}}
	original := []map[string]any{{"items": []any{map[string]any{"price": "0.10"}, map[string]any{"price": json.Number("9007199254740993.10")}, map[string]any{"price": nil}}}}
	rows, err := payload.CoerceDecimalRows(contract, original)
	require.NoError(t, err)
	require.Nil(t, payload.ValidateContractFieldValues(contract, rows))
	require.Equal(t, "0.10", original[0]["items"].([]any)[0].(map[string]any)["price"])
	_, err = payload.CoerceDecimalRows(contract, []map[string]any{{"items": []any{map[string]any{"price": "0.10"}, map[string]any{"price": "0.105"}}}})
	require.Error(t, err)
	rows, err = payload.CoerceDecimalRows(contract, []map[string]any{{"items": []any{map[string]any{"price": "9007199254740993.11"}}}})
	require.NoError(t, err)
	require.NotNil(t, payload.ValidateContractFieldValues(contract, rows))
}

func TestDecimalYAMLValidationPrecisionSurvivesClone(t *testing.T) {
	var contract payload.Contract
	require.NoError(t, yaml.Unmarshal([]byte(`fields:
  - name: price
    type: decimal
    precision: 38
    scale: 2
    validations:
      min: 9007199254740993.10
      enum: [9007199254740993.10]
`), &contract))
	cloned := payload.CloneContract(&contract)
	require.Equal(t, json.Number("9007199254740993.10"), cloned.Fields[0].Validations.Min)
	require.Equal(t, []any{json.Number("9007199254740993.10")}, cloned.Fields[0].Validations.Enum)
	require.Nil(t, payload.ValidateContractFieldValues(cloned, []map[string]any{{"price": json.Number("9007199254740993.10")}}))
	require.NotNil(t, payload.ValidateContractFieldValues(cloned, []map[string]any{{"price": json.Number("9007199254740993.09")}}))
}
