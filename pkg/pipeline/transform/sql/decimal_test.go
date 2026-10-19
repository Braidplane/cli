package sql

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Braidplane/cli/pkg/decimal"
	"github.com/Braidplane/cli/pkg/model"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
	"github.com/stretchr/testify/require"
)

func decimalContract(name string, precision, scale int) *pipepayload.Contract {
	return &pipepayload.Contract{Fields: []pipepayload.ContractField{{Name: name, Type: model.ContractFieldTypeValueDecimal, Precision: &precision, Scale: &scale, Required: true, Nullable: true}}}
}

func TestDeclaredDecimalRoundTrips(t *testing.T) {
	for _, source := range []*pipepayload.Payload{
		{Representation: model.ContractRepresentationCSV, CSV: "price\n0.10\n0.20\n-2.675\n9007199254740993.10\n"},
		{Representation: model.ContractRepresentationJSON, Document: json.RawMessage(`[{"price":0.10},{"price":0.20},{"price":-2.675},{"price":9007199254740993.10}]`)},
		{Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"price": "0.10"}, {"price": "0.20"}, {"price": "-2.675"}, {"price": json.Number("9007199254740993.10")}}},
	} {
		for _, kind := range []model.TransformOutputKind{model.TransformOutputKindRows, model.TransformOutputKindJSON, model.TransformOutputKindCSV} {
			t.Run(source.Representation.String()+"/"+kind.String(), func(t *testing.T) {
				statement := "SELECT CAST(price + 0.20 AS DECIMAL(38,2)) AS v FROM input"
				outputContract := decimalContract("v", 38, 2)
				_, err := ValidateTransformSQL(statement)
				require.NoError(t, err)
				require.NoError(t, ValidateOutputContractTypesWithInputSchemaForOutputKind(statement, kind, outputContract, model.DataSourceTypeS3, map[string]model.ContractFieldType{"price": model.ContractFieldTypeValueDecimal}))
				inferred, err := InferOutputContractWithInputSchema(statement, kind, model.DataSourceTypeS3, map[string]model.ContractFieldType{"price": model.ContractFieldTypeValueDecimal})
				require.NoError(t, err)
				require.Equal(t, outputContract.Fields[0].Precision, inferred.Fields[0].Precision)
				require.Equal(t, outputContract.Fields[0].Scale, inferred.Fields[0].Scale)
				output, err := NewRuntime(qualificationLimits()).Execute(Transform{SQL: statement, InputContract: decimalContract("price", 38, 3), OutputKind: kind}, map[string]*pipepayload.Payload{"input": source})
				require.NoError(t, err)
				require.NoError(t, ValidateOutputContract(output, outputContract))
				switch kind {
				case model.TransformOutputKindRows:
					for i, want := range []string{"0.30", "0.40", "-2.48", "9007199254740993.30"} {
						require.Equal(t, want, output.Rows[i]["v"].(decimal.Value).String())
					}
				case model.TransformOutputKindJSON:
					require.Equal(t, `[{"v":0.30},{"v":0.40},{"v":-2.48},{"v":9007199254740993.30}]`, string(output.Document))
				case model.TransformOutputKindCSV:
					require.Equal(t, "v\n0.30\n0.40\n-2.48\n9007199254740993.30\n", output.CSV)
				case model.TransformOutputKindUnknown:
					t.Fatal("unknown output kind")
				}
			})
		}
	}
}

func TestDecimalAggregatesAndNulls(t *testing.T) {
	input := &pipepayload.Payload{Representation: model.ContractRepresentationJSON, Document: json.RawMessage(`[{"price":0.1},{"price":0.2},{"price":null}]`)}
	for _, test := range []struct{ expr, want string }{
		{"SUM(price)", "0.3"},
		{"AVG(price)", "0.15"},
		{"MIN(price)", "0.1"},
		{"MAX(price)", "0.2"},
		{"SUM(CAST(price AS DECIMAL(38,2)))", "0.3"},
	} {
		output, err := NewRuntime().Execute(Transform{SQL: "SELECT " + test.expr + " AS v FROM input", InputContract: decimalContract("price", 38, 2)}, map[string]*pipepayload.Payload{"input": input})
		require.NoError(t, err)
		value := output.Rows[0]["v"].(decimal.Value)
		expected, err := decimal.Parse(test.want)
		require.NoError(t, err)
		require.Zero(t, value.Cmp(expected))
	}
	for _, statement := range []string{"SELECT price + NULL AS v FROM input", "SELECT SUM(price) AS v FROM input WHERE price IS NULL", "SELECT AVG(price) AS v FROM input WHERE price > 1"} {
		output, err := NewRuntime().Execute(Transform{SQL: statement, InputContract: decimalContract("price", 38, 2)}, map[string]*pipepayload.Payload{"input": input})
		require.NoError(t, err)
		for _, row := range output.Rows {
			require.Nil(t, row["v"])
		}
	}
}

