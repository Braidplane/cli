package sql

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Braidplane/cli/pkg/model"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
	"github.com/stretchr/testify/require"
)

func TestUnionAllTenInputsPreservesDuplicatesAndEmptySuccess(t *testing.T) {
	inputs := make(map[string]*pipepayload.Payload)
	contracts := make(map[string]*pipepayload.Contract)
	schema := make(map[string]model.ContractFieldType)
	branches := make([]string, 10)
	for i := range branches {
		name := fmt.Sprintf("supplier_%d", i)
		branches[i] = "SELECT id FROM " + name
		inputs[name] = &pipepayload.Payload{Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"id": int64(1)}, {"id": int64(1)}, {"id": int64(2)}}, ColumnNames: []string{"id"}, RowCount: 3}
		contracts[name] = &pipepayload.Contract{Fields: []pipepayload.ContractField{{Name: "id", Type: model.ContractFieldTypeValueInteger, Required: true}}}
		schema[name+".id"] = model.ContractFieldTypeValueInteger
	}
	statement := strings.Join(branches, " UNION ALL ")
	for _, sql := range []string{statement, "WITH combined AS (" + statement + ") SELECT id FROM combined ORDER BY id"} {
		t.Run(sql, func(t *testing.T) {
			_, err := ValidateTransformSQL(sql)
			require.NoError(t, err)
			inferred, err := InferOutputContractWithInputSchema(sql, model.TransformOutputKindRows, model.DataSourceTypeS3, schema)
			require.NoError(t, err)
			transform := Transform{SQL: sql, InputContracts: contracts}
			out, err := NewRuntime(qualificationLimits()).Execute(transform, inputs)
			require.NoError(t, err)
			require.Len(t, out.Rows, 30)
			require.NoError(t, ValidateOutputContract(out, inferred))
			original := inputs["supplier_4"]
			inputs["supplier_4"] = &pipepayload.Payload{Representation: model.ContractRepresentationRows, ColumnNames: []string{"id"}}
			out, err = NewRuntime(qualificationLimits()).Execute(transform, inputs)
			require.NoError(t, err)
			require.Len(t, out.Rows, 27)
			inputs["supplier_4"] = nil
			out, err = NewRuntime().Execute(transform, inputs)
			require.Error(t, err)
			require.Nil(t, out)
			delete(inputs, "supplier_4")
			_, err = NewRuntime().Execute(transform, inputs)
			require.Error(t, err)
			inputs["supplier_4"] = original
		})
	}
}

func TestUnionAllOrderingCTEsAndBudgets(t *testing.T) {
	inputs := map[string]*pipepayload.Payload{
		"a": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"id": int64(2)}}},
		"b": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"id": int64(1)}}},
	}
	for _, statement := range []string{
		"SELECT id FROM a UNION ALL SELECT id FROM b ORDER BY id LIMIT 1",
		"WITH first AS (SELECT id FROM a), combined AS (SELECT id FROM first UNION ALL SELECT id FROM b) SELECT id FROM combined ORDER BY id LIMIT 1",
		"WITH first AS (SELECT id FROM a) SELECT id FROM first UNION ALL SELECT id FROM b ORDER BY id LIMIT 1",
	} {
		out, err := NewRuntime().Execute(Transform{SQL: statement}, inputs)
		require.NoError(t, err)
		require.Equal(t, []map[string]any{{"id": int64(1)}}, out.Rows)
	}
	transform := Transform{SQL: "SELECT id FROM a UNION ALL SELECT id FROM b"}
	for _, limit := range []Limits{{MaxWork: 1}, {MaxIntermediateRows: 3}, {MaxIntermediateBytes: 1}, {MaxOutputRows: 1}, {MaxDepth: 1}} {
		_, err := NewRuntime(limit).Execute(transform, inputs)
		require.Error(t, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewRuntime().ExecuteContext(ctx, transform, inputs)
	require.ErrorIs(t, err, ErrExecutionBudgetInterrupted)
}

func TestUnionAllRejectsMismatchedShapesAndUnsupportedOperations(t *testing.T) {
	for _, sql := range []string{
		"SELECT id FROM a UNION SELECT id FROM b",
		"SELECT id FROM a INTERSECT SELECT id FROM b",
		"SELECT id FROM a EXCEPT SELECT id FROM b",
		"SELECT id FROM a UNION ALL SELECT id AS other FROM b",
		"SELECT id FROM a UNION ALL SELECT id, other FROM b",
		"SELECT id FROM a UNION ALL (SELECT id FROM b LIMIT 1)",
		"WITH a AS (SELECT id FROM a UNION ALL SELECT id FROM b) SELECT id FROM a",
		"WITH a AS (SELECT id FROM later UNION ALL SELECT id FROM b), later AS (SELECT id FROM b) SELECT id FROM a",
	} {
		_, err := ValidateTransformSQL(sql)
		require.Error(t, err, sql)
	}
	_, err := InferOutputContractWithInputSchema("SELECT id FROM a UNION ALL SELECT id FROM b", model.TransformOutputKindRows, model.DataSourceTypeS3, map[string]model.ContractFieldType{"a.id": model.ContractFieldTypeValueString, "b.id": model.ContractFieldTypeValueInteger})
	require.ErrorContains(t, err, `field "id"`)
	_, err = InferOutputContractWithInputSchema("WITH x AS (SELECT CAST(id AS DECIMAL(10,2)) AS price FROM a), y AS (SELECT CAST(id AS DECIMAL(10,3)) AS price FROM b) SELECT price FROM x UNION ALL SELECT price FROM y", model.TransformOutputKindRows, model.DataSourceTypeS3, map[string]model.ContractFieldType{"a.id": model.ContractFieldTypeValueInteger, "b.id": model.ContractFieldTypeValueInteger})
	require.ErrorContains(t, err, "precision/scale")
}

func TestUnionAllKnownContractsAndRuntimeTypes(t *testing.T) {
	statement := "SELECT price FROM a UNION ALL SELECT price FROM b"
	for _, change := range []func(*pipepayload.ContractField){
		func(f *pipepayload.ContractField) {
			f.Type, f.Precision, f.Scale = model.ContractFieldTypeValueString, nil, nil
		},
		func(f *pipepayload.ContractField) { p := 12; f.Precision = &p },
		func(f *pipepayload.ContractField) { s := 3; f.Scale = &s },
		func(f *pipepayload.ContractField) { f.Nullable = false },
		func(f *pipepayload.ContractField) { f.Required = false },
	} {
		contracts := map[string]*pipepayload.Contract{"a": decimalContract("price", 10, 2), "b": decimalContract("price", 10, 2)}
		require.NoError(t, ValidateTransformInputContracts(statement, contracts))
		change(&contracts["b"].Fields[0])
		require.ErrorContains(t, ValidateTransformInputContracts(statement, contracts), `input "b" field "price"`)
		_, err := NewRuntime().Execute(Transform{SQL: statement, InputContracts: contracts}, map[string]*pipepayload.Payload{"a": {Representation: model.ContractRepresentationRows}, "b": {Representation: model.ContractRepresentationRows}})
		require.ErrorContains(t, err, `input "b" field "price"`)
	}
	ctes := "WITH x AS (SELECT CAST(id AS DECIMAL(10,2)) AS price FROM a), y AS (SELECT CAST(id AS DECIMAL(10,3)) AS price FROM b) SELECT price FROM x UNION ALL SELECT price FROM y"
	contracts := map[string]*pipepayload.Contract{"a": {Fields: []pipepayload.ContractField{{Name: "id", Type: model.ContractFieldTypeValueInteger}}}, "b": {Fields: []pipepayload.ContractField{{Name: "id", Type: model.ContractFieldTypeValueInteger}}}}
	require.ErrorContains(t, ValidateTransformInputContracts(ctes, contracts), "precision/scale")
	_, err := NewRuntime().Execute(Transform{SQL: statement}, map[string]*pipepayload.Payload{"a": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"price": 1}}}, "b": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"price": "1"}}}})
	require.ErrorContains(t, err, `input "b" field "price"`)
}

