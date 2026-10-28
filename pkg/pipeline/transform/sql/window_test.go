package sql

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/Braidplane/cli/pkg/model"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
	"github.com/stretchr/testify/require"
)

func TestRowNumberLatestAndExactPriceRanking(t *testing.T) {
	inputs := map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationJSON, Document: json.RawMessage(`[
		{"provider":"a","product":"x","effective_at":"2026-10-19","record_id":1,"price":9007199254740993.20},
		{"provider":"a","product":"x","effective_at":"2026-10-19","record_id":2,"price":9007199254740993.10},
		{"provider":"b","product":"x","effective_at":null,"record_id":3,"price":9007199254740993.30},
		{"provider":"a","product":null,"effective_at":"2026-10-18","record_id":4,"price":null}
	]`)}}
	schema := map[string]model.ContractFieldType{"provider": model.ContractFieldTypeValueString, "product": model.ContractFieldTypeValueString, "effective_at": model.ContractFieldTypeValueString, "record_id": model.ContractFieldTypeValueInteger, "price": model.ContractFieldTypeValueDecimal}
	for _, tc := range []struct{ statement, want string }{
		{"WITH ranked AS (SELECT provider, product, record_id, ROW_NUMBER() OVER (PARTITION BY provider, product ORDER BY effective_at DESC, record_id DESC) AS rn FROM input) SELECT record_id FROM ranked WHERE rn = 1 ORDER BY record_id", `[{"record_id":2},{"record_id":3},{"record_id":4}]`},
		{"SELECT record_id, ROW_NUMBER() OVER (PARTITION BY product ORDER BY price, record_id) AS rn FROM input ORDER BY record_id", `[{"record_id":1,"rn":2},{"record_id":2,"rn":1},{"record_id":3,"rn":3},{"record_id":4,"rn":1}]`},
		{"SELECT record_id, ROW_NUMBER() OVER (ORDER BY effective_at, record_id) AS rn FROM input ORDER BY rn", `[{"record_id":4,"rn":1},{"record_id":1,"rn":2},{"record_id":2,"rn":3},{"record_id":3,"rn":4}]`},
		{"SELECT record_id, ROW_NUMBER() OVER (ORDER BY effective_at DESC, record_id) AS rn FROM input ORDER BY rn LIMIT 1", `[{"record_id":3,"rn":1}]`},
		{"SELECT ROW_NUMBER() OVER (ORDER BY record_id) AS rn FROM input WHERE record_id > 9", `[]`},
	} {
		t.Run(tc.statement, func(t *testing.T) {
			contract, err := InferOutputContractWithInputSchema(tc.statement, model.TransformOutputKindRows, model.DataSourceTypeS3, schema)
			require.NoError(t, err)
			for range 3 {
				out, err := NewRuntime(qualificationLimits()).Execute(Transform{SQL: tc.statement, InputContract: decimalContract("price", 38, 2)}, inputs)
				require.NoError(t, err)
				require.NoError(t, ValidateOutputContract(out, contract))
				encoded, err := json.Marshal(out.Rows)
				require.NoError(t, err)
				require.JSONEq(t, tc.want, string(encoded))
				var records []json.RawMessage
				require.NoError(t, json.Unmarshal(inputs["input"].Document, &records))
				slices.Reverse(records)
				inputs["input"].Document, err = json.Marshal(records)
				require.NoError(t, err)
			}
		})
	}
}

