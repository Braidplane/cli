package sql

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Braidplane/cli/pkg/model"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
	"github.com/stretchr/testify/require"
)

func TestSQLPredicateTruthTables(t *testing.T) {
	t.Parallel()
	values := []any{false, true, nil}
	for _, tc := range []struct {
		expression string
		want       [3][3]predicateValue
	}{
		{"a AND b", [3][3]predicateValue{
			{predicateFalse, predicateFalse, predicateFalse},
			{predicateFalse, predicateTrue, predicateUnknown},
			{predicateFalse, predicateUnknown, predicateUnknown},
		}},
		{"a OR b", [3][3]predicateValue{
			{predicateFalse, predicateTrue, predicateUnknown},
			{predicateTrue, predicateTrue, predicateTrue},
			{predicateUnknown, predicateTrue, predicateUnknown},
		}},
		{"NOT a", [3][3]predicateValue{
			{predicateTrue, predicateTrue, predicateTrue},
			{predicateFalse, predicateFalse, predicateFalse},
			{predicateUnknown, predicateUnknown, predicateUnknown},
		}},
	} {
		stmt, _, err := parseTransformSelect("SELECT id FROM input WHERE " + tc.expression)
		require.NoError(t, err)
		for i, a := range values {
			for j, b := range values {
				t.Run(fmt.Sprintf("%s/%v/%v", tc.expression, a, b), func(t *testing.T) {
					got, err := evaluateWhereExpression(stmt.Where, map[string]any{"a": a, "b": b}, newExecutionBudget(context.Background(), Limits{}), 1)
					require.NoError(t, err)
					require.Equal(t, tc.want[i][j], got)
				})
			}
		}
	}
	for _, operator := range []string{"=", "!=", "<", "<=", ">", ">="} {
		stmt, _, err := parseTransformSelect("SELECT id FROM input WHERE a " + operator + " b")
		require.NoError(t, err)
		for _, row := range []map[string]any{{"a": nil, "b": nil}, {"a": nil, "b": 1}, {"a": 1, "b": nil}} {
			got, err := evaluateWhereExpression(stmt.Where, row, newExecutionBudget(context.Background(), Limits{}), 1)
			require.NoError(t, err)
			require.Equal(t, predicateUnknown, got, "%s with %v", operator, row)
		}
	}
}

func TestSQLNullPredicatesAtRetentionBoundaries(t *testing.T) {
	t.Parallel()
	inputs := map[string]*pipepayload.Payload{
		"input": {Representation: model.ContractRepresentationJSON, Document: json.RawMessage(`[{"id":1,"n":null},{"id":2,"n":0},{"id":3,"n":2}]`)},
		"other": {Representation: model.ContractRepresentationJSON, Document: json.RawMessage(`[{"key":1}]`)},
	}
	schema := map[string]model.ContractFieldType{"input.id": model.ContractFieldTypeValueInteger, "input.n": model.ContractFieldTypeValueNumber, "other.key": model.ContractFieldTypeValueInteger}
	contract := &pipepayload.Contract{Fields: []pipepayload.ContractField{{Name: "v", Type: model.ContractFieldTypeValueInteger, Required: true}}}
	for _, tc := range []struct{ predicate, rows string }{
		{"input.n = NULL", `[]`},
		{"NULL != input.n", `[]`},
		{"input.n != 0", `[{"v":3}]`},
		{"NOT (input.n > 0)", `[{"v":2}]`},
		{"NOT input.n", `[{"v":2}]`},
		{"NOT NOT input.n", `[{"v":3}]`},
		{"input.n IS NULL", `[{"v":1}]`},
		{"input.n IS NOT NULL", `[{"v":2},{"v":3}]`},
		{"NULL OR input.n > 0", `[{"v":3}]`},
		{"NOT (NULL AND input.n > 0)", `[{"v":2}]`},
		{"NOT (NULL OR input.n > 0)", `[]`},
		{"input.n + 1 > 1", `[{"v":3}]`},
		{"input.n + 1 IS NULL", `[{"v":1}]`},
	} {
		for _, clause := range []string{"WHERE", "JOIN other ON"} {
			t.Run(clause+"/"+tc.predicate, func(t *testing.T) {
				statement := "SELECT input.id AS v FROM input " + clause + " " + tc.predicate
				_, err := ValidateTransformSQL(statement)
				require.NoError(t, err)
				require.NoError(t, ValidateOutputContractTypesWithInputSchema(statement, contract, model.DataSourceTypePostgres, schema))
				output, err := NewRuntime(qualificationLimits()).Execute(Transform{SQL: statement}, inputs)
				require.NoError(t, err)
				require.NoError(t, ValidateOutputContract(output, contract))
				encoded, err := json.Marshal(output.Rows)
				require.NoError(t, err)
				require.Equal(t, tc.rows, string(encoded))
			})
		}
	}
	for _, predicate := range []string{"input.n = other.n", "NOT (input.n != other.n)"} {
		statement := "SELECT input.id AS v FROM input JOIN input AS other ON " + predicate
		require.NoError(t, ValidateOutputContractTypesWithInputSchema(statement, contract, model.DataSourceTypePostgres, schema))
		output, err := NewRuntime(qualificationLimits()).Execute(Transform{SQL: statement}, inputs)
		require.NoError(t, err)
		require.Equal(t, []map[string]any{{"v": json.Number("2")}, {"v": json.Number("3")}}, output.Rows)
		_, err = NewRuntime(Limits{MaxWork: 8}).Execute(Transform{SQL: statement}, inputs)
		require.ErrorIs(t, err, ErrWorkBudgetExceeded)
	}
}

