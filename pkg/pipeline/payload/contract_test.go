package payload_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/model"
	formpayload "github.com/Braidplane/cli/pkg/payload/form"
	"github.com/Braidplane/cli/pkg/pipeline/payload"
	"github.com/Braidplane/cli/pkg/xerror"
)

func TestResolveFieldPathValues_ResolvesArrayElementPaths(t *testing.T) {
	t.Parallel()

	values, exists, hasMissing, err := payload.ResolveFieldPathValues(
		map[string]any{
			"items": []any{
				map[string]any{"sku": "sku-1"},
				map[string]any{"sku": "sku-2"},
			},
		},
		"items[].sku",
	)

	require.NoError(t, err)
	require.True(t, exists)
	require.False(t, hasMissing)
	require.Equal(t, []any{"sku-1", "sku-2"}, values)
}

func TestResolveFieldPathValues_ReportsPartiallyMissingArrayPath(t *testing.T) {
	t.Parallel()

	values, exists, hasMissing, err := payload.ResolveFieldPathValues(
		map[string]any{
			"items": []any{
				map[string]any{"sku": "sku-1"},
				map[string]any{"name": "missing-sku"},
			},
		},
		"items[].sku",
	)

	require.NoError(t, err)
	require.True(t, exists)
	require.True(t, hasMissing)
	require.Equal(t, []any{"sku-1"}, values)
}

func TestResolveFieldPathValues_ReturnsTypedErrorForInvalidArraySegment(t *testing.T) {
	t.Parallel()

	values, exists, hasMissing, err := payload.ResolveFieldPathValues(
		map[string]any{"items": "not-array"},
		"items[].sku",
	)

	require.Error(t, err)
	require.Nil(t, values)
	require.False(t, exists)
	require.False(t, hasMissing)
	require.True(t, xerror.Is(err, xerror.InvalidFieldPath()))
}

func TestFieldPathExistsRequiresCompletePathInAtLeastOneRow(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"profile": map[string]any{"email": "user@example.com"}},
		{"profile": map[string]any{"name": "No Email"}},
	}

	require.True(t, payload.FieldPathExists(rows, "profile.email"))
	require.False(t, payload.FieldPathExists(rows, "profile.phone"))
}

func TestInferFieldTypeMapsRuntimeValues(t *testing.T) {
	t.Parallel()

	require.Equal(t, model.ContractFieldTypeValueInteger, payload.InferFieldType(json.Number("12")))
	require.Equal(t, model.ContractFieldTypeValueNumber, payload.InferFieldType(json.Number("12.5")))
	require.Equal(t, model.ContractFieldTypeValueObject, payload.InferFieldType(map[string]any{"id": 1}))
	require.Equal(t, model.ContractFieldTypeValueArray, payload.InferFieldType([]any{"id"}))
}

func TestCollectColumnNamesSortsUniqueRowKeys(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		[]string{"email", "id", "name"},
		payload.CollectColumnNames([]map[string]any{
			{"id": 1, "email": "user@example.com"},
			{"name": "User", "id": 2},
		}),
	)
}

func TestParseCSVBodyToRowsRejectsInvalidHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{
			name: "EmptyHeader",
			body: "id,,email\n1,ignored,alice@example.com\n",
		},
		{
			name: "DuplicateHeader",
			body: "id,id\n1,2\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := payload.ParseCSVBodyToRows([]byte(tt.body))
			require.ErrorIs(t, err, xerror.UnsupportedHTTPResponseFormat())
		})
	}
}

func TestParseJSONBodyToRowsPreserveNumbersRejectsTrailingData(t *testing.T) {
	t.Parallel()

	_, _, err := payload.ParseJSONBodyToRowsPreserveNumbers([]byte(`[{"id":1}] trailing`))
	require.ErrorIs(t, err, xerror.UnsupportedHTTPResponseFormat())
}

func TestEffectiveContractDerivesFieldsFromRowsPayload(t *testing.T) {
	t.Parallel()

	contract := payload.EffectiveContract(
		"fetch-users-output",
		&payload.Payload{
			Representation: model.ContractRepresentationRows,
			ColumnNames:    []string{"id", "email"},
			Rows: []map[string]any{{
				"id":    1,
				"email": "alice@example.com",
			}},
		},
		nil,
	)

	require.NotNil(t, contract)
	require.Equal(t, "fetch-users-output", contract.Name)
	require.Equal(t, model.ContractRepresentationRows, contract.Representation)
	require.Equal(t, model.ContractRootShapeUnknown, contract.RootShape)
	require.Equal(t, []payload.ContractField{
		{Name: "id", Type: model.ContractFieldTypeValueInteger},
		{Name: "email", Type: model.ContractFieldTypeValueString},
	}, contract.Fields)
}

