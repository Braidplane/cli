package payload_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/model"
	formpayload "github.com/Braidplane/cli/pkg/payload/form"
	"github.com/Braidplane/cli/pkg/pipeline/payload"
)

func TestPayloadClone_DetachesMutableFields(t *testing.T) {
	t.Parallel()

	original := &payload.Payload{
		Representation: model.ContractRepresentationRows,
		RootShape:      model.ContractRootShapeUnknown,
		Contract: &payload.Contract{Fields: []payload.ContractField{{
			Name:     "id",
			Type:     model.ContractFieldTypeValueInteger,
			Required: true,
		}}},
		ColumnNames: []string{"id"},
		Rows:        []map[string]any{{"id": 1}},
		Document:    []byte(`{"id":1}`),
		Form: &formpayload.FormData{
			Encoding: formpayload.FormEncodingMultipart,
			Fields: []formpayload.FormField{{
				Name:   "token",
				Values: []string{"secret"},
			}},
			Files: []formpayload.FormFile{{
				Name:    "upload",
				Content: []byte("file"),
			}},
		},
	}

	cloned := original.Clone()
	require.Equal(t, original, cloned)

	original.Contract.Fields[0].Name = "changed"
	original.ColumnNames[0] = "changed"
	original.Rows[0]["id"] = 2
	original.Document[0] = '['
	original.Form.Fields[0].Values[0] = "changed"
	original.Form.Files[0].Content[0] = 'x'

	require.Equal(t, "id", cloned.Contract.Fields[0].Name)
	require.Equal(t, []string{"id"}, cloned.ColumnNames)
	require.Equal(t, []map[string]any{{"id": 1}}, cloned.Rows)
	require.Equal(t, []byte(`{"id":1}`), []byte(cloned.Document))
	require.Equal(t, []string{"secret"}, cloned.Form.Fields[0].Values)
	require.Equal(t, []byte("file"), cloned.Form.Files[0].Content)
}