func TestDecimalLiteralsAndNativeDeclarations(t *testing.T) {
	for _, test := range []struct{ statement, want string }{
		{"SELECT 0.1 + 0.2 AS v FROM input", "0.3"},
		{"SELECT -0.1 AS v FROM input", "-0.1"},
		{"SELECT CAST('1e-2' AS DECIMAL(38,2)) AS v FROM input", "0.01"},
	} {
		_, err := ValidateTransformSQL(test.statement)
		require.NoError(t, err)
		inferred, err := InferOutputContractWithInputSchema(test.statement, model.TransformOutputKindRows, model.DataSourceTypeS3, map[string]model.ContractFieldType{"id": model.ContractFieldTypeValueInteger})
		require.NoError(t, err)
		output, err := NewRuntime().Execute(Transform{SQL: test.statement}, map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"id": 1}}}})
		require.NoError(t, err)
		require.Equal(t, test.want, output.Rows[0]["v"].(decimal.Value).String())
		require.NoError(t, ValidateOutputContract(output, inferred))
	}
	for _, native := range []string{"numeric(18,2)", "decimal(38,9)"} {
		field := ContractFieldFromNative(model.DataSourceTypePostgres, "price", native)
		require.Equal(t, model.ContractFieldTypeValueDecimal, field.Type)
		require.NoError(t, pipepayload.ValidateDecimalFieldDeclaration(field))
	}
	require.Equal(t, model.ContractFieldTypeValueNumber, ContractFieldFromNative(model.DataSourceTypePostgres, "price", "numeric").Type)
	require.Equal(t, model.ContractFieldTypeValueString, ContractFieldFromNative(model.DataSourceTypeBigQuery, "price", "bignumeric(76,38)").Type)
	bounded := ContractFieldFromNative(model.DataSourceTypeBigQuery, "price", "bignumeric(38,2)")
	require.Equal(t, model.ContractFieldTypeValueDecimal, bounded.Type)
	require.Equal(t, 38, *bounded.Precision)
	require.Equal(t, 2, *bounded.Scale)
	_, err := ValidateTransformSQL("SELECT " + strings.Repeat("9", 39) + ".1 AS v FROM input")
	require.ErrorIs(t, err, decimal.ErrConversion)
}

func TestDecimalFailuresAndBudgets(t *testing.T) {
	for _, statement := range []string{"SELECT CAST(price AS DECIMAL(39,2)) AS v FROM input", "SELECT CAST(price AS DECIMAL(3,4)) AS v FROM input"} {
		_, err := ValidateTransformSQL(statement)
		require.Error(t, err)
		_, err = NewRuntime().Execute(Transform{SQL: statement}, map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"price": "1"}}}})
		require.Error(t, err)
	}
	for _, input := range []any{"$1.00", "1,000.00", "1e1000000000", strings.Repeat("9", 129), strings.Repeat("9", 39), float64(0.1)} {
		_, err := NewRuntime().Execute(Transform{SQL: "SELECT price AS v FROM input", InputContract: decimalContract("price", 38, 2)}, map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"price": input}}}})
		require.ErrorIs(t, err, decimal.ErrConversion)
	}
	for _, statement := range []string{"SELECT CAST(price AS DECIMAL(38,2)) / 0 AS v FROM input", "SELECT CAST(price AS DECIMAL(38,2)) + f AS v FROM input"} {
		_, err := NewRuntime().Execute(Transform{SQL: statement}, map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"price": "0.10", "f": float64(0.2)}}}})
		require.ErrorIs(t, err, decimal.ErrConversion)
	}
	inputs := map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"price": "0.10"}}}}
	_, err := NewRuntime(Limits{MaxWork: 1}).Execute(Transform{SQL: "SELECT CAST(price AS DECIMAL(38,2)) + 0.2 AS v FROM input"}, inputs)
	require.ErrorIs(t, err, ErrWorkBudgetExceeded)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = NewRuntime().ExecuteContext(ctx, Transform{SQL: "SELECT CAST(price AS DECIMAL(38,2)) AS v FROM input"}, inputs)
	require.ErrorIs(t, err, ErrExecutionBudgetInterrupted)
}

