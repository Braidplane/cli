package spec

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/Braidplane/cli/pkg/slug"
)

func TestLoadRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		path        string
		data        string
		wantMessage string
	}{
		{
			name: "YAMLTopLevelCompatibilityMode",
			path: "unknown-field.pipeline.yaml",
			data: `
metadata:
  slug: unknown-field
  name: Unknown Field
compatibility_mode: loose
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-users
`,
			wantMessage: "compatibility_mode",
		},
		{
			name: "YAMLNestedStepField",
			path: "unknown-step-field.pipeline.yaml",
			data: `
metadata:
  slug: unknown-step-field
  name: Unknown Step Field
steps:
  - kind: query
    slug: fetch-users
    retries: 3
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-users
`,
			wantMessage: "retries",
		},
		{
			name: "YAMLLegacyDataSourceSlug",
			path: "legacy-data-source-slug.pipeline.yaml",
			data: `
metadata:
  slug: legacy-data-source-slug
  name: Legacy Data Source Slug
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_slug: acme-postgres
      operation_type: sql
      operation_ref: list-users
`,
			wantMessage: "data_source_slug",
		},
		{
			name: "YAMLLegacySinkDataSourceSlug",
			path: "legacy-sink-data-source-slug.pipeline.yaml",
			data: `
metadata:
  slug: legacy-sink-data-source-slug
  name: Legacy Sink Data Source Slug
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-users
  - kind: sink
    slug: write-users
    sink:
      input: fetch-users
      data_source_slug: acme-mysql
      operation_type: sql
      operation_ref: upsert-users
      idempotency_class: idempotent_with_key
`,
			wantMessage: "data_source_slug",
		},
		{
			name: "JSONTopLevelCompatibilityMode",
			path: "unknown-field.pipeline.json",
			data: `{
  "metadata": {"slug": "unknown-field", "name": "Unknown Field"},
  "compatibility_mode": "loose",
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-postgres",
      "operation_type": "sql",
      "operation_ref": "list-users"
    }
  }]
}`,
			wantMessage: "compatibility_mode",
		},
		{
			name: "JSONNestedStepField",
			path: "unknown-step-field.pipeline.json",
			data: `{
  "metadata": {"slug": "unknown-step-field", "name": "Unknown Step Field"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "retries": 3,
    "query": {
      "data_source_ref": "acme-postgres",
      "operation_type": "sql",
      "operation_ref": "list-users"
    }
  }]
}`,
			wantMessage: "retries",
		},
		{
			name: "JSONLegacyDataSourceSlug",
			path: "legacy-data-source-slug.pipeline.json",
			data: `{
  "metadata": {"slug": "legacy-data-source-slug", "name": "Legacy Data Source Slug"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_slug": "acme-postgres",
      "operation_type": "sql",
      "operation_ref": "list-users"
    }
  }]
	}`,
			wantMessage: "data_source_slug",
		},
		{
			name: "JSONLegacySinkDataSourceSlug",
			path: "legacy-sink-data-source-slug.pipeline.json",
			data: `{
  "metadata": {"slug": "legacy-sink-data-source-slug", "name": "Legacy Sink Data Source Slug"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-postgres",
      "operation_type": "sql",
      "operation_ref": "list-users"
    }
  }, {
    "kind": "sink",
    "slug": "write-users",
    "sink": {
      "input": "fetch-users",
      "data_source_slug": "acme-mysql",
      "operation_type": "sql",
      "operation_ref": "upsert-users",
      "idempotency_class": "idempotent_with_key"
    }
  }]
}`,
			wantMessage: "data_source_slug",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pipeline, err := Load(tt.path, []byte(tt.data))

			require.Nil(t, pipeline)
			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantMessage)
		})
	}
}

func TestLoadRejectsNullContractFieldValidationRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		data string
	}{
		{
			name: "YAMLNullFormat",
			path: "null-validation.pipeline.yaml",
			data: `
metadata:
  slug: null-validation
  name: Null Validation
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-users
      output_contract:
        name: users_rows
        fields:
          - name: email
            type: string
            validations:
              format: null
`,
		},
		{
			name: "JSONNullMin",
			path: "null-validation.pipeline.json",
			data: `{
  "metadata": {"slug": "null-validation", "name": "Null Validation"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-postgres",
      "operation_type": "sql",
      "operation_ref": "list-users",
      "output_contract": {
        "name": "users_rows",
        "fields": [{
          "name": "score",
          "type": "integer",
          "validations": {"min": null}
        }]
      }
    }
  }]
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pipeline, err := Load(tt.path, []byte(tt.data))

			require.Nil(t, pipeline)
			require.Error(t, err)
			require.ErrorContains(t, err, "must not be null")
		})
	}
}

func TestStepInputScalarJSON(t *testing.T) {
	t.Parallel()

	input := StepInput{FromStep: slug.Slug("fetch-users")}

	data, err := json.Marshal(input)
	require.NoError(t, err)
	require.JSONEq(t, `"fetch-users"`, string(data))

	var decoded StepInput
	require.NoError(t, json.Unmarshal([]byte(`"normalize-users"`), &decoded))
	require.Equal(t, slug.Slug("normalize-users"), decoded.FromStep)
}

func TestLoadJSONDynamicNumbersRemainRuntimeCompatible(t *testing.T) {
	t.Parallel()

	pipeline, err := Load("runtime-numbers.pipeline.json", []byte(`{
  "metadata": {"slug": "runtime-numbers", "name": "Runtime Numbers"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-postgres",
      "operation_type": "sql",
      "operation_ref": "list-users",
      "parameters": {
        "limit": 100
      }
    }
  }]
}`))

	require.NoError(t, err)
	require.IsType(t, float64(0), pipeline.Steps[0].Query.Parameters["limit"])
	require.InDelta(t, 100, pipeline.Steps[0].Query.Parameters["limit"], 0)
}

func TestLoadJSONPreservesContractValidationIntegerPrecision(t *testing.T) {
	t.Parallel()

	pipeline, err := Load("contract-validation-numbers.pipeline.json", []byte(`{
  "metadata": {"slug": "contract-validation-numbers", "name": "Contract Validation Numbers"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-postgres",
      "operation_type": "sql",
      "operation_ref": "list-users",
      "output_contract": {
        "name": "users_rows",
        "representation": "rows",
        "fields": [{
          "name": "id",
          "type": "integer",
          "required": true,
          "validations": {
            "min": 9007199254740993,
            "enum": [9007199254740993]
          }
        }]
      }
    }
  }]
}`))

	require.NoError(t, err)
	validations := pipeline.Steps[0].Query.OutputContract.Fields[0].Validations
	require.NotNil(t, validations)
	require.Equal(t, json.Number("9007199254740993"), validations.Min)
	require.Equal(t, []any{json.Number("9007199254740993")}, validations.Enum)
}

func TestLoadJSONRejectsUnknownContractValidationKeys(t *testing.T) {
	t.Parallel()

	pipeline, err := Load("unknown-contract-validation.pipeline.json", []byte(`{
  "metadata": {"slug": "unknown-contract-validation", "name": "Unknown Contract Validation"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-postgres",
      "operation_type": "sql",
      "operation_ref": "list-users",
      "output_contract": {
        "name": "users_rows",
        "representation": "rows",
        "fields": [{
          "name": "id",
          "type": "integer",
          "validations": {
            "minimum": 1
          }
        }]
      }
    }
  }]
}`))

	require.Nil(t, pipeline)
	require.Error(t, err)
	require.ErrorContains(t, err, "minimum")
}

func TestStepInputScalarYAML(t *testing.T) {
	t.Parallel()

	input := StepInput{FromStep: slug.Slug("fetch-users")}

	data, err := yaml.Marshal(input)
	require.NoError(t, err)
	require.Equal(t, "fetch-users\n", string(data))

	var decoded StepInput
	require.NoError(t, yaml.Unmarshal([]byte("normalize-users\n"), &decoded))
	require.Equal(t, slug.Slug("normalize-users"), decoded.FromStep)
}

func TestStepInputYAMLRejectsObject(t *testing.T) {
	t.Parallel()

	var decoded StepInput
	err := yaml.Unmarshal([]byte("from_step: fetch-users\n"), &decoded)

	require.Error(t, err)
	require.ErrorContains(t, err, "step input must be a step slug")
}
