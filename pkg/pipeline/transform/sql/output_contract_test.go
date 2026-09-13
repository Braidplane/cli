package sql

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/model"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"
	"github.com/Braidplane/cli/pkg/xerror"
)

func TestValidateOutputContract_DocumentNestedPath(t *testing.T) {
	t.Parallel()

	output := &pipepayload.Output{
		OutputKind: model.TransformOutputKindJSON,
		Document:   []byte(`{"profile":{"email":"alice@example.com"}}`),
	}
	contract := &pipepayload.Contract{Fields: []pipepayload.ContractField{{
		Name:     "profile.email",
		Type:     model.ContractFieldTypeValueString,
		Required: true,
	}}}

	err := ValidateOutputContract(output, contract)
	require.NoError(t, err)
}

func TestValidateOutputContract_DocumentArrayNestedPath(t *testing.T) {
	t.Parallel()

	output := &pipepayload.Output{
		OutputKind: model.TransformOutputKindJSON,
		Document:   []byte(`[{"profile":{"email":"alice@example.com"}}]`),
	}
	contract := &pipepayload.Contract{Fields: []pipepayload.ContractField{{
		Name:     "profile.email",
		Type:     model.ContractFieldTypeValueString,
		Required: true,
	}}}

	err := ValidateOutputContract(output, contract)
	require.NoError(t, err)
}

func TestValidateOutputContract_DocumentArrayIntegerField(t *testing.T) {
	t.Parallel()

	output := &pipepayload.Output{
		OutputKind: model.TransformOutputKindJSON,
		Document:   []byte(`[{"id":1}]`),
	}
	contract := &pipepayload.Contract{Fields: []pipepayload.ContractField{{
		Name:     "id",
		Type:     model.ContractFieldTypeValueInteger,
		Required: true,
	}}}

	err := ValidateOutputContract(output, contract)
	require.NoError(t, err)
}

func TestValidateOutputContract_DocumentRootShapeMismatch(t *testing.T) {
	t.Parallel()

	output := &pipepayload.Output{
		OutputKind: model.TransformOutputKindJSON,
		Document:   []byte(`[{"id":1}]`),
	}
	contract := &pipepayload.Contract{
		RootShape: model.ContractRootShapeObject,
		Fields: []pipepayload.ContractField{{
			Name:     "id",
			Type:     model.ContractFieldTypeValueInteger,
			Required: true,
		}},
	}

	err := ValidateOutputContract(output, contract)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrOutputContractViolation)
	require.ErrorContains(t, err, "root_shape")
	require.ErrorContains(t, err, model.ContractRootShapeObject.String())
	require.ErrorContains(t, err, model.ContractRootShapeArray.String())
}

func TestValidateOutputContract_DocumentNestedTypeMismatch(t *testing.T) {
	t.Parallel()

	output := &pipepayload.Output{
		OutputKind: model.TransformOutputKindJSON,
		Document:   []byte(`{"profile":{"age":"42"}}`),
	}
	contract := &pipepayload.Contract{Fields: []pipepayload.ContractField{{
		Name:     "profile.age",
		Type:     model.ContractFieldTypeValueInteger,
		Required: true,
	}}}

	err := ValidateOutputContract(output, contract)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrOutputContractViolation)
	require.ErrorContains(t, err, "profile.age")
	require.ErrorContains(t, err, "expects")
}

func TestValidateOutputContract_RowsNestedPath(t *testing.T) {
	t.Parallel()

	output := &pipepayload.Output{
		OutputKind: model.TransformOutputKindRows,
		Rows: []map[string]any{{
			"profile": map[string]any{"email": "alice@example.com"},
		}},
	}
	contract := &pipepayload.Contract{Fields: []pipepayload.ContractField{{
		Name:     "profile.email",
		Type:     model.ContractFieldTypeValueString,
		Required: true,
	}}}

	err := ValidateOutputContract(output, contract)
	require.NoError(t, err)
}

func TestValidateOutputContract_RowsNestedNonNullable(t *testing.T) {
	t.Parallel()

	output := &pipepayload.Output{
		OutputKind: model.TransformOutputKindRows,
		Rows: []map[string]any{{
			"profile": map[string]any{"email": nil},
		}},
	}
	contract := &pipepayload.Contract{Fields: []pipepayload.ContractField{{
		Name:     "profile.email",
		Type:     model.ContractFieldTypeValueString,
		Required: true,
		Nullable: false,
	}}}

	err := ValidateOutputContract(output, contract)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrOutputContractViolation)
	require.ErrorContains(t, err, "profile.email")
	require.ErrorContains(t, err, "non-nullable")
}

func TestValidateOutputContract_RowsArrayElementPath(t *testing.T) {
	t.Parallel()

	output := &pipepayload.Output{
		OutputKind: model.TransformOutputKindRows,
		Rows: []map[string]any{{
			"items": []any{
				map[string]any{"sku": "A-1"},
				map[string]any{"sku": "B-2"},
			},
		}},
	}
	contract := &pipepayload.Contract{Fields: []pipepayload.ContractField{{
		Name:     "items[].sku",
		Type:     model.ContractFieldTypeValueString,
		Required: true,
	}}}

	err := ValidateOutputContract(output, contract)
	require.NoError(t, err)
}