func TestRowNumberRejectsUnsupportedSyntaxAndAmbiguousTies(t *testing.T) {
	for _, statement := range []string{
		"SELECT ROW_NUMBER() OVER () AS rn FROM input",
		"SELECT RANK() OVER (ORDER BY id) AS rn FROM input",
		"SELECT ROW_NUMBER() OVER (ORDER BY id ROWS UNBOUNDED PRECEDING) AS rn FROM input",
		"SELECT ROW_NUMBER() OVER w AS rn FROM input WINDOW w AS (ORDER BY id)",
		"SELECT ROW_NUMBER() OVER (ORDER BY id + 1) AS rn FROM input",
		"SELECT ROW_NUMBER() OVER (PARTITION BY id + 1 ORDER BY id) AS rn FROM input",
		"SELECT ROW_NUMBER() OVER (ORDER BY id) + 1 AS rn FROM input",
		"SELECT id FROM input WHERE ROW_NUMBER() OVER (ORDER BY id) = 1",
		"SELECT ROW_NUMBER() OVER (ORDER BY id) AS rn FROM input GROUP BY id",
	} {
		_, err := ValidateTransformSQL(statement)
		require.Error(t, err, statement)
	}
	transform := Transform{SQL: "SELECT ROW_NUMBER() OVER (PARTITION BY product ORDER BY stamp) AS rn FROM input"}
	_, err := NewRuntime().Execute(transform, map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"product": "x", "stamp": "same"}, {"product": "x", "stamp": "same"}}}})
	require.ErrorContains(t, err, "stable unique tie-breaker")
	_, err = InferOutputContractWithInputSchema("SELECT ROW_NUMBER() OVER (ORDER BY doc) AS rn FROM input", model.TransformOutputKindRows, model.DataSourceTypeS3, map[string]model.ContractFieldType{"doc": model.ContractFieldTypeValueObject})
	require.ErrorContains(t, err, "uncertain")
}

func TestRowNumberRuntimeKeyTypesBudgetsAndCancellation(t *testing.T) {
	transform := Transform{SQL: "SELECT id, ROW_NUMBER() OVER (PARTITION BY product ORDER BY stamp, id) AS rn FROM input"}
	inputs := map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"product": nil, "id": 1, "stamp": time.Unix(2, 0)}, {"product": nil, "id": 2, "stamp": time.Unix(1, 0)}}}}
	out, err := NewRuntime().Execute(transform, inputs)
	require.NoError(t, err)
	require.Equal(t, int64(2), out.Rows[0]["rn"])
	for _, limits := range []Limits{{MaxWork: 1}, {MaxIntermediateRows: 1}, {MaxIntermediateBytes: 1}} {
		_, err := NewRuntime(limits).Execute(transform, inputs)
		require.Error(t, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = NewRuntime().ExecuteContext(ctx, transform, inputs)
	require.ErrorIs(t, err, ErrExecutionBudgetInterrupted)
	for _, key := range []string{"product", "stamp"} {
		inputs["input"].Rows[0][key] = map[string]any{"unsupported": true}
		_, err := NewRuntime().Execute(transform, inputs)
		require.Error(t, err)
		inputs["input"].Rows[0][key] = nil
	}
}

func TestRowNumberSortBudgetAndCancellationAfterInputPreparation(t *testing.T) {
	stmt, _, err := parseTransformSelect("SELECT id, ROW_NUMBER() OVER (PARTITION BY product ORDER BY price, id) AS rn FROM input")
	require.NoError(t, err)
	rows := []map[string]any{{"product": "x", "price": 2, "id": 1}, {"product": "x", "price": 1, "id": 2}, {"product": "x", "price": 3, "id": 3}}
	_, err = evaluateRowNumbers(stmt, rows, newExecutionBudget(context.Background(), Limits{MaxWork: 3}))
	require.ErrorIs(t, err, ErrWorkBudgetExceeded)
	_, err = evaluateRowNumbers(stmt, rows, newExecutionBudget(newInterruptAfterChecksContext(25), Limits{}))
	require.ErrorIs(t, err, ErrExecutionBudgetInterrupted)
	_, err = evaluateRowNumbers(stmt, rows, newExecutionBudget(context.Background(), Limits{MaxIntermediateBytes: 48}))
	require.ErrorIs(t, err, ErrIntermediateBytesExceeded)
}

func TestRowNumberRejectsKnownUnsupportedKeysOnEmptyInputs(t *testing.T) {
	statement := "SELECT ROW_NUMBER() OVER (ORDER BY doc) AS rn FROM input"
	contract := &pipepayload.Contract{Fields: []pipepayload.ContractField{{Name: "doc", Type: model.ContractFieldTypeValueObject}}}
	require.ErrorContains(t, ValidateTransformInputContracts(statement, map[string]*pipepayload.Contract{"input": contract}), "window key type object")
	_, err := NewRuntime().Execute(Transform{SQL: statement, InputContracts: map[string]*pipepayload.Contract{"input": contract}}, map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationRows}})
	require.ErrorContains(t, err, "window key type object")
}