func TestSQLPredicateShortCircuit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		predicate string
		wantRows  int
		wantError error
	}{
		{"FALSE AND input.n / 0 = 1", 0, nil},
		{"TRUE OR input.n / 0 = 1", 1, nil},
		{"NULL AND input.n / 0 = 1", 0, ErrNumericConversion},
		{"NULL OR input.n / 0 = 1", 0, ErrNumericConversion},
		{"NULL AND missing = 1", 0, ErrRuntimeProjectionFieldNotFound},
		{"NULL OR missing = 1", 0, ErrRuntimeProjectionFieldNotFound},
	} {
		for _, clause := range []string{"WHERE", "JOIN input AS other ON"} {
			t.Run(clause+"/"+tc.predicate, func(t *testing.T) {
				statement := "SELECT input.n AS v FROM input " + clause + " " + tc.predicate
				output, err := NewRuntime().Execute(Transform{SQL: statement}, map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"n": 1}}}})
				if tc.wantError != nil {
					require.ErrorIs(t, err, tc.wantError)
					return
				}
				require.NoError(t, err)
				require.Equal(t, tc.wantRows, output.RowCount)
			})
		}
	}
}

func TestSQLNullArithmetic(t *testing.T) {
	t.Parallel()
	schema := map[string]model.ContractFieldType{"input.n": model.ContractFieldTypeValueNumber}
	for _, operator := range []string{"+", "-", "*", "/"} {
		for _, operands := range [][2]string{{"n", "1"}, {"1", "n"}, {"NULL", "1"}, {"1", "NULL"}, {"NULL", "NULL"}, {"NULL", "0"}, {"n + 1", "1"}} {
			statement := "SELECT " + operands[0] + " " + operator + " " + operands[1] + " AS v FROM input"
			for _, kind := range []model.TransformOutputKind{model.TransformOutputKindRows, model.TransformOutputKindJSON, model.TransformOutputKindCSV} {
				t.Run(statement+"/"+kind.String(), func(t *testing.T) {
					contract := &pipepayload.Contract{Fields: []pipepayload.ContractField{{Name: "v", Type: model.ContractFieldTypeValueNumber, Required: true, Nullable: true}}}
					_, err := ValidateTransformSQL(statement)
					require.NoError(t, err)
					require.NoError(t, ValidateOutputContractTypesWithInputSchema(statement, contract, model.DataSourceTypePostgres, schema))
					output, err := NewRuntime(qualificationLimits()).Execute(Transform{SQL: statement, OutputKind: kind}, map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationJSON, Document: json.RawMessage(`[{"n":null}]`)}})
					require.NoError(t, err)
					require.NoError(t, ValidateOutputContract(output, contract))
					switch kind {
					case model.TransformOutputKindUnknown, model.TransformOutputKindRows:
						require.Equal(t, []map[string]any{{"v": nil}}, output.Rows)
					case model.TransformOutputKindJSON:
						require.Equal(t, `[{"v":null}]`, string(output.Document))
					case model.TransformOutputKindCSV:
						require.Equal(t, "v\n\n", output.CSV)
					}
					if kind != model.TransformOutputKindCSV {
						contract.Fields[0].Nullable = false
						require.ErrorIs(t, ValidateOutputContract(output, contract), ErrOutputContractViolation)
					}
				})
			}
		}
	}
	for _, statement := range []string{"SELECT n % 1 AS v FROM input", "SELECT n + missing AS v FROM input"} {
		_, err := NewRuntime().Execute(Transform{SQL: statement}, map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"n": nil}}}})
		require.Error(t, err)
	}
	require.Error(t, ValidateOutputContractTypesWithInputSchema("SELECT NULL + unknown_type AS v FROM input", &pipepayload.Contract{Fields: []pipepayload.ContractField{{Name: "v", Type: model.ContractFieldTypeValueNumber}}}, model.DataSourceTypePostgres, map[string]model.ContractFieldType{"input.unknown_type": model.ContractFieldTypeUnknown}))
}
