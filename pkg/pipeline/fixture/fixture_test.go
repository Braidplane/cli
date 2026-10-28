package fixture

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/model"
	"github.com/Braidplane/cli/pkg/pipeline/payload"
	transformsql "github.com/Braidplane/cli/pkg/pipeline/transform/sql"
)

func TestFixtureCatchesMappingRegressionAndRequiredFieldBeforeDeployment(t *testing.T) {
	t.Parallel()
	p, s := 18, 2
	transform := transformsql.Transform{
		SQL:        "SELECT item_code AS sku, CAST(price AS DECIMAL(18,2)) AS unit_price, 'PLN' AS currency FROM input",
		OutputKind: model.TransformOutputKindRows,
		OutputContract: &payload.Contract{Name: "catalog", Fields: []payload.ContractField{
			{Name: "sku", Type: model.ContractFieldTypeValueString, Required: true},
			{Name: "unit_price", Type: model.ContractFieldTypeValueDecimal, Precision: &p, Scale: &s, Required: true},
			{Name: "currency", Type: model.ContractFieldTypeValueString, Required: true},
		}},
	}
	fixture := Fixture{
		Inputs:   map[string]json.RawMessage{"input": json.RawMessage(`[{"item_code":"CHAIR-42","price":"249.90"}]`)},
		Expected: json.RawMessage(`[{"sku":"CHAIR-42","unit_price":249.9,"currency":"PLN"}]`),
	}
	result := Run(t.Context(), "map-products", transform, fixture)
	require.True(t, result.Passed, "%+v", result.Diagnostics)
	require.Contains(t, string(result.Output), "249.90")
	transform.SQL = strings.Replace(transform.SQL, "CAST(price AS DECIMAL(18,2))", "CAST(price AS DECIMAL(18,2)) + 1", 1)
	result = Run(t.Context(), "map-products", transform, fixture)
	require.False(t, result.Passed)
	require.Equal(t, "TRANSFORM_FIXTURE_ASSERTION_FAILED", result.Diagnostics[0].Code)
	require.True(t, result.Asserted)
	require.Equal(t, "map-products", result.Diagnostics[0].Step)
	require.Empty(t, result.Output)
	transform.SQL = "SELECT item_code AS sku FROM input"
	result = Run(t.Context(), "map-products", transform, fixture)
	require.False(t, result.Passed)
	require.Equal(t, "TRANSFORM_FIXTURE_OUTPUT_CONTRACT_FAILED", result.Diagnostics[0].Code)
	require.Equal(t, "unit_price", result.Diagnostics[0].Field)
}

func TestFixtureFailuresDoNotEchoSampleValues(t *testing.T) {
	t.Parallel()
	secret := "sensitive-customer-value"
	result := Run(t.Context(), "map", transformsql.Transform{SQL: "SELECT CAST(price AS DECIMAL(18,2)) AS price FROM input"},
		Fixture{Inputs: map[string]json.RawMessage{"input": json.RawMessage(`[{"price":"` + secret + `"}]`)}})
	require.False(t, result.Passed)
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(raw), secret)
	require.Empty(t, result.Output)
}

func TestFixtureExactNumbersOutputKindsAndOrder(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		kind     model.TransformOutputKind
		expected string
	}{
		{model.TransformOutputKindRows, `[{"value":9007199254740993.30}]`},
		{model.TransformOutputKindJSON, `[{"value":9007199254740993.3}]`},
		{model.TransformOutputKindCSV, `"value\n9007199254740993.30\n"`},
	} {
		result := Run(t.Context(), "map", transformsql.Transform{SQL: "SELECT CAST(price AS DECIMAL(18,2)) AS value FROM input", OutputKind: test.kind},
			Fixture{Inputs: map[string]json.RawMessage{"input": json.RawMessage(`[{"price":"9007199254740993.30"}]`)}, Expected: json.RawMessage(test.expected)})
		require.True(t, result.Passed, "%+v", result.Diagnostics)
	}
	result := Run(t.Context(), "map", transformsql.Transform{SQL: "SELECT id FROM input"}, Fixture{
		Inputs: map[string]json.RawMessage{"input": json.RawMessage(`[{"id":1},{"id":2}]`)}, Expected: json.RawMessage(`[{"id":2},{"id":1}]`),
	})
	require.False(t, result.Passed)
}