func TestValidateOutputContract_CSVRequiredColumnsPresent(t *testing.T) {
	t.Parallel()

	output := &pipepayload.Output{
		OutputKind:  model.TransformOutputKindCSV,
		ColumnNames: []string{"id", "email"},
		CSV:         "id,email\n1,alice@example.com\n",
	}
	contract := &pipepayload.Contract{Fields: []pipepayload.ContractField{
		{
			Name:     "id",
			Type:     model.ContractFieldTypeValueInteger,
			Required: true,
		},
		{
			Name:     "email",
			Type:     model.ContractFieldTypeValueString,
			Required: true,
		},
	}}

	err := ValidateOutputContract(output, contract)
	require.NoError(t, err)
}

func TestValidateOutputContract_CSVMissingRequiredColumn(t *testing.T) {
	t.Parallel()

	output := &pipepayload.Output{
		OutputKind:  model.TransformOutputKindCSV,
		ColumnNames: []string{"id"},
		CSV:         "id\n1\n",
	}
	contract := &pipepayload.Contract{Fields: []pipepayload.ContractField{
		{
			Name:     "id",
			Type:     model.ContractFieldTypeValueInteger,
			Required: true,
		},
		{
			Name:     "email",
			Type:     model.ContractFieldTypeValueString,
			Required: true,
		},
	}}

	err := ValidateOutputContract(output, contract)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrOutputContractViolation)
	require.ErrorContains(t, err, `required field "email" is missing`)
}

func TestValidateOutputContract_FieldValidations(t *testing.T) {
	t.Parallel()

	contract := loadTransformOutputContractFromYAML(t, `metadata:
  slug: transform-validation-runtime
  name: Transform Validation Runtime
steps:
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        input: fetch-users
      sql: SELECT id, email FROM input
      output_kind: rows
      output_contract:
        name: normalized_users
        representation: rows
        fields:
          - name: email
            type: string
            required: true
            validations:
              format: email
              min_length: 1
          - name: age
            type: integer
            required: true
            validations:
              min: 18
`)
	output := &pipepayload.Output{
		OutputKind: model.TransformOutputKindRows,
		Rows: []map[string]any{{
			"email": "not-an-email",
			"age":   16,
		}},
		ColumnNames: []string{"email", "age"},
	}

	err := ValidateOutputContract(output, contract)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrOutputContractViolation)
	require.ErrorContains(t, err, "normalized_users")
	require.ErrorContains(t, err, "email")
	require.ErrorContains(t, err, "format")
	require.NotContains(t, err.Error(), "not-an-email")
}

func TestValidateOutputContract_CSVCoercesValuesForFieldValidations(t *testing.T) {
	t.Parallel()

	contract := &pipepayload.Contract{
		Name:           "csv_users",
		Representation: model.ContractRepresentationCSV,
		Fields: []pipepayload.ContractField{{
			Name:     "id",
			Type:     model.ContractFieldTypeValueInteger,
			Required: true,
			Validations: &pipepayload.ContractFieldValidations{
				Min: int64(1),
			},
		}},
	}
	output := &pipepayload.Output{
		OutputKind:  model.TransformOutputKindCSV,
		ColumnNames: []string{"id"},
		CSV:         "id\n2\n",
	}

	err := ValidateOutputContract(output, contract)
	require.NoError(t, err)
}

func TestValidateTransformInputContract_FieldValidations(t *testing.T) {
	t.Parallel()

	contract := loadTransformInputContractFromYAML(t, `metadata:
  slug: transform-input-validation-runtime
  name: Transform Input Validation Runtime
steps:
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        input: fetch-users
      sql: SELECT id, email FROM input
      output_kind: rows
      input_contract:
        name: fetched_users
        representation: rows
        fields:
          - name: email
            type: string
            required: true
            validations:
              format: email
`)

	err := validateTransformInputContract(
		[]map[string]any{{"email": "not-an-email"}},
		contract,
		"runtime payload",
	)
	require.Error(t, err)
	require.ErrorIs(t, err, xerror.TransformInputContractViolation())
	require.ErrorContains(t, err, "fetched_users")
	require.ErrorContains(t, err, "email")
	require.ErrorContains(t, err, "format")
	require.NotContains(t, err.Error(), "not-an-email")
}

func loadTransformOutputContractFromYAML(t *testing.T, data string) *pipepayload.Contract {
	t.Helper()

	pipeline, err := pipelinespec.Load("pipeline.yaml", []byte(data))
	require.NoError(t, err)
	require.Len(t, pipeline.Steps, 1)
	require.NotNil(t, pipeline.Steps[0].Transform)
	require.NotNil(t, pipeline.Steps[0].Transform.OutputContract)
	return pipeline.Steps[0].Transform.OutputContract
}

func loadTransformInputContractFromYAML(t *testing.T, data string) *pipepayload.Contract {
	t.Helper()

	pipeline, err := pipelinespec.Load("pipeline.yaml", []byte(data))
	require.NoError(t, err)
	require.Len(t, pipeline.Steps, 1)
	require.NotNil(t, pipeline.Steps[0].Transform)
	require.NotNil(t, pipeline.Steps[0].Transform.InputContract)
	return pipeline.Steps[0].Transform.InputContract
}
