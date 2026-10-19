package sql

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"

	"github.com/Braidplane/cli/pkg/model"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
	"github.com/stretchr/testify/require"
)

func TestExactIntegerTransform(t *testing.T) {
	t.Parallel()
	for _, literal := range []string{"9007199254740991", "9007199254740992", "9007199254740993", "-9007199254740993", "-9223372036854775808", "9223372036854775807"} {
		for _, kind := range []model.TransformOutputKind{model.TransformOutputKindRows, model.TransformOutputKindJSON, model.TransformOutputKindCSV} {
			t.Run(fmt.Sprintf("%s/%s", literal, kind), func(t *testing.T) {
				contract := &pipepayload.Contract{Fields: []pipepayload.ContractField{{Name: "id", Type: model.ContractFieldTypeValueInteger, Required: true, Validations: &pipepayload.ContractFieldValidations{Min: json.Number(literal), Max: json.Number(literal)}}}}
				statement := "SELECT id FROM input WHERE id = '" + literal + "'"
				require.NoError(t, ValidateOutputContractTypesWithInputSchema(statement, contract, model.DataSourceTypePostgres, map[string]model.ContractFieldType{"id": model.ContractFieldTypeValueInteger}))
				output, err := NewRuntime().Execute(Transform{SQL: statement, OutputKind: kind, InputContract: contract}, map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationJSON, Document: json.RawMessage(`[{"id":` + literal + `}]`)}})
				require.NoError(t, err)
				require.Equal(t, 1, output.RowCount)
				require.NoError(t, ValidateOutputContract(output, contract))
				switch kind {
				case model.TransformOutputKindUnknown, model.TransformOutputKindRows:
					require.Equal(t, json.Number(literal), output.Rows[0]["id"])
				case model.TransformOutputKindJSON:
					require.Equal(t, `[{"id":`+literal+`}]`, string(output.Document))
				case model.TransformOutputKindCSV:
					require.Equal(t, "id\n"+literal+"\n", output.CSV)
				}
			})
		}
	}
}