func TestFixtureBudgetsAndMalformedInputs(t *testing.T) {
	t.Parallel()
	for _, input := range []string{`{}`, `null`, `[null]`, `[] {}`, `[{"n":1e99999999}]`, `[{"n":` + strings.Repeat("[", 33) + "0" + strings.Repeat("]", 33) + `}]`, `[{"value":"` + strings.Repeat("x", MaxBytes) + `"}]`} {
		result := Run(t.Context(), "map", transformsql.Transform{SQL: "SELECT id FROM input"}, Fixture{Inputs: map[string]json.RawMessage{"input": json.RawMessage(input)}})
		require.False(t, result.Passed)
		require.NotEmpty(t, result.Diagnostics)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result := Run(ctx, "map", transformsql.Transform{SQL: "SELECT id FROM input"}, Fixture{Inputs: map[string]json.RawMessage{"input": json.RawMessage(`[{"id":1}]`)}})
	require.False(t, result.Passed)
	result = Run(t.Context(), "map", transformsql.Transform{SQL: "SELECT id FROM input", OutputKind: model.TransformOutputKindRows}, Fixture{Inputs: map[string]json.RawMessage{"input": json.RawMessage(`[]`)}, Expected: json.RawMessage(`[]`)})
	require.True(t, result.Passed)
	tooManyFields := &payload.Contract{Fields: make([]payload.ContractField, 257)}
	result = Run(t.Context(), "map", transformsql.Transform{SQL: "SELECT id FROM input", OutputContract: tooManyFields}, Fixture{Inputs: map[string]json.RawMessage{"input": json.RawMessage(`[]`)}})
	require.False(t, result.Passed)
	require.Equal(t, "TRANSFORM_FIXTURE_CONTRACT_INVALID", result.Diagnostics[0].Code)
}

func TestFixtureChecksRepresentationRootShapeAndInputValues(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name           string
		representation model.ContractRepresentation
		root           model.ContractRootShape
		input          string
		passed         bool
	}{
		{"rows", model.ContractRepresentationRows, model.ContractRootShapeUnknown, `[{"id":"one"}]`, true},
		{"json array", model.ContractRepresentationJSON, model.ContractRootShapeArray, `[{"id":"one"}]`, true},
		{"json object", model.ContractRepresentationJSON, model.ContractRootShapeObject, `{"id":"one"}`, true},
		{"csv", model.ContractRepresentationCSV, model.ContractRootShapeUnknown, `"id\none\n"`, true},
		{"wrong json root", model.ContractRepresentationJSON, model.ContractRootShapeObject, `[{"id":"one"}]`, false},
		{"missing input field", model.ContractRepresentationRows, model.ContractRootShapeUnknown, `[{"other":"one"}]`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Run(t.Context(), "map", transformsql.Transform{SQL: "SELECT 'ok' AS result FROM input", InputContract: &payload.Contract{
				Name: "source", Representation: test.representation, RootShape: test.root,
				Fields: []payload.ContractField{{Name: "id", Type: model.ContractFieldTypeValueString, Required: true}},
			}}, Fixture{Inputs: map[string]json.RawMessage{"input": json.RawMessage(test.input)}})
			require.Equal(t, test.passed, result.Passed, "%+v", result.Diagnostics)
		})
	}
	for _, kind := range []model.TransformOutputKind{model.TransformOutputKindCSV, model.TransformOutputKindJSON} {
		result := Run(t.Context(), "map", transformsql.Transform{SQL: "SELECT id FROM input", OutputKind: kind, OutputContract: &payload.Contract{
			Name: "target", Representation: model.ContractRepresentationRows,
			Fields: []payload.ContractField{{Name: "id", Type: model.ContractFieldTypeValueString, Required: true}},
		}}, Fixture{Inputs: map[string]json.RawMessage{"input": json.RawMessage(`[{"id":"one"}]`)}})
		require.False(t, result.Passed)
		require.Empty(t, result.Output)
	}
}