func TestEffectiveContractDerivesFieldsFromJSONPayload(t *testing.T) {
	t.Parallel()

	contract := payload.EffectiveContract(
		"fetch-http-output",
		&payload.Payload{
			Representation: model.ContractRepresentationJSON,
			RootShape:      model.ContractRootShapeArray,
			Document:       []byte(`[{"id":1,"active":true}]`),
		},
		nil,
	)

	require.NotNil(t, contract)
	require.Equal(t, model.ContractRepresentationJSON, contract.Representation)
	require.Equal(t, model.ContractRootShapeArray, contract.RootShape)
	require.Equal(t, []payload.ContractField{
		{Name: "active", Type: model.ContractFieldTypeValueBoolean},
		{Name: "id", Type: model.ContractFieldTypeValueInteger},
	}, contract.Fields)
}

func TestEffectiveContractDerivesFieldsFromCSVPayload(t *testing.T) {
	t.Parallel()

	contract := payload.EffectiveContract(
		"fetch-file-output",
		&payload.Payload{
			Representation: model.ContractRepresentationCSV,
			CSV:            "id,email\n1,alice@example.com\n",
		},
		nil,
	)

	require.NotNil(t, contract)
	require.Equal(t, model.ContractRepresentationCSV, contract.Representation)
	require.Equal(t, []payload.ContractField{
		{Name: "id", Type: model.ContractFieldTypeValueString},
		{Name: "email", Type: model.ContractFieldTypeValueString},
	}, contract.Fields)
}

func TestEffectiveContractDerivesFieldsFromMultipartFormPayload(t *testing.T) {
	t.Parallel()

	contract := payload.EffectiveContract(
		"submit-ticket-output",
		&payload.Payload{
			Representation: model.ContractRepresentationMultipartForm,
			Form: &formpayload.FormData{
				Encoding: formpayload.FormEncodingMultipart,
				Fields: []formpayload.FormField{
					{Name: "subject", Values: []string{"broken import"}},
					{Name: "tag", Values: []string{"urgent", "customer"}},
				},
				Files: []formpayload.FormFile{{
					Name:        "attachment",
					Filename:    "import.csv",
					ContentType: "text/csv",
					Content:     []byte("id\n1\n"),
				}},
			},
		},
		nil,
	)

	require.NotNil(t, contract)
	require.Equal(t, model.ContractRepresentationMultipartForm, contract.Representation)
	require.Equal(t, []payload.ContractField{
		{Name: "attachment", Type: model.ContractFieldTypeValueObject},
		{Name: "subject", Type: model.ContractFieldTypeValueString},
		{Name: "tag", Type: model.ContractFieldTypeValueArray},
	}, contract.Fields)
}

func TestEffectiveContractPreservesDeclaredFacets(t *testing.T) {
	t.Parallel()

	contract := payload.EffectiveContract(
		"fetch-users-output",
		&payload.Payload{
			Representation: model.ContractRepresentationRows,
			Rows:           []map[string]any{{"payload": map[string]any{"id": 1}}},
			ColumnNames:    []string{"payload"},
		},
		&payload.Contract{
			Representation: model.ContractRepresentationJSON,
			RootShape:      model.ContractRootShapeObject,
		},
	)

	require.NotNil(t, contract)
	require.Equal(t, model.ContractRepresentationJSON, contract.Representation)
	require.Equal(t, model.ContractRootShapeObject, contract.RootShape)
	require.Equal(t, []payload.ContractField{
		{Name: "payload", Type: model.ContractFieldTypeValueObject},
	}, contract.Fields)
}

func TestMissingRequiredFieldChecksColumnsBeforeRows(t *testing.T) {
	t.Parallel()

	fieldName, missing := payload.MissingRequiredField(
		[]string{"id"},
		nil,
		&payload.Contract{Fields: []payload.ContractField{{
			Name:     "id",
			Required: true,
		}}},
	)

	require.False(t, missing)
	require.Empty(t, fieldName)
}

func TestMissingRequiredFieldChecksNestedRows(t *testing.T) {
	t.Parallel()

	fieldName, missing := payload.MissingRequiredField(
		nil,
		[]map[string]any{{
			"profile": map[string]any{"email": "user@example.com"},
		}},
		&payload.Contract{Fields: []payload.ContractField{{
			Name:     "profile.email",
			Required: true,
		}}},
	)

	require.False(t, missing)
	require.Empty(t, fieldName)
}

func TestMissingRequiredFieldReturnsFirstMissingRequiredField(t *testing.T) {
	t.Parallel()

	fieldName, missing := payload.MissingRequiredField(
		[]string{"id"},
		[]map[string]any{{"id": 1}},
		&payload.Contract{Fields: []payload.ContractField{
			{Name: "id", Required: true},
			{Name: "email", Required: true},
		}},
	)

	require.True(t, missing)
	require.Equal(t, "email", fieldName)
}
