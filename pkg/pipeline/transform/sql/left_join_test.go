package sql

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/model"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
)

func TestSQLLeftJoin(t *testing.T) {
	t.Parallel()
	schema := map[string]model.ContractFieldType{
		"settlements.ref":   model.ContractFieldTypeValueString,
		"settlements.gross": model.ContractFieldTypeValueInteger,
		"ledger.ref":        model.ContractFieldTypeValueString,
		"ledger.gross":      model.ContractFieldTypeValueInteger,
	}
	inputs := map[string]*pipepayload.Payload{
		"settlements": {Representation: model.ContractRepresentationJSON, Document: json.RawMessage(`[{"ref":"capture","gross":10000},{"ref":"refund","gross":-1500},{"ref":"unknown","gross":2500}]`)},
		"ledger":      {Representation: model.ContractRepresentationJSON, Document: json.RawMessage(`[{"ref":"capture","gross":10000},{"ref":"refund","gross":-2000}]`)},
	}
	join := " FROM settlements s LEFT OUTER JOIN ledger l ON s.ref = l.ref"
	for _, tc := range []struct{ name, statement, want string }{
		{"finance", "SELECT s.ref AS ref, l.gross AS expected, s.gross - l.gross AS difference" + join, `[{"ref":"capture","expected":10000,"difference":0},{"ref":"refund","expected":-2000,"difference":500},{"ref":"unknown","expected":null,"difference":null}]`},
		{"unqualified unique field", "SELECT ref AS ref FROM settlements LEFT JOIN ledger l ON settlements.ref = l.ref", ""},
		{"where null", "SELECT s.ref AS ref" + join + " WHERE l.ref IS NULL", `[{"ref":"unknown"}]`},
		{"where discards unknown", "SELECT s.ref AS ref" + join + " WHERE l.gross > 0", `[{"ref":"capture"}]`},
		{"on filter preserves left", "SELECT s.ref AS ref, l.gross AS expected" + join + " AND l.gross > 0", `[{"ref":"capture","expected":10000},{"ref":"refund","expected":null},{"ref":"unknown","expected":null}]`},
		{"aggregates", "SELECT COUNT(*) AS line_count, COUNT(l.ref) AS matches, SUM(l.gross) AS total" + join, `[{"line_count":3,"matches":2,"total":8000}]`},
		{"grouped", "SELECT s.ref AS ref, COUNT(l.ref) AS matches, SUM(l.gross) AS total" + join + " GROUP BY s.ref", `[{"ref":"capture","matches":1,"total":10000},{"ref":"refund","matches":1,"total":-2000},{"ref":"unknown","matches":0,"total":null}]`},
		{"empty cte", "WITH filtered AS (SELECT ref, gross FROM ledger WHERE gross > 10000) SELECT s.ref AS ref, l.gross AS expected FROM settlements s LEFT JOIN filtered l ON s.ref = l.ref", `[{"ref":"capture","expected":null},{"ref":"refund","expected":null},{"ref":"unknown","expected":null}]`},
		{"chained", "SELECT s.ref AS ref, r.gross AS expected" + join + " LEFT JOIN ledger r ON l.ref = r.ref", `[{"ref":"capture","expected":10000},{"ref":"refund","expected":-2000},{"ref":"unknown","expected":null}]`},
		{"right subtree", "SELECT s.ref AS ref, l.gross AS expected, r.gross AS other FROM settlements s LEFT JOIN (ledger l JOIN ledger r ON l.ref = r.ref AND l.gross > 0) ON s.ref = l.ref", `[{"ref":"capture","expected":10000,"other":10000},{"ref":"refund","expected":null,"other":null},{"ref":"unknown","expected":null,"other":null}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateTransformSQL(tc.statement)
			require.NoError(t, err)
			contract, err := InferOutputContractWithInputSchema(tc.statement, model.TransformOutputKindRows, model.DataSourceTypePostgres, schema)
			if tc.want == "" {
				require.ErrorIs(t, err, ErrTypeInput)
				_, err = NewRuntime().Execute(Transform{SQL: tc.statement}, inputs)
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NoError(t, ValidateOutputContractTypesWithInputSchema(tc.statement, contract, model.DataSourceTypePostgres, schema))
			for _, field := range contract.Fields {
				require.True(t, field.Nullable)
			}
			output, err := NewRuntime(qualificationLimits()).Execute(Transform{SQL: tc.statement}, inputs)
			require.NoError(t, err)
			require.NoError(t, ValidateOutputContract(output, contract))
			encoded, err := json.Marshal(output.Rows)
			require.NoError(t, err)
			require.JSONEq(t, tc.want, string(encoded))
			if tc.name == "finance" {
				for i := range contract.Fields {
					if contract.Fields[i].Name == "difference" {
						contract.Fields[i].Nullable = false
					}
				}
				require.ErrorIs(t, ValidateOutputContract(output, contract), ErrOutputContractViolation)
			}
		})
	}
}

func TestSQLLeftJoinEmptyRight(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		payload  *pipepayload.Payload
		declared bool
	}{
		{"row metadata", &pipepayload.Payload{Representation: model.ContractRepresentationRows, ColumnNames: []string{"ref", "gross"}}, false},
		{"csv header", &pipepayload.Payload{Representation: model.ContractRepresentationCSV, CSV: "ref,gross\n"}, false},
		{"declared rows", &pipepayload.Payload{Representation: model.ContractRepresentationRows}, true},
		{"declared json", &pipepayload.Payload{Representation: model.ContractRepresentationJSON, Document: json.RawMessage(`[]`)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transform := Transform{SQL: "SELECT s.id AS id, l.ref AS ref, s.gross - l.gross AS difference FROM settlements s LEFT JOIN ledger l ON s.id = l.ref"}
			if tc.declared {
				transform.InputContract = &pipepayload.Contract{Fields: []pipepayload.ContractField{
					{Name: "ledger.ref", Type: model.ContractFieldTypeValueString},
					{Name: "ledger.gross", Type: model.ContractFieldTypeValueInteger},
				}}
			}
			inputs := map[string]*pipepayload.Payload{
				"settlements": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"id": "missing", "gross": 2500}}},
				"ledger":      tc.payload,
			}
			for _, kind := range []model.TransformOutputKind{model.TransformOutputKindRows, model.TransformOutputKindJSON, model.TransformOutputKindCSV} {
				transform.OutputKind = kind
				output, err := NewRuntime(qualificationLimits()).Execute(transform, inputs)
				require.NoError(t, err)
				require.Equal(t, 1, output.RowCount)
				switch kind {
				case model.TransformOutputKindUnknown, model.TransformOutputKindRows:
					require.Equal(t, []map[string]any{{"id": "missing", "ref": nil, "difference": nil}}, output.Rows)
				case model.TransformOutputKindJSON:
					require.JSONEq(t, `[{"id":"missing","ref":null,"difference":null}]`, string(output.Document))
				case model.TransformOutputKindCSV:
					require.Equal(t, "id,ref,difference\nmissing,,\n", output.CSV)
				}
			}
			transform.OutputKind = model.TransformOutputKindRows
			transform.SQL = "SELECT COUNT(*) AS line_count, COUNT(l.ref) AS matches, SUM(l.gross) AS total FROM settlements s LEFT JOIN ledger l ON s.id = l.ref"
			output, err := NewRuntime().Execute(transform, inputs)
			require.NoError(t, err)
			encoded, err := json.Marshal(output.Rows)
			require.NoError(t, err)
			require.JSONEq(t, `[{"line_count":1,"matches":0,"total":null}]`, string(encoded))
			transform.SQL = "SELECT l.missing AS value FROM settlements s LEFT JOIN ledger l ON s.id = l.ref"
			_, err = NewRuntime().Execute(transform, inputs)
			require.ErrorIs(t, err, ErrRuntimeProjectionFieldNotFound)
		})
	}
}

func TestSQLLeftJoinMultiplicityNullKeysAndPrecision(t *testing.T) {
	t.Parallel()
	inputs := map[string]*pipepayload.Payload{
		"left_rows":  {Representation: model.ContractRepresentationJSON, Document: json.RawMessage(`[{"id":1,"amount":9007199254740993},{"id":null,"amount":7},{"id":2,"amount":9}]`)},
		"right_rows": {Representation: model.ContractRepresentationJSON, Document: json.RawMessage(`[{"id":1,"amount":9007199254740992},{"id":1,"amount":9007199254740991},{"id":null,"amount":0}]`)},
	}
	output, err := NewRuntime(qualificationLimits()).Execute(Transform{SQL: "SELECT l.id AS id, r.amount AS expected, l.amount - r.amount AS difference FROM left_rows l LEFT JOIN right_rows r ON l.id = r.id"}, inputs)
	require.NoError(t, err)
	encoded, err := json.Marshal(output.Rows)
	require.NoError(t, err)
	require.Equal(t, `[{"difference":1,"expected":9007199254740992,"id":1},{"difference":2,"expected":9007199254740991,"id":1},{"difference":null,"expected":null,"id":null},{"difference":null,"expected":null,"id":2}]`, string(encoded))
}

func TestSQLLeftJoinRejectUnsupportedForms(t *testing.T) {
	t.Parallel()
	inputs := map[string]*pipepayload.Payload{"left_rows": budgetRowsPayload("id", 1), "right_rows": budgetRowsPayload("id", 1)}
	schema := map[string]model.ContractFieldType{"left_rows.id": model.ContractFieldTypeValueInteger, "right_rows.id": model.ContractFieldTypeValueInteger}
	contract := &pipepayload.Contract{Fields: []pipepayload.ContractField{{Name: "id", Type: model.ContractFieldTypeValueInteger}}}
	for _, join := range []string{"RIGHT JOIN right_rows ON left_rows.id = right_rows.id", "FULL OUTER JOIN right_rows ON left_rows.id = right_rows.id", "FULL JOIN right_rows ON left_rows.id = right_rows.id", "FULL /* comment */ JOIN right_rows ON left_rows.id = right_rows.id", "LEFT JOIN right_rows USING (id)", "NATURAL LEFT JOIN right_rows", "LEFT JOIN right_rows"} {
		statement := "SELECT left_rows.id AS id FROM left_rows " + join
		_, err := ValidateTransformSQL(statement)
		require.Error(t, err, statement)
		require.Error(t, ValidateOutputContractTypesWithInputSchema(statement, contract, model.DataSourceTypePostgres, schema), statement)
		_, err = NewRuntime().Execute(Transform{SQL: statement}, inputs)
		require.Error(t, err, statement)
	}
}

func TestSQLLeftJoinBudgets(t *testing.T) {
	t.Parallel()
	transform := Transform{SQL: "SELECT l.id AS id, r.id AS other FROM left_rows l LEFT JOIN right_rows r ON l.id = r.id LIMIT 1"}
	inputs := map[string]*pipepayload.Payload{"left_rows": budgetRowsPayload("id", 1, 2), "right_rows": {Representation: model.ContractRepresentationRows, ColumnNames: []string{"id"}}}
	var usage Usage
	_, err := NewRuntime().WithObserver(func(u Usage, _ error) { usage = u }).Execute(transform, inputs)
	require.NoError(t, err)
	require.Equal(t, int64(2), usage.Work)
	for _, tc := range []struct {
		limits Limits
		want   error
	}{
		{Limits{MaxWork: 1}, ErrWorkBudgetExceeded},
		{Limits{MaxIntermediateRows: int(usage.IntermediateRows) - 1}, ErrIntermediateRowsExceeded},
		{Limits{MaxIntermediateBytes: usage.IntermediateBytes - 1}, ErrIntermediateBytesExceeded},
		{Limits{MaxDepth: 1}, ErrDepthBudgetExceeded},
	} {
		_, err := NewRuntime(tc.limits).Execute(transform, inputs)
		require.ErrorIs(t, err, tc.want)
	}
	inputs["right_rows"] = budgetRowsPayload("id", 3, 4)
	_, err = NewRuntime(Limits{MaxWork: 3}).Execute(transform, inputs)
	require.ErrorIs(t, err, ErrWorkBudgetExceeded)
	transform.SQL = strings.TrimSuffix(transform.SQL, " LIMIT 1")
	_, err = NewRuntime(Limits{MaxOutputRows: 1}).Execute(transform, inputs)
	require.ErrorIs(t, err, ErrOutputRowsExceeded)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = NewRuntime().ExecuteContext(ctx, transform, inputs)
	require.ErrorIs(t, err, ErrExecutionBudgetInterrupted)
}

func TestSQLJoinFullAlias(t *testing.T) {
	t.Parallel()
	for _, alias := range []string{"AS full", "`full`"} {
		statement := "SELECT full.id AS id FROM left_rows " + alias + " JOIN right_rows r ON full.id = r.id"
		_, err := ValidateTransformSQL(statement)
		require.NoError(t, err)
		output, err := NewRuntime().Execute(Transform{SQL: statement}, map[string]*pipepayload.Payload{"left_rows": budgetRowsPayload("id", 1), "right_rows": budgetRowsPayload("id", 1)})
		require.NoError(t, err)
		require.Len(t, output.Rows, 1)
	}
	for _, statement := range []string{"SELECT 'FULL JOIN' AS label FROM input", "SELECT `my full join field` AS v FROM input", "SELECT `my `` full join field` AS v FROM input"} {
		_, err := ValidateTransformSQL(statement)
		require.NoError(t, err)
	}
	statement := "SELECT full.id AS id FROM full JOIN right_rows r ON full.id = r.id"
	output, err := NewRuntime().Execute(Transform{SQL: statement}, map[string]*pipepayload.Payload{"full": budgetRowsPayload("id", 1), "right_rows": budgetRowsPayload("id", 1)})
	require.NoError(t, err)
	require.Len(t, output.Rows, 1)
}
