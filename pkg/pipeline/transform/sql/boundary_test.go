package sql_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/model"
	filepayload "github.com/Braidplane/cli/pkg/payload/file"
	formpayload "github.com/Braidplane/cli/pkg/payload/form"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
	transformsql "github.com/Braidplane/cli/pkg/pipeline/transform/sql"
	"github.com/Braidplane/cli/pkg/xerror"
)

func TestBoundary_ValidatesAndExecutesRowsWithNeutralTypes(t *testing.T) {
	t.Parallel()

	outputContract := &pipepayload.Contract{
		Fields: []pipepayload.ContractField{{
			Name:     "user_id",
			Type:     model.ContractFieldTypeValueInteger,
			Required: true,
		}},
	}

	err := transformsql.ValidateOutputContractStructure(
		"SELECT id AS user_id FROM input",
		outputContract,
	)
	require.NoError(t, err)

	err = transformsql.ValidateOutputContractTypesWithInputSchema(
		"SELECT id AS user_id FROM input",
		outputContract,
		model.DataSourceTypePostgres,
		map[string]model.ContractFieldType{
			"id": model.ContractFieldTypeValueInteger,
		},
	)
	require.NoError(t, err)

	runtime := transformsql.NewRuntime()
	output, err := runtime.Execute(
		transformsql.Transform{
			SQL:            "SELECT id AS user_id FROM input",
			OutputKind:     model.TransformOutputKindRows,
			OutputContract: outputContract,
		},
		map[string]*pipepayload.Payload{
			"input": {
				Representation: model.ContractRepresentationRows,
				Rows:           []map[string]any{{"id": 42}},
				ColumnNames:    []string{"id"},
				RowCount:       1,
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, model.TransformOutputKindRows, output.OutputKind)
	require.Equal(t, []string{"user_id"}, output.ColumnNames)
	require.Equal(t, []map[string]any{{"user_id": 42}}, output.Rows)

	err = transformsql.ValidateOutputContract(output, outputContract)
	require.NoError(t, err)
}

func TestBoundary_SQLV1RejectsOpaqueFileAndMultipartFormRuntimeInputs(t *testing.T) {
	t.Parallel()

	fileMetadata := filepayload.NewFilePayloadMetadata(
		"users.pdf",
		"application/pdf",
		[]byte("%PDF"),
	)

	tests := []struct {
		name    string
		payload *pipepayload.Payload
	}{
		{
			name: "RejectsOpaqueFilePayload",
			payload: &pipepayload.Payload{
				Representation: model.ContractRepresentationFile,
				File:           &fileMetadata,
				FileBytes:      []byte("%PDF"),
			},
		},
		{
			name: "RejectsMultipartFormPayload",
			payload: &pipepayload.Payload{
				Representation: model.ContractRepresentationMultipartForm,
				Form: &formpayload.FormData{
					Encoding: formpayload.FormEncodingMultipart,
					Fields: []formpayload.FormField{{
						Name:       "email",
						ValueCount: 1,
						Values:     []string{"alice@example.com"},
					}},
				},
			},
		},
	}

	runtime := transformsql.NewRuntime()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			output, err := runtime.Execute(
				transformsql.Transform{
					SQL:        "SELECT email AS email FROM input",
					OutputKind: model.TransformOutputKindRows,
				},
				map[string]*pipepayload.Payload{"input": tt.payload},
			)

			require.Nil(t, output)
			require.ErrorIs(t, err, xerror.UnsupportedTransformRuntimeInput())
			require.ErrorContains(
				t,
				err,
				"transform runtime currently supports only SQL/FTP/HTTP/MongoDB tabular inputs",
			)
		})
	}
}