func TestUnionAllEmptySerializationAndCombinedUniquenessScope(t *testing.T) {
	inputs := map[string]*pipepayload.Payload{"a": {Representation: model.ContractRepresentationRows}, "b": {Representation: model.ContractRepresentationRows}}
	out, err := NewRuntime().Execute(Transform{SQL: "SELECT product FROM a UNION ALL SELECT product FROM b", OutputKind: model.TransformOutputKindJSON}, inputs)
	require.NoError(t, err)
	require.JSONEq(t, `[]`, string(out.Document))
	unique := true
	contract := &pipepayload.Contract{Fields: []pipepayload.ContractField{{Name: "provider", Type: model.ContractFieldTypeValueString, Required: true}, {Name: "product", Type: model.ContractFieldTypeValueString, Required: true, Validations: &pipepayload.ContractFieldValidations{Unique: &unique}}}}
	for _, name := range []string{"a", "b"} {
		inputs[name].Rows = []map[string]any{{"provider": name, "product": "shared-product"}}
	}
	statement := "SELECT provider, product FROM a UNION ALL SELECT provider, product FROM b"
	transform := Transform{SQL: statement, InputContracts: map[string]*pipepayload.Contract{"a": contract, "b": contract}, OutputKind: model.TransformOutputKindRows}
	out, err = NewRuntime().Execute(transform, inputs)
	require.NoError(t, err)
	require.Len(t, out.Rows, 2, "individually unique product keys may overlap across providers")
	require.Error(t, ValidateOutputContract(out, contract), "combined product uniqueness is a separate output obligation")
	transform.SQL = "WITH combined AS (" + statement + ") SELECT provider, product, COUNT(*) AS n FROM combined GROUP BY provider, product HAVING n > 1"
	out, err = NewRuntime().Execute(transform, inputs)
	require.NoError(t, err)
	require.Empty(t, out.Rows)
	inputs["b"].Rows[0]["provider"] = "a"
	out, err = NewRuntime().Execute(transform, inputs)
	require.NoError(t, err)
	require.Equal(t, []map[string]any{{"provider": "a", "product": "shared-product", "n": int64(2)}}, out.Rows)
}

func TestUnionAllInferenceKeepsNullabilityAcrossEveryBranch(t *testing.T) {
	statement := "WITH ranked AS (SELECT ROW_NUMBER() OVER (ORDER BY id) AS rn FROM a) SELECT rn FROM ranked UNION ALL SELECT rn FROM b"
	contract, err := InferOutputContractWithInputSchema(statement, model.TransformOutputKindRows, model.DataSourceTypeS3, map[string]model.ContractFieldType{"a.id": model.ContractFieldTypeValueInteger, "b.rn": model.ContractFieldTypeValueInteger})
	require.NoError(t, err)
	require.True(t, contract.Fields[0].Nullable)
	_, err = NewRuntime().Execute(Transform{SQL: "SELECT id FROM a UNION ALL SELECT id FROM b"}, map[string]*pipepayload.Payload{"a": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"id": struct{}{}}}}, "b": {Representation: model.ContractRepresentationRows}})
	require.ErrorContains(t, err, "unsupported runtime type")
}