func TestDecimalRoundingIsObservableAndBudgeted(t *testing.T) {
	var usage Usage
	runtime := NewRuntime().WithObserver(func(observed Usage, err error) { usage = observed; require.NoError(t, err) })
	inputs := map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"price": "2.675"}}}}
	output, err := runtime.Execute(Transform{SQL: "SELECT CAST(price AS DECIMAL(3,2)) AS v FROM input"}, inputs)
	require.NoError(t, err)
	require.True(t, output.Rows[0]["v"].(decimal.Value).Inexact())
	require.Equal(t, int64(1), usage.InexactDecimalValues)
	require.Equal(t, int64(1), usage.RoundedDecimalValues)
	require.Positive(t, usage.Work)
	_, err = NewRuntime(Limits{MaxIntermediateBytes: 48}).Execute(Transform{SQL: "SELECT CAST(price AS DECIMAL(3,2)) AS v FROM input"}, inputs)
	require.ErrorIs(t, err, ErrIntermediateBytesExceeded)
	for _, kind := range []model.TransformOutputKind{model.TransformOutputKindRows, model.TransformOutputKindJSON, model.TransformOutputKindCSV} {
		output, err := NewRuntime().Execute(Transform{SQL: "SELECT CAST(price AS DECIMAL(4,3)) AS v FROM input", OutputKind: kind}, inputs)
		require.NoError(t, err)
		require.ErrorIs(t, ValidateOutputContract(output, decimalContract("v", 4, 2)), decimal.ErrConversion)
	}
}

func TestDecimalTypeValidationRejectsMixedFloatAndScaleLoss(t *testing.T) {
	for _, statement := range []string{
		"SELECT CAST(price AS DECIMAL(5,2)) + f AS v FROM input",
		"SELECT CAST(price AS DECIMAL(5,3)) AS v FROM input",
	} {
		err := ValidateOutputContractTypesWithInputSchema(statement, decimalContract("v", 5, 2), model.DataSourceTypeS3, map[string]model.ContractFieldType{"price": model.ContractFieldTypeValueString, "f": model.ContractFieldTypeValueNumber})
		require.ErrorIs(t, err, ErrContractType)
	}
}

func TestDecimalArithmeticDeclarationInference(t *testing.T) {
	for _, test := range []struct {
		expression       string
		precision, scale int
	}{
		{"CAST('1.00' AS DECIMAL(12,2)) + CAST('2.00' AS DECIMAL(12,2))", 13, 2},
		{"CAST('1.00' AS DECIMAL(12,2)) - 0.20", 13, 2},
		{"CAST('1.00' AS DECIMAL(12,2)) * CAST('2.000' AS DECIMAL(10,3))", 22, 5},
		{"0.1 + 0.2", 2, 1},
		{"0.01 * 0.02", 4, 4},
		{"CAST('1.00' AS DECIMAL(38,2)) + 1", 38, 2},
	} {
		t.Run(test.expression, func(t *testing.T) {
			statement := "SELECT " + test.expression + " AS v FROM input"
			inferred, err := InferOutputContractWithInputSchema(statement, model.TransformOutputKindRows, model.DataSourceTypePostgres, nil)
			require.NoError(t, err)
			require.Equal(t, test.precision, *inferred.Fields[0].Precision)
			require.Equal(t, test.scale, *inferred.Fields[0].Scale)
			contract := decimalContract("v", test.precision, test.scale)
			require.NoError(t, ValidateOutputContractTypesWithInputSchema(statement, contract, model.DataSourceTypePostgres, nil))
			output, err := NewRuntime().Execute(Transform{SQL: statement, OutputKind: model.TransformOutputKindRows}, map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"id": 1}}}})
			require.NoError(t, err)
			require.NoError(t, ValidateOutputContract(output, contract))
		})
	}
}
