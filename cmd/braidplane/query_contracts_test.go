package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSavedQueryContractFileMapsCreateAndReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contracts.json")
	content := []byte(`{"output_contract_ref":{"contract_slug":"orders","revision_slug":"v1","schema_object_id":"row","representation":"rows"},"expected_input_contract":{"name":"rows","fields":[{"name":"id","type":"integer","required":true}]}}`)
	require.NoError(t, os.WriteFile(path, content, 0o600))
	flags := queryPayloadFlags{Name: "Orders", StatementJSON: `{"type":"postgres","sql":"SELECT 1"}`, ContractsFile: path}
	create, err := (&createQueryCommand{queryPayloadFlags: flags}).newQuery()
	require.NoError(t, err)
	require.NotNil(t, create.ExpectedInputContract)
	require.NotNil(t, create.OutputContractRef)
	update, err := (&updateQueryCommand{queryPayloadFlags: flags}).updateQuery()
	require.NoError(t, err)
	require.Equal(t, create.OutputContractRef, update.OutputContractRef)
	flags.ContractsFile = ""
	update, err = (&updateQueryCommand{queryPayloadFlags: flags}).updateQuery()
	require.NoError(t, err)
	require.Nil(t, update.OutputContractRef)
	require.Nil(t, update.ExpectedInputContract)
	for _, invalid := range []string{`null`, `{"unknown":true}`, `{} {}`, `{"output_contract":{"name":"rows"},"output_contract_ref":{"contract_slug":"orders","revision_slug":"v1","schema_object_id":"row"}}`} {
		require.NoError(t, os.WriteFile(path, []byte(invalid), 0o600))
		flags.ContractsFile = path
		_, err = flags.contracts()
		require.Error(t, err)
	}
}