func TestExactIntegerExpressions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		sql     string
		rows    []map[string]any
		want    any
		wantErr string
	}{
		{sql: "SELECT id + 0 AS value FROM input", rows: []map[string]any{{"id": float64(1e100)}}, want: float64(1e100)},
		{sql: "SELECT SUM(id) AS value FROM input", rows: []map[string]any{{"id": float64(2)}, {"id": float64(3)}}, want: float64(5)},
		{sql: "SELECT MAX(id) AS value FROM input", rows: []map[string]any{{"id": float64(1e100)}}, want: float64(1e100)},
		{sql: "SELECT id AS value FROM input WHERE id = '1e400'", rows: []map[string]any{{"id": "1e400"}}, wantErr: "exponent exceeds supported range"},
		{sql: "SELECT id / 0 AS value FROM input", rows: []map[string]any{{"id": int64(1)}}, wantErr: "division by zero"},
		{sql: "SELECT id + 1 AS value FROM input", rows: []map[string]any{{"id": int64(9007199254740993)}}, want: int64(9007199254740994)},
		{sql: "SELECT id - 1 AS value FROM input", rows: []map[string]any{{"id": int64(-9007199254740993)}}, want: int64(-9007199254740994)},
		{sql: "SELECT id * 2 AS value FROM input", rows: []map[string]any{{"id": int64(9007199254740993)}}, want: int64(18014398509481986)},
		{sql: "SELECT id / 3 AS value FROM input", rows: []map[string]any{{"id": int64(9007199254740993)}}, want: int64(3002399751580331)},
		{sql: "SELECT CAST(id AS SIGNED) AS value FROM input", rows: []map[string]any{{"id": "9223372036854775807"}}, want: int64(math.MaxInt64)},
		{sql: "SELECT CAST(id AS UNSIGNED) AS value FROM input", rows: []map[string]any{{"id": "18446744073709551615"}}, want: uint64(math.MaxUint64)},
		{sql: "SELECT JSON_EXTRACT(doc, '$.id') AS value FROM input", rows: []map[string]any{{"doc": `{"id":9007199254740993}`}}, want: json.Number("9007199254740993")},
		{sql: "SELECT SUM(id) AS value FROM input", rows: []map[string]any{{"id": int64(9007199254740993)}, {"id": int64(2)}}, want: int64(9007199254740995)},
		{sql: "SELECT AVG(id) AS value FROM input", rows: []map[string]any{{"id": int64(9007199254740993)}, {"id": int64(9007199254740993)}}, want: int64(9007199254740993)},
		{sql: "SELECT MAX(id) AS value FROM input", rows: []map[string]any{{"id": "9007199254740992"}, {"id": "9007199254740993"}}, want: int64(9007199254740993)},
		{sql: "SELECT MIN(id) AS value FROM input", rows: []map[string]any{{"id": int64(-9007199254740992)}, {"id": int64(-9007199254740993)}}, want: int64(-9007199254740993)},
		{sql: "SELECT id AS value FROM input ORDER BY value DESC LIMIT 1", rows: []map[string]any{{"id": "9007199254740992"}, {"id": "9007199254740993"}}, want: "9007199254740993"},
		{sql: "SELECT id AS value FROM input WHERE id > 9007199254740992", rows: []map[string]any{{"id": int64(9007199254740993)}}, want: int64(9007199254740993)},
		{sql: "SELECT id AS value FROM input WHERE id != 9007199254740992", rows: []map[string]any{{"id": int64(9007199254740993)}}, want: int64(9007199254740993)},
		{sql: "SELECT id AS value FROM input", rows: []map[string]any{{"id": json.Number("18446744073709551616")}}, want: json.Number("18446744073709551616")},
		{sql: "SELECT id + 1 AS value FROM input", rows: []map[string]any{{"id": uint64(math.MaxUint64)}}, wantErr: "64-bit range"},
		{sql: "SELECT id - 1 AS value FROM input", rows: []map[string]any{{"id": int64(math.MinInt64)}}, wantErr: "64-bit range"},
		{sql: "SELECT CAST(id AS SIGNED) AS value FROM input", rows: []map[string]any{{"id": uint64(math.MaxUint64)}}, wantErr: "int64 range"},
		{sql: "SELECT CAST(id AS UNSIGNED) AS value FROM input", rows: []map[string]any{{"id": int64(-1)}}, wantErr: "uint64 range"},
		{sql: "SELECT CAST(id AS DOUBLE) AS value FROM input", rows: []map[string]any{{"id": int64(9007199254740993)}}, wantErr: "integer precision"},
		{sql: "SELECT id + fraction AS value FROM input", rows: []map[string]any{{"id": int64(9007199254740993), "fraction": 0.5}}, wantErr: "integer precision"},
		{sql: "SELECT id / 2 AS value FROM input", rows: []map[string]any{{"id": int64(9007199254740993)}}, wantErr: "integer precision"},
		{sql: "SELECT id AS value FROM input WHERE id = 1", rows: []map[string]any{{"id": json.Number("18446744073709551616")}}, wantErr: "64-bit range"},
		{sql: "SELECT id AS value FROM input WHERE id > 1", rows: []map[string]any{{"id": math.Inf(1)}}, wantErr: "finite"},
	} {
		t.Run(tc.sql+fmt.Sprint(tc.rows), func(t *testing.T) {
			_, validationErr := ValidateTransformSQL(tc.sql)
			require.NoError(t, validationErr)
			output, err := NewRuntime().Execute(Transform{SQL: tc.sql}, map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationRows, Rows: tc.rows}})
			if tc.wantErr != "" {
				require.ErrorIs(t, err, ErrNumericConversion)
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, []map[string]any{{"value": tc.want}}, output.Rows)
		})
	}
}
