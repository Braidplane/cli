package sql

import (
	"context"
	"testing"

	"github.com/Braidplane/cli/pkg/model"
	"github.com/Braidplane/cli/pkg/pipeline/payload"
	"github.com/stretchr/testify/require"
)

func TestNamedInputContractsValidateEachRelationBeforeSQL(t *testing.T) {
	contract := func(name string) *payload.Contract {
		return &payload.Contract{Representation: model.ContractRepresentationRows, Fields: []payload.ContractField{{Name: name, Type: model.ContractFieldTypeValueInteger, Required: true}}}
	}
	transform := Transform{SQL: "SELECT orders.id FROM orders JOIN accounts ON orders.id = accounts.id", OutputKind: model.TransformOutputKindRows, InputContracts: map[string]*payload.Contract{"orders": contract("amount"), "accounts": contract("id")}}
	inputs := map[string]*payload.Payload{
		"orders":   {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"id": int64(1), "amount": int64(20)}}, ColumnNames: []string{"id", "amount"}, RowCount: 1},
		"accounts": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"id": int64(1)}}, ColumnNames: []string{"id"}, RowCount: 1},
	}
	runtime := NewRuntime(Limits{MaxWork: 1000, MaxIntermediateRows: 20, MaxIntermediateBytes: 10000, MaxOutputRows: 20, MaxOutputBytes: 10000, MaxDepth: 20})
	output, err := runtime.ExecuteContext(context.Background(), transform, inputs)
	require.NoError(t, err)
	require.Len(t, output.Rows, 1)
	delete(inputs["orders"].Rows[0], "amount")
	_, err = runtime.ExecuteContext(context.Background(), transform, inputs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "orders")
	inputs["orders"].Rows[0]["amount"] = int64(20)
	inputs["accounts"].Rows[0]["id"] = "renamed"
	_, err = runtime.ExecuteContext(context.Background(), transform, inputs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "accounts")
	inputs["accounts"].Rows[0]["id"] = int64(1)
	transform.InputContracts["orders"].Representation = model.ContractRepresentationJSON
	_, err = runtime.ExecuteContext(context.Background(), transform, inputs)
	require.Error(t, err)
	transform.InputContracts["orders"].Representation = model.ContractRepresentationRows
	transform.InputContracts["unknown"] = contract("id")
	_, err = runtime.ExecuteContext(context.Background(), transform, inputs)
	require.Error(t, err)
	delete(transform.InputContracts, "unknown")
	transform.InputContract = contract("id")
	_, err = runtime.ExecuteContext(context.Background(), transform, inputs)
	require.Error(t, err)
	transform.InputContract = nil
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = runtime.ExecuteContext(cancelled, transform, inputs)
	require.Error(t, err)
	transform.InputContracts = map[string]*payload.Contract{}
	_, err = runtime.ExecuteContext(context.Background(), transform, inputs)
	require.ErrorContains(t, err, "input_contracts must contain at least one entry")
}
