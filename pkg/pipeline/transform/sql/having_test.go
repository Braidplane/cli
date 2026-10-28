package sql

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Braidplane/cli/pkg/model"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
	"github.com/stretchr/testify/require"
)

func TestHavingGroupsAliasesNullsAndExactDecimals(t *testing.T) {
	inputs := map[string]*pipepayload.Payload{"input": {
		Representation: model.ContractRepresentationJSON,
		Document:       json.RawMessage(`[{"product":"a","price":0.10},{"product":"a","price":0.30},{"product":"b","price":0.20},{"product":null,"price":null}]`),
	}}
	schema := map[string]model.ContractFieldType{"product": model.ContractFieldTypeValueString, "price": model.ContractFieldTypeValueDecimal}
	for _, tc := range []struct{ statement, want string }{
		{"SELECT product, COUNT(*) AS offers FROM input GROUP BY product HAVING COUNT(*) < 2 ORDER BY product LIMIT 1", `[{"offers":1,"product":"b"}]`},
		{"SELECT product, COUNT(*) AS offers FROM input GROUP BY product HAVING offers < 2 ORDER BY product", `[{"offers":1,"product":"b"},{"offers":1,"product":null}]`},
		{"SELECT product FROM input GROUP BY product HAVING MAX(price) - MIN(price) > 0.19", `[{"product":"a"}]`},
		{"SELECT product FROM input GROUP BY product HAVING (MAX(price) - MIN(price)) > 0.19", `[{"product":"a"}]`},
		{"SELECT product, CAST(MAX(price) - MIN(price) AS DECIMAL(38,2)) AS spread FROM input GROUP BY product HAVING spread > 0.19", `[{"product":"a","spread":0.20}]`},
		{"SELECT input.product FROM input GROUP BY product HAVING COUNT(*) > 1", `[{"product":"a"}]`},
		{"SELECT product FROM input GROUP BY product HAVING MAX(price) - MIN(price) > 0.20", `[]`},
		{"SELECT product FROM input GROUP BY product HAVING MAX(price) IS NULL", `[{"product":null}]`},
		{"SELECT product FROM input GROUP BY product HAVING NOT (MAX(price) > 0.25) AND COUNT(*) = 1", `[{"product":"b"}]`},
		{"SELECT COUNT(*) AS offers FROM input WHERE price IS NULL HAVING offers = 1", `[{"offers":1}]`},
		{"SELECT COUNT(*) AS offers FROM input WHERE product = 'missing' HAVING COUNT(*) = 0", `[{"offers":0}]`},
		{"SELECT CAST(MAX(price) AS DECIMAL(38,2)) AS highest FROM input WHERE product = 'missing' HAVING highest IS NULL", `[{"highest":null}]`},
		{"SELECT product, COUNT(*) AS offers FROM input WHERE product = 'missing' GROUP BY product HAVING COUNT(*) = 0", `[]`},
		{"WITH counts AS (SELECT product, COUNT(*) AS offers FROM input GROUP BY product HAVING offers > 1) SELECT product FROM counts", `[{"product":"a"}]`},
	} {
		t.Run(tc.statement, func(t *testing.T) {
			_, err := ValidateTransformSQL(tc.statement)
			require.NoError(t, err)
			contract, err := InferOutputContractWithInputSchema(tc.statement, model.TransformOutputKindRows, model.DataSourceTypeS3, schema)
			require.NoError(t, err)
			out, err := NewRuntime(qualificationLimits()).Execute(Transform{SQL: tc.statement, InputContract: decimalContract("price", 38, 2)}, inputs)
			require.NoError(t, err)
			require.NoError(t, ValidateOutputContract(out, contract))
			encoded, err := json.Marshal(out.Rows)
			require.NoError(t, err)
			require.JSONEq(t, tc.want, string(encoded))
		})
	}
}

func TestHavingRejectsUngroupedAndUnknownReferences(t *testing.T) {
	for _, statement := range []string{
		"SELECT product FROM input GROUP BY product HAVING price > 1",
		"SELECT product, COUNT(*) AS n FROM input HAVING n > 1",
		"SELECT COUNT(*) AS n FROM input HAVING price > 1",
	} {
		_, err := ValidateTransformSQL(statement)
		require.ErrorContains(t, err, "must be grouped")
	}
	_, err := InferOutputContractWithInputSchema("SELECT COUNT(*) AS n FROM input HAVING MAX(missing) > 1", model.TransformOutputKindRows, model.DataSourceTypeS3, map[string]model.ContractFieldType{"price": model.ContractFieldTypeValueDecimal})
	require.ErrorContains(t, err, "missing")
}

func TestHavingBudgetsAndCancellation(t *testing.T) {
	inputs := map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"id": 1}, {"id": 2}}}}
	transform := Transform{SQL: "SELECT COUNT(*) AS n FROM input HAVING MAX(id) - MIN(id) > 0"}
	_, err := NewRuntime(Limits{MaxWork: 1}).Execute(transform, inputs)
	require.ErrorIs(t, err, ErrWorkBudgetExceeded)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = NewRuntime().ExecuteContext(ctx, transform, inputs)
	require.ErrorIs(t, err, ErrExecutionBudgetInterrupted)
}
