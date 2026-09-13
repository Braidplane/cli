package validate

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/model"
	"github.com/Braidplane/cli/pkg/must"
	"github.com/Braidplane/cli/pkg/pipeline/payload"
	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"
	"github.com/Braidplane/cli/pkg/slug"
)

func testSinkSpec(fromStep slug.Slug, inputContract *payload.Contract) *pipelinespec.SinkSpec {
	return &pipelinespec.SinkSpec{
		Input:            &pipelinespec.StepInput{FromStep: fromStep},
		InputContract:    inputContract,
		DataSourceSlug:   "acme-sink",
		OperationType:    "sql",
		OperationRef:     "upsert-users",
		IdempotencyClass: pipelinespec.SinkIdempotencyClassIdempotentWithKey,
	}
}

func TestValidateStepsAcceptsOneJSONInputAndRejectsUnsupportedInputShapes(t *testing.T) {
	t.Parallel()

	jsonContract := func() *payload.Contract {
		return &payload.Contract{
			Name:           "event",
			Representation: model.ContractRepresentationJSON,
			RootShape:      model.ContractRootShapeObject,
			Fields: []payload.ContractField{{
				Name: "id", Type: model.ContractFieldTypeValueString, Required: true,
			}},
		}
	}
	inputStep := func(stepSlug slug.Slug, contract *payload.Contract) pipelinespec.Step {
		return pipelinespec.Step{
			Kind: "input", Slug: stepSlug, Input: &pipelinespec.InputSpec{OutputContract: contract},
		}
	}

	require.NoError(t, validateSteps([]pipelinespec.Step{inputStep("event", jsonContract())}))

	err := validateSteps([]pipelinespec.Step{
		inputStep("event", jsonContract()),
		inputStep("another-event", jsonContract()),
	})
	require.ErrorContains(t, err, "exactly one input step")

	rowsContract := jsonContract()
	rowsContract.Representation = model.ContractRepresentationRows
	err = validateSteps([]pipelinespec.Step{inputStep("event", rowsContract)})
	require.ErrorContains(t, err, "requires json representation")
}

func TestValidateStepsRejectsBlocksThatDoNotMatchKind(t *testing.T) {
	t.Parallel()

	err := validateSteps([]pipelinespec.Step{{
		Kind:  nodeKindQuery,
		Slug:  "fetch-users",
		Query: &pipelinespec.QueryRef{},
		Input: &pipelinespec.InputSpec{},
	}})

	require.ErrorContains(t, err, "kind=query requires exactly its matching block")

	err = validateSteps([]pipelinespec.Step{{
		Kind:  nodeKindQuery,
		Slug:  "fetch-users",
		Input: &pipelinespec.InputSpec{},
	}})

	require.ErrorContains(t, err, "kind=query requires exactly its matching block")
}

func TestValidateStepsAllowsHTTPMultipartFormSinkInputContract(t *testing.T) {
	t.Parallel()

	err := validateSteps([]pipelinespec.Step{{
		Kind: "sink",
		Slug: "submit-form",
		Sink: &pipelinespec.SinkSpec{
			IdempotencyClass: pipelinespec.SinkIdempotencyClassIdempotentWithKey,
			Input:            &pipelinespec.StepInput{FromStep: "build-form"},
			DataSourceSlug:   "acme-http",
			OperationType:    "http",
			OperationRef:     "submit-ticket",
			InputContract: &payload.Contract{
				Representation: model.ContractRepresentationMultipartForm,
				Fields: []payload.ContractField{{
					Name: "subject",
					Type: model.ContractFieldTypeValueString,
				}},
			},
		},
	}})

	require.NoError(t, err)
}

func TestValidateStepsRejectsMultipartFormSQLTransformContract(t *testing.T) {
	t.Parallel()

	err := validateSteps([]pipelinespec.Step{{
		Kind: "transform",
		Slug: "normalize-form",
		Transform: &pipelinespec.TransformSpec{
			SQL:        "SELECT subject FROM input",
			OutputKind: model.TransformOutputKindRows,
			InputContract: &payload.Contract{
				Representation: model.ContractRepresentationMultipartForm,
				Fields: []payload.ContractField{{
					Name: "subject",
					Type: model.ContractFieldTypeValueString,
				}},
			},
		},
	}})

	require.ErrorContains(t, err, "multipart_form contracts are not supported by sql_v1 transform steps")
}

func TestValidateStepsRejectsMultipartFormNonHTTPSinkContract(t *testing.T) {
	t.Parallel()

	err := validateSteps([]pipelinespec.Step{{
		Kind: "sink",
		Slug: "submit-form",
		Sink: testSinkSpec("build-form", &payload.Contract{
			Representation: model.ContractRepresentationMultipartForm,
			Fields: []payload.ContractField{{
				Name: "subject",
				Type: model.ContractFieldTypeValueString,
			}},
		}),
	}})

	require.ErrorContains(t, err, "multipart_form sink input_contract is only supported for http operation_type")
}

func TestLoadAllowsContractFieldValidations(t *testing.T) {
	t.Parallel()

	pipeline := loadPipelineSpecForValidationTest(t, `metadata:
  slug: validated-users
  name: Validated Users
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-users
      output_contract:
        name: users_rows
        representation: rows
        fields:
          - name: id
            type: integer
            required: true
            validations:
              unique: true
          - name: email
            type: string
            required: true
            nullable: false
            validations:
              format: email
              min_length: 1
          - name: status
            type: string
            required: true
            validations:
              enum:
                - active
                - paused
          - name: updated_at
            type: datetime
            required: true
            validations:
              min: "2026-01-01T00:00:00Z"
`)

	require.NoError(t, Validate(pipeline))
}

func TestValidateRejectsMalformedContractFieldValidations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		fieldType       string
		representation  string
		operationType   string
		validations     string
		wantLoadErr     bool
		wantErrContains []string
	}{
		{
			name:           "RejectsStringMin",
			fieldType:      "string",
			representation: "rows",
			validations: `              min: 10
`,
			wantErrContains: []string{"email", "min", "number"},
		},
		{
			name:           "RejectsUnsupportedFormat",
			fieldType:      "string",
			representation: "rows",
			validations: `              format: postal_code
`,
			wantErrContains: []string{"email", "format", "postal_code"},
		},
		{
			name:           "RejectsUnsupportedOperator",
			fieldType:      "string",
			representation: "rows",
			validations: `              pattern: "^[a-z]+$"
`,
			wantLoadErr:     true,
			wantErrContains: []string{"pattern"},
		},
		{
			name:           "RejectsEmptyEnum",
			fieldType:      "string",
			representation: "rows",
			validations: `              enum: []
`,
			wantErrContains: []string{"email", "enum"},
		},
		{
			name:           "RejectsInvalidLengthRange",
			fieldType:      "string",
			representation: "rows",
			validations: `              min_length: 10
              max_length: 3
`,
			wantErrContains: []string{"email", "min_length", "max_length"},
		},
		{
			name:           "RejectsInvalidNumericRange",
			fieldType:      "integer",
			representation: "rows",
			validations: `              min: 10
              max: 3
`,
			wantErrContains: []string{"email", "min", "max"},
		},
		{
			name:           "RejectsUniqueOnJSON",
			fieldType:      "string",
			representation: "json",
			validations: `              unique: true
`,
			wantErrContains: []string{"email", "unique", "json"},
		},
		{
			name:           "RejectsFileFieldValidations",
			fieldType:      "string",
			representation: "file",
			validations: `              format: uuid
`,
			wantErrContains: []string{"email", "format", "file"},
		},
		{
			name:           "RejectsMultipartFormFieldValidations",
			fieldType:      "string",
			representation: "multipart_form",
			operationType:  "http",
			validations: `              format: email
`,
			wantErrContains: []string{"email", "format", "multipart_form"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			operationType := tt.operationType
			if operationType == "" {
				operationType = "sql"
			}
			pipelineData := `metadata:
  slug: malformed-validations
  name: Malformed Validations
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: ` + operationType + `
      operation_ref: list-users
      output_contract:
        name: users_rows
        representation: ` + tt.representation + `
        fields:
          - name: email
            type: ` + tt.fieldType + `
            required: true
            validations:
` + tt.validations

			pipeline, loadErr := pipelinespec.Load("pipeline.yaml", []byte(pipelineData))
			if tt.wantLoadErr {
				require.Error(t, loadErr)
				for _, want := range tt.wantErrContains {
					require.ErrorContains(t, loadErr, want)
				}
				return
			}
			require.NoError(t, loadErr)
			err := Validate(pipeline)
			require.Error(t, err)
			for _, want := range tt.wantErrContains {
				require.ErrorContains(t, err, want)
			}
		})
	}
}

func TestValidateAllowsUniqueWithInferredRowsRepresentation(t *testing.T) {
	t.Parallel()

	pipelineData := `metadata:
  slug: inferred-unique-validation
  name: Inferred Unique Validation
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: postgres-main
      operation_type: sql
      operation_ref: list-users
      output_contract:
        name: users_rows
        fields:
          - name: email
            type: string
            required: true
            validations:
              unique: true
`
	pipeline, err := Load("inferred-unique-validation.pipeline.yaml", []byte(pipelineData))
	require.NoError(t, err)

	err = Validate(pipeline)
	require.NoError(t, err)
}

func TestValidateRejectsUniqueWithInferredJSONRepresentation(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"InferredMongoDBJSON": `metadata:
  slug: inferred-json-unique-validation
  name: Inferred JSON Unique Validation
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: mongo-main
      operation_type: mongodb
      operation_ref: list-users
      output_contract:
        name: users_json
        fields:
          - name: email
            type: string
            required: true
            validations:
              unique: true
`,
		"ExplicitRowsMongoDBJSON": `metadata:
  slug: explicit-rows-json-unique-validation
  name: Explicit Rows JSON Unique Validation
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: mongo-main
      operation_type: mongodb
      operation_ref: list-users
      output_contract:
        name: users_json
        representation: rows
        fields:
          - name: email
            type: string
            required: true
            validations:
              unique: true
`,
	}

	for name, pipelineData := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			pipeline, err := pipelinespec.Load("inferred-json-unique-validation.pipeline.yaml", []byte(pipelineData))
			require.NoError(t, err)

			err = Validate(pipeline)
			require.Error(t, err)
			require.ErrorContains(t, err, "email")
			require.ErrorContains(t, err, "unique")
			require.ErrorContains(t, err, "json")
		})
	}
}

func loadPipelineSpecForValidationTest(t *testing.T, data string) *pipelinespec.Pipeline {
	t.Helper()

	pipeline, err := pipelinespec.Load("pipeline.yaml", []byte(data))
	require.NoError(t, err)
	return pipeline
}

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		path            string
		data            string
		wantErrContains string
		wantErrIs       error
		assertFn        func(t *testing.T, p *pipelinespec.Pipeline)
	}{
		{
			name: "LoadsTransformStep",
			path: "pipeline.transform.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
      output_contract:
        fields:
          - name: id
            type: integer
            required: true
          - name: email
            type: string
            required: true
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        input: fetch-users
      sql: SELECT id, email FROM input
      output_kind: rows
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 2)
				require.NotNil(t, p.Steps[1].Transform)
				require.Equal(t, "fetch-users", string(p.Steps[1].Transform.Inputs["input"].FromStep))
				require.Equal(t, model.TransformOutputKindRows, p.Steps[1].Transform.OutputKind)
			},
		},
		{
			name: "LoadsTransformStepWithCSVOutputKind",
			path: "pipeline.transform.csv-output-kind.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
      output_contract:
        fields:
          - name: id
            type: integer
            required: true
          - name: email
            type: string
            required: true
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        input: fetch-users
      sql: SELECT id, email FROM input
      output_kind: csv
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 2)
				require.NotNil(t, p.Steps[1].Transform)
				require.Equal(t, model.TransformOutputKindCSV, p.Steps[1].Transform.OutputKind)
			},
		},
		{
			name: "LoadsTransformStepWithJSONOutputKind",
			path: "pipeline.transform.json-output-kind.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
      output_contract:
        fields:
          - name: id
            type: integer
            required: true
          - name: email
            type: string
            required: true
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        input: fetch-users
      sql: SELECT id, email FROM input
      output_kind: json
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 2)
				require.NotNil(t, p.Steps[1].Transform)
				require.Equal(t, model.TransformOutputKindJSON, p.Steps[1].Transform.OutputKind)
			},
		},
		{
			name: "RejectsTransformReadingFromSink",
			path: "pipeline.sink-terminal.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
  - kind: sink
    slug: load-users
    sink:
      idempotency_class: idempotent_with_key
      input: fetch-users
      data_source_ref: acme-sink
      operation_type: sql
      operation_ref: upsert-users
  - kind: transform
    slug: after-sink
    transform:
      inputs:
        input: load-users
      sql: SELECT * FROM input
      output_kind: rows
`,
			wantErrContains: `source "load-users" must be a query or transform step`,
		},
		{
			name: "LoadsPipelineWithNamedTransformInputsFromMultipleSources",
			path: "pipeline.multi-source-transform.yaml",
			data: `metadata:
  slug: acme-source-inspection
  name: ACME Source Inspection
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-users
      output_contract:
        name: user-rows
        representation: rows
        fields:
          - name: id
            type: integer
            required: true
          - name: email
            type: string
            required: true
  - kind: query
    slug: fetch-accounts
    query:
      data_source_ref: acme-crm
      operation_type: sql
      operation_ref: list-accounts
      output_contract:
        name: account-rows
        representation: rows
        fields:
          - name: user_id
            type: integer
            required: true
          - name: name
            type: string
            required: true
  - kind: transform
    slug: enrich-users
    transform:
      inputs:
        users: fetch-users
        accounts: fetch-accounts
      sql: |
        SELECT users.id, users.email, accounts.name AS account_name
        FROM users
        JOIN accounts ON accounts.user_id = users.id
      output_kind: rows
      output_contract:
        fields:
          - name: id
            type: integer
            required: true
          - name: email
            type: string
            required: true
          - name: account_name
            type: string
            required: true
  - kind: sink
    slug: write-enriched-users
    sink:
      idempotency_class: idempotent_with_key
      input: enrich-users
      data_source_ref: acme-sink
      operation_type: sql
      operation_ref: upsert-users
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 4)
				require.Equal(t, "fetch-users", p.Steps[0].Slug.String())
				require.Equal(t, "acme-postgres", p.Steps[0].Query.DataSourceSlug.String())
				require.Equal(t, "fetch-accounts", p.Steps[1].Slug.String())
				require.Equal(t, "acme-crm", p.Steps[1].Query.DataSourceSlug.String())
				require.Equal(t, "enrich-users", p.Steps[2].Slug.String())
				require.Equal(t, "write-enriched-users", p.Steps[3].Slug.String())
			},
		},
		{
			name: "RejectsTransformStepWithUnknownOutputKind",
			path: "pipeline.transform.invalid-output-kind.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
      output_contract:
        fields:
          - name: id
            type: integer
            required: true
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        input: fetch-users
      sql: SELECT id, email FROM input
      output_kind: binary
`,
			wantErrContains: "does not belong to TransformOutputKind values",
		},
		{
			name: "RejectsTransformInputNameThatIsNotSQLRelationIdentifier",
			path: "pipeline.transform.invalid-input-name.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
      output_contract:
        fields:
          - name: id
            type: integer
            required: true
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        fetch-users: fetch-users
      sql: SELECT id FROM users
      output_kind: rows
`,
			wantErrContains: `transform input name "fetch-users" must be a valid unquoted SQL relation identifier`,
		},
		{
			name: "RejectsTransformStepWithInvalidSQLBeforeRuntimeGuard",
			path: "pipeline.transform.invalid-sql.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        input: fetch-users
      sql: "INSERT INTO users VALUES (1)"
      output_kind: rows
`,
			wantErrContains: "transform sql must start with SELECT",
		},
		{
			name: "RejectsTransformStepWithMissingRequiredOutputContractFieldBeforeRuntimeGuard",
			path: "pipeline.transform.missing-contract-field.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        input: fetch-users
      sql: "SELECT CAST(1 AS SIGNED) AS id FROM input"
      output_kind: rows
      output_contract:
        fields:
          - name: email
            type: string
            required: true
`,
			wantErrContains: "required output contract field \"email\" is missing",
		},
		{
			name: "LoadsTransformStepWithOutputContract",
			path: "pipeline.transform.contract-type-mismatch.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
      output_contract:
        fields:
          - name: id
            type: integer
            required: true
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        input: fetch-users
      sql: "SELECT CAST('1' AS CHAR) AS id FROM input"
      output_kind: rows
      output_contract:
        fields:
          - name: id
            type: integer
            required: true
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 2)
				require.NotNil(t, p.Steps[1].Transform)
				require.NotNil(t, p.Steps[1].Transform.OutputContract)
				require.Len(t, p.Steps[1].Transform.OutputContract.Fields, 1)
				require.Equal(
					t,
					model.ContractFieldTypeValueInteger,
					p.Steps[1].Transform.OutputContract.Fields[0].Type,
				)
			},
		},
		{
			name: "LoadsQueryStepWithOutputContract",
			path: "pipeline.query.output-contract.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
      output_contract:
        fields:
          - name: email
            type: string
            required: true
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 1)
				require.NotNil(t, p.Steps[0].Query)
				require.NotNil(t, p.Steps[0].Query.OutputContract)
				require.Len(t, p.Steps[0].Query.OutputContract.Fields, 1)
				require.Equal(
					t,
					model.ContractFieldTypeValueString,
					p.Steps[0].Query.OutputContract.Fields[0].Type,
				)
			},
		},
		{
			name: "LoadsQueryStepWithPersistedOperationRef",
			path: "pipeline.query.operation-ref.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 1)
				require.NotNil(t, p.Steps[0].Query)
				require.Equal(t, "sql", p.Steps[0].Query.OperationType)
				require.Equal(t, "list-active-users", p.Steps[0].Query.OperationRef.String())
			},
		},
		{
			name: "LoadsQueryStepWithHTTPPersistedOperationRef",
			path: "pipeline.query.operation-ref-http.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: crm-api
      operation_type: http
      operation_ref: fetch-users-http
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 1)
				require.NotNil(t, p.Steps[0].Query)
				require.Equal(t, "crm-api", p.Steps[0].Query.DataSourceSlug.String())
				require.Equal(t, "http", p.Steps[0].Query.OperationType)
				require.Equal(t, "fetch-users-http", p.Steps[0].Query.OperationRef.String())
			},
		},
		{
			name: "LoadsQueryStepWithHubSpotPersistedOperationRef",
			path: "pipeline.query.operation-ref-hubspot.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-contacts
    query:
      data_source_ref: hubspot-crm
      operation_type: hubspot
      operation_ref: search-contacts
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 1)
				require.NotNil(t, p.Steps[0].Query)
				require.Equal(t, "hubspot-crm", p.Steps[0].Query.DataSourceSlug.String())
				require.Equal(t, "hubspot", p.Steps[0].Query.OperationType)
				require.Equal(t, "search-contacts", p.Steps[0].Query.OperationRef.String())
			},
		},
		{
			name: "LoadsQueryStepWithMongoDBPersistedOperationRef",
			path: "pipeline.query.operation-ref-mongodb.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-mongodb
      operation_type: mongodb
      operation_ref: find-users
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 1)
				require.NotNil(t, p.Steps[0].Query)
				require.Equal(t, "acme-mongodb", p.Steps[0].Query.DataSourceSlug.String())
				require.Equal(t, "mongodb", p.Steps[0].Query.OperationType)
				require.Equal(t, "find-users", p.Steps[0].Query.OperationRef.String())
			},
		},
		{
			name: "LoadsQueryStepWithInlineOperation",
			path: "pipeline.query.operation-inline.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: crm-api
      operation_type: http
      operation_inline:
        method: GET
        path: /v1/users
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 1)
				require.NotNil(t, p.Steps[0].Query)
				require.Equal(t, "http", p.Steps[0].Query.OperationType)
				require.Equal(t, "GET", p.Steps[0].Query.OperationInline["method"])
				require.Equal(t, "/v1/users", p.Steps[0].Query.OperationInline["path"])
			},
		},
		{
			name: "LoadsQueryStepWithSQLInlineOperation",
			path: "pipeline.query.operation-inline-sql.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_inline:
        sql: SELECT id, email FROM users
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 1)
				require.NotNil(t, p.Steps[0].Query)
				require.Equal(t, "sql", p.Steps[0].Query.OperationType)
				require.Equal(
					t,
					"SELECT id, email FROM users",
					p.Steps[0].Query.OperationInline["sql"],
				)
			},
		},
		{
			name: "LoadsQueryStepWithFTPInlineOperation",
			path: "pipeline.query.operation-inline-ftp.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-ftp
      operation_type: ftp
      operation_inline:
        path: /exports/users.csv
        format: csv
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 1)
				require.NotNil(t, p.Steps[0].Query)
				require.Equal(t, "ftp", p.Steps[0].Query.OperationType)
				require.Equal(t, "/exports/users.csv", p.Steps[0].Query.OperationInline["path"])
				require.Equal(t, "csv", p.Steps[0].Query.OperationInline["format"])
			},
		},
		{
			name: "LoadsFTPInlineFileSourceToFileSink",
			path: "pipeline.query.operation-inline-ftp-file.yaml",
			data: `metadata:
  slug: acme-file-sync
  name: ACME File Sync
steps:
  - kind: query
    slug: fetch-users-file
    query:
      data_source_ref: acme-ftp
      operation_type: ftp
      operation_inline:
        path: /exports/users.pdf
        format: file
  - kind: sink
    slug: upload-users-file
    sink:
      idempotency_class: idempotent_with_key
      input: fetch-users-file
      data_source_ref: acme-http
      operation_type: http
      operation_ref: upload-users-file
      input_contract:
        name: users-file
        representation: file
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 2)
				require.Equal(t, "file", p.Steps[0].Query.OperationInline["format"])
				require.Equal(
					t,
					model.ContractRepresentationFile,
					p.Steps[1].Sink.InputContract.Representation,
				)
				require.Empty(t, p.Steps[1].Sink.InputContract.Fields)
			},
		},
		{
			name: "RejectsFileContractOnSQLTransformInput",
			path: "pipeline.transform.file-input-contract.yaml",
			data: `metadata:
  slug: acme-file-sync
  name: ACME File Sync
steps:
  - kind: query
    slug: fetch-users-file
    query:
      data_source_ref: acme-ftp
      operation_type: ftp
      operation_inline:
        path: /exports/users.pdf
        format: file
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        input: fetch-users-file
      sql: SELECT * FROM input
      output_kind: rows
      input_contract:
        name: users-file
        representation: file
`,
			wantErrContains: "file representation is only supported",
		},
		{
			name: "LoadsQueryStepWithMongoDBInlineOperation",
			path: "pipeline.query.operation-inline-mongodb.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-mongodb
      operation_type: mongodb
      operation_inline:
        operation: find
        collection: users
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 1)
				require.NotNil(t, p.Steps[0].Query)
				require.Equal(t, "mongodb", p.Steps[0].Query.OperationType)
				require.Equal(t, "find", p.Steps[0].Query.OperationInline["operation"])
				require.Equal(t, "users", p.Steps[0].Query.OperationInline["collection"])
			},
		},
		{
			name: "RejectsQueryStepWithMissingOperationTarget",
			path: "pipeline.query.operation-missing-target.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
`,
			wantErrContains: "exactly one of operation_ref or operation_inline",
		},
		{
			name: "RejectsQueryStepWithBothOperationTargets",
			path: "pipeline.query.operation-both-targets.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
      operation_inline:
        sql: SELECT id FROM users
`,
			wantErrContains: "exactly one of operation_ref or operation_inline",
		},
		{
			name: "RejectsQueryStepWithOperationTargetWithoutType",
			path: "pipeline.query.operation-missing-type.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_ref: list-active-users
`,
			wantErrContains: "failed on the 'required' tag",
		},
		{
			name: "LoadsQueryStepWithFTPDataSource",
			path: "pipeline.query.ftp-source.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users-csv
    query:
      data_source_ref: acme-ftp
      operation_type: ftp
      operation_ref: read-users-csv
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 1)
				require.NotNil(t, p.Steps[0].Query)
				require.Equal(t, "acme-ftp", p.Steps[0].Query.DataSourceSlug.String())
				require.Equal(t, "ftp", p.Steps[0].Query.OperationType)
				require.Equal(t, "read-users-csv", p.Steps[0].Query.OperationRef.String())
			},
		},
		{
			name: "LoadsQueryStepWithGitDataSource",
			path: "pipeline.query.git-source.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users-git
    query:
      data_source_ref: acme-git
      operation_type: git
      operation_ref: read-users-json
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 1)
				require.NotNil(t, p.Steps[0].Query)
				require.Equal(t, "acme-git", p.Steps[0].Query.DataSourceSlug.String())
				require.Equal(t, "git", p.Steps[0].Query.OperationType)
				require.Equal(t, "read-users-json", p.Steps[0].Query.OperationRef.String())
			},
		},
		{
			name: "LoadsTransformStepWithInputContract",
			path: "pipeline.transform.input-contract.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
      output_contract:
        fields:
          - name: email
            type: string
            required: true
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        input: fetch-users
      sql: "SELECT email FROM input"
      output_kind: rows
      input_contract:
        fields:
          - name: email
            type: string
            required: true
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 2)
				require.NotNil(t, p.Steps[1].Transform)
				require.NotNil(t, p.Steps[1].Transform.InputContract)
				require.Len(t, p.Steps[1].Transform.InputContract.Fields, 1)
				require.Equal(
					t,
					model.ContractFieldTypeValueString,
					p.Steps[1].Transform.InputContract.Fields[0].Type,
				)
			},
		},
		{
			name: "LoadsYAMLPipeline",
			path: "single-query.pipeline.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
      parameters:
        limit: 100
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Equal(t, "fetch-users", string(p.Steps[0].Slug))
				require.Equal(t, int64(100), p.Steps[0].Query.Parameters["limit"])
			},
		},
		{
			name: "LoadsJSONPipeline",
			path: "single-query.pipeline.json",
			data: `{
  "metadata": {
    "slug": "acme-users-sync",
    "name": "ACME Users Sync"
  },
  "steps": [
    {
      "kind": "query",
      "slug": "fetch-users",
      "query": {
        "data_source_ref": "acme-postgres",
        "operation_type": "sql",
        "operation_ref": "list-active-users",
        "parameters": {
          "limit": 100
        }
      }
    }
  ]
}`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Equal(t, "acme-users-sync", string(p.Metadata.Slug))
			},
		},
		{
			name:            "RejectsUnsupportedExtension",
			path:            "pipeline.toml",
			data:            `kind = "Pipeline"`,
			wantErrContains: "unsupported pipeline file format",
		},
		{
			name: "RejectsOrphanStepInMultiStepPipeline",
			path: "pipeline.orphan.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
  - kind: query
    slug: notify-users
    query:
      data_source_ref: acme-email
      operation_type: sql
      operation_ref: send-bulk-email
      parameters:
        subject: hello
`,
			wantErrContains: "upstream or downstream step",
		},
		{
			name: "AllowsQueryStepConsumedOnlyByDownstreamStepInput",
			path: "pipeline.step-input.downstream-only.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
  - kind: query
    slug: stage-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-staged-users
      output_contract:
        fields:
          - name: email
            type: string
            required: true
  - kind: sink
    slug: acme-users-sync
    sink:
      idempotency_class: idempotent_with_key
      input: stage-users
      data_source_ref: acme-sink
      operation_type: sql
      operation_ref: upsert-users
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 3)
				require.Equal(t, "stage-users", string(p.Steps[2].Sink.Input.FromStep))
			},
		},
		{
			name: "LoadsMultiStepPipelineWithRefs",
			path: "pipeline.multi-step.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
  - kind: query
    slug: notify-users
    query:
      data_source_ref: acme-email
      operation_type: sql
      operation_ref: send-bulk-email
      parameters:
        recipients: $steps.fetch-users.rows
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 2)
				require.Equal(
					t,
					"$steps.fetch-users.rows",
					p.Steps[1].Query.Parameters["recipients"],
				)
			},
		},
		{
			name: "LoadsPipelineWithStepInput",
			path: "pipeline.step-input.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
      output_contract:
        fields:
          - name: email
            type: string
            required: true
  - kind: sink
    slug: acme-users-sync
    sink:
      idempotency_class: idempotent_with_key
      input: fetch-users
      data_source_ref: acme-sink
      operation_type: sql
      operation_ref: upsert-users
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 2)
				require.NotNil(t, p.Steps[1].Sink)
				require.NotNil(t, p.Steps[1].Sink.Input)
				require.Equal(t, "fetch-users", string(p.Steps[1].Sink.Input.FromStep))
			},
		},
		{
			name: "LoadsStepInputModeWithoutDollarStepRefs",
			path: "pipeline.step-input.no-refs.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
  - kind: query
    slug: notify-users
    query:
      data_source_ref: acme-email
      operation_type: sql
      operation_ref: send-bulk-email
      parameters:
        recipients: $steps.fetch-users.rows
      output_contract:
        fields:
          - name: email
            type: string
            required: true
  - kind: sink
    slug: acme-users-sync
    sink:
      idempotency_class: idempotent_with_key
      input: notify-users
      data_source_ref: acme-sink
      operation_type: sql
      operation_ref: upsert-users
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 3)
				require.NotNil(t, p.Steps[2].Sink)
				require.NotNil(t, p.Steps[2].Sink.Input)
				require.Equal(t, "notify-users", string(p.Steps[2].Sink.Input.FromStep))
			},
		},
		{
			name: "RejectsDisconnectedTransformNodeInStepInputGraph",
			path: "pipeline.step-input.disconnected-transform.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        input: missing-step
      sql: SELECT id AS id FROM input
      output_kind: rows
  - kind: sink
    slug: acme-users-sync
    sink:
      idempotency_class: idempotent_with_key
      input: fetch-users
      data_source_ref: acme-sink
      operation_type: sql
      operation_ref: upsert-users
`,
			wantErrContains: "must reference step",
		},
		{
			name: "RejectsSinkWithoutOperationDelivery",
			path: "pipeline.sink.noop.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
  - kind: sink
    slug: acme-users-sync
    sink:
      idempotency_class: idempotent_with_key
      input: fetch-users
`,
			wantErrContains: "sink target requires operation_type",
		},
		{
			name: "RejectsSinkWithoutDataSourceSlug",
			path: "pipeline.sink.missing-data-source.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
  - kind: sink
    slug: acme-users-sync
    sink:
      idempotency_class: idempotent_with_key
      input: fetch-users
      operation_type: sql
      operation_ref: upsert-users
`,
			wantErrContains: "sink target requires data_source_ref",
		},
		{
			name: "RejectsSinkWithoutExplicitInputFromStep",
			path: "pipeline.step-input.disconnected-sink.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        input: fetch-users
      sql: SELECT id AS id FROM input
      output_kind: rows
  - kind: sink
    slug: acme-users-sync
    sink:
      idempotency_class: idempotent_with_key
      data_source_ref: acme-sink
      operation_type: sql
      operation_ref: upsert-users
`,
			wantErrContains: "must reference at least one upstream step output",
		},
		{
			name: "RejectsQueryRowsStepInputIntoJSONSink",
			path: "pipeline.document-sink-mismatch.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
      output_contract:
        representation: rows
        fields:
          - name: email
            type: string
            required: true
  - kind: sink
    slug: acme-users-sync
    sink:
      idempotency_class: idempotent_with_key
      input: fetch-users
      data_source_ref: acme-sink
      operation_type: sql
      operation_ref: upsert-users
      input_contract:
        name: sink-input
        representation: json
        root_shape: object
        fields:
          - name: email
            type: string
            required: true
`,
			wantErrContains: ContractReasonRepresentationMismatch.String(),
		},
		{
			name: "RejectsMissingTransformInputFromStep",
			path: "pipeline.invalid-step-input.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: transform
    slug: normalize-users
    transform:
      sql: SELECT id AS id FROM input
      output_kind: rows
`,
			wantErrContains: "must reference at least one upstream step output",
		},
		{
			name: "RejectsTransformWithoutExplicitInputFromStep",
			path: "pipeline.invalid-transform-step-input.strict.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
  - kind: transform
    slug: normalize-users
    transform:
      sql: SELECT id AS id FROM input
      output_kind: rows
`,
			wantErrContains: "must reference at least one upstream step output via inputs",
		},
		{
			name: "RejectsMissingSinkInputFromStep",
			path: "pipeline.invalid-sink-step-input.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: sink
    slug: acme-users-sync
    sink:
      idempotency_class: idempotent_with_key
      data_source_ref: acme-sink
      operation_type: sql
      operation_ref: upsert-users
`,
			wantErrContains: "must reference at least one upstream step output",
		},
		{
			name: "RejectsTransformFromStepReferencingSinkStep",
			path: "pipeline.transform-from-sink.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
  - kind: sink
    slug: deliver-users
    sink:
      idempotency_class: idempotent_with_key
      input: fetch-users
      data_source_ref: acme-sink
      operation_type: sql
      operation_ref: upsert-users
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        input: deliver-users
      sql: SELECT id AS id FROM input
      output_kind: rows
`,
			wantErrContains: "must be a query or transform step",
		},
		{
			name: "RejectsSinkFromStepReferencingSinkStep",
			path: "pipeline.sink-from-sink.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
  - kind: sink
    slug: deliver-users
    sink:
      idempotency_class: idempotent_with_key
      input: fetch-users
      data_source_ref: acme-sink
      operation_type: sql
      operation_ref: upsert-users
  - kind: sink
    slug: deliver-users-copy
    sink:
      idempotency_class: idempotent_with_key
      input: deliver-users
      data_source_ref: acme-sink
      operation_type: sql
      operation_ref: upsert-users
`,
			wantErrContains: "must be a query or transform step",
		},
		{
			name: "RejectsTransformWithoutInputFromStepRegardlessOfPreviousStepKind",
			path: "pipeline.transform-implicit-from-sink.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
  - kind: sink
    slug: deliver-users
    sink:
      idempotency_class: idempotent_with_key
      input: fetch-users
      data_source_ref: acme-sink
      operation_type: sql
      operation_ref: upsert-users
  - kind: transform
    slug: normalize-users
    transform:
      sql: SELECT id AS id FROM input
      output_kind: rows
`,
			wantErrContains: "must reference at least one upstream step output via inputs",
		},
		{
			name: "RejectsCueWithoutPipelineValue",
			path: "pipeline.invalid.cue",
			data: `package pipeline

value: {
}
`,
			wantErrContains: `".cue"`,
		},
		{
			name: "RejectsInvalidSlugViaValidator",
			path: "pipeline.invalid.yaml",
			data: `metadata:
  slug: BAD SLUG
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
`,
			wantErrContains: "Slug",
		},
		{
			name: "LoadsPipelineWithContractFieldType",
			path: "pipeline.run-mode.contracts.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
      output_contract:
        representation: rows
        fields:
          - name: email
            type: string
            required: true
  - kind: sink
    slug: acme-users-sync
    sink:
      idempotency_class: idempotent_with_key
      input: fetch-users
      data_source_ref: acme-sink
      operation_type: sql
      operation_ref: upsert-users
      input_contract:
        name: users-contract
        representation: rows
        fields:
          - name: email
            type: string
            required: true
            nullable: false
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.Len(t, p.Steps, 2)
				require.NotNil(t, p.Steps[1].Sink)
				require.NotNil(t, p.Steps[1].Sink.InputContract)
				require.Equal(
					t,
					model.ContractRepresentationRows,
					p.Steps[1].Sink.InputContract.Representation,
				)
				require.Equal(
					t,
					model.ContractRootShapeUnknown,
					p.Steps[1].Sink.InputContract.RootShape,
				)
				require.Len(t, p.Steps[1].Sink.InputContract.Fields, 1)
				require.Equal(
					t,
					model.ContractFieldTypeValueString,
					p.Steps[1].Sink.InputContract.Fields[0].Type,
				)
				require.False(t, p.Steps[1].Sink.InputContract.Fields[0].Nullable)
			},
		},
		{
			name: "RejectsContractRootShapeWithoutJSONRepresentation",
			path: "pipeline.invalid-contract-root-shape.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
  - kind: sink
    slug: acme-users-sync
    sink:
      idempotency_class: idempotent_with_key
      input: fetch-users
      data_source_ref: acme-sink
      operation_type: sql
      operation_ref: upsert-users
      input_contract:
        name: users-contract
        representation: rows
        root_shape: object
        fields:
          - name: email
            type: string
            required: true
`,
			wantErrContains: ContractReasonInvalidDefinition.String(),
		},
		{
			name: "RejectsUnknownContractFieldType",
			path: "pipeline.invalid-contract-field-type.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
      output_contract:
        fields:
          - name: email
            type: uuid
            required: true
`,
			wantErrContains: "does not belong to ContractFieldType values",
		},
		{
			name: "RejectsIncompatibleTransformToSinkContractsInStrictMode",
			path: "pipeline.contract-mismatch.strict.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-users
      output_contract:
        fields:
          - name: email
            type: string
            required: true
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        input: fetch-users
      sql: SELECT email FROM input
      output_kind: rows
      output_contract:
        fields:
          - name: email
            type: string
            required: true
  - kind: sink
    slug: acme-users-sync
    sink:
      idempotency_class: idempotent_with_key
      input: normalize-users
      data_source_ref: acme-sink
      operation_type: sql
      operation_ref: upsert-users
      input_contract:
        name: sink-input
        representation: json
        root_shape: object
        fields:
          - name: email
            type: string
            required: true
`,
			wantErrContains: "contract_representation_mismatch",
		},
		{
			name: "RejectsIncompatibleTransformToTransformContracts",
			path: "pipeline.transform-chain.contract-mismatch.strict.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-users
      output_contract:
        fields:
          - name: email
            type: string
            required: true
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        input: fetch-users
      sql: SELECT email FROM input
      output_kind: rows
      output_contract:
        fields:
          - name: email
            type: string
            required: true
  - kind: transform
    slug: filter-users
    transform:
      inputs:
        input: normalize-users
      sql: SELECT email FROM input
      output_kind: rows
      input_contract:
        fields:
          - name: email
            type: integer
            required: true
  - kind: sink
    slug: acme-users-sync
    sink:
      idempotency_class: idempotent_with_key
      input: filter-users
      data_source_ref: acme-sink
      operation_type: sql
      operation_ref: upsert-users
`,
			wantErrContains: "contract_type_mismatch",
		},
		{
			name: "RejectsIncompatibleQueryToTransformContracts",
			path: "pipeline.query-transform.contract-mismatch.strict.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
      output_contract:
        fields:
          - name: email
            type: string
            required: true
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        input: fetch-users
      sql: SELECT email FROM input
      output_kind: rows
      input_contract:
        fields:
          - name: email
            type: integer
            required: true
  - kind: sink
    slug: acme-users-sync
    sink:
      idempotency_class: idempotent_with_key
      input: normalize-users
      data_source_ref: acme-sink
      operation_type: sql
      operation_ref: upsert-users
`,
			wantErrContains: "contract_type_mismatch",
		},
		{
			name: "RejectsContractFieldWithDoubleDotPath",
			path: "pipeline.transform.double-dot-path.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
      output_contract:
        fields:
          - name: email
            type: string
            required: true
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        input: fetch-users
      sql: "SELECT profile FROM input"
      output_kind: rows
      output_contract:
        fields:
          - name: "profile..email"
            type: string
            required: true
`,
			wantErrContains: "invalid field path",
		},
		{
			name: "RejectsContractFieldWithLeadingDotPath",
			path: "pipeline.transform.leading-dot-path.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: transform
    slug: normalize-users
    transform:
      sql: "SELECT profile FROM input"
      output_kind: rows
      output_contract:
        fields:
          - name: ".email"
            type: string
            required: true
`,
			wantErrContains: "invalid field path",
		},
		{
			name: "LoadsContractWithValidNestedPath",
			path: "pipeline.transform.nested-path.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
      output_contract:
        fields:
          - name: profile
            type: object
            required: true
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        input: fetch-users
      sql: "SELECT profile FROM input"
      output_kind: rows
      output_contract:
        fields:
          - name: "profile.email"
            type: string
            required: true
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.NotNil(t, p.Steps[1].Transform.OutputContract)
				require.Equal(
					t,
					"profile.email",
					p.Steps[1].Transform.OutputContract.Fields[0].Name,
				)
			},
		},
		{
			name: "LoadsContractWithValidArrayNestedPath",
			path: "pipeline.transform.array-path.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
      output_contract:
        fields:
          - name: items
            type: array
            required: true
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        input: fetch-users
      sql: "SELECT items FROM input"
      output_kind: rows
      output_contract:
        fields:
          - name: "items[].sku"
            type: string
            required: true
`,
			assertFn: func(t *testing.T, p *pipelinespec.Pipeline) {
				require.NotNil(t, p.Steps[1].Transform.OutputContract)
				require.Equal(t, "items[].sku", p.Steps[1].Transform.OutputContract.Fields[0].Name)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pipeline, err := Load(tc.path, []byte(tc.data))
			if tc.wantErrContains != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErrContains)
				if tc.wantErrIs != nil {
					require.ErrorIs(t, err, tc.wantErrIs)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, pipeline)
			if tc.assertFn != nil {
				tc.assertFn(t, pipeline)
			}
		})
	}
}

func TestLoadMultiSourceYAMLExample(t *testing.T) {
	t.Parallel()

	spec, err := os.ReadFile("../../../examples/multi-source-roots.example.yaml")
	require.NoError(t, err)

	pipeline, err := Load("multi-source-roots.example.yaml", spec)
	require.NoError(t, err)
	require.Len(t, pipeline.Steps, 4)
	require.Len(t, pipeline.Steps[2].Transform.Inputs, 2)
}

func TestValidatePipeline_ContractFirstStepInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		path            string
		data            string
		wantErrContains string
	}{
		{
			name: "RejectsIncompatibleTransformToSinkContracts",
			path: "pipeline.contract-mismatch.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-users
      output_contract:
        fields:
          - name: email
            type: string
            required: true
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        input: fetch-users
      sql: SELECT email FROM input
      output_kind: rows
      output_contract:
        fields:
          - name: email
            type: string
            required: true
  - kind: sink
    slug: acme-users-sync
    sink:
      idempotency_class: idempotent_with_key
      input: normalize-users
      data_source_ref: acme-sink
      operation_type: sql
      operation_ref: upsert-users
      input_contract:
        name: sink-input
        representation: json
        root_shape: object
        fields:
          - name: email
            type: string
            required: true
`,
			wantErrContains: "contract_representation_mismatch",
		},
		{
			name: "RejectsIncompatibleTransformToTransformContracts",
			path: "pipeline.contract-mismatch.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-users
      output_contract:
        fields:
          - name: id
            type: integer
            required: true
  - kind: transform
    slug: project-user-id
    transform:
      inputs:
        input: fetch-users
      sql: SELECT id AS user_id FROM input
      output_kind: rows
      output_contract:
        fields:
          - name: user_id
            type: integer
            required: true
  - kind: transform
    slug: project-user-id-copy
    transform:
      inputs:
        input: project-user-id
      sql: SELECT user_id AS user_id FROM input
      output_kind: rows
      input_contract:
        fields:
          - name: user_id
            type: string
            required: true
  - kind: sink
    slug: acme-users-sync
    sink:
      idempotency_class: idempotent_with_key
      input: project-user-id-copy
      data_source_ref: acme-sink
      operation_type: sql
      operation_ref: upsert-users
`,
			wantErrContains: "contract_type_mismatch",
		},
		{
			name: "RejectsIncompatibleQueryToTransformContracts",
			path: "pipeline.contract-mismatch.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-active-users
      output_contract:
        fields:
          - name: email
            type: string
            required: true
  - kind: transform
    slug: normalize-users
    transform:
      inputs:
        input: fetch-users
      sql: SELECT email FROM input
      output_kind: rows
      input_contract:
        fields:
          - name: email
            type: integer
            required: true
  - kind: sink
    slug: acme-users-sync
    sink:
      idempotency_class: idempotent_with_key
      input: normalize-users
      data_source_ref: acme-sink
      operation_type: sql
      operation_ref: upsert-users
`,
			wantErrContains: "contract_type_mismatch",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Contract compatibility is enforced when both sides declare contracts.
			pipeline, err := Load(tc.path, []byte(tc.data))
			require.Error(t, err)
			require.Nil(t, pipeline)
			require.ErrorIs(t, err, ErrContractMismatch)
			require.ErrorContains(t, err, tc.wantErrContains)
		})
	}
}

func TestValidatePipeline_ExplicitEdgeContracts(t *testing.T) {
	t.Parallel()

	t.Run("RejectsEdgeWithoutProducerOrConsumerContract", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-users",
					},
				},
				{
					Kind: "transform",
					Slug: "normalize-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{"input": {FromStep: "fetch-users"}}, SQL: "SELECT email FROM input",
						OutputKind: model.TransformOutputKindRows,
					},
				},
				{
					Kind: "sink",
					Slug: "acme-users-sync",
					Sink: testSinkSpec(
						"normalize-users",
						&payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: false,
							}},
						},
					),
				},
			},
		}

		err := validateStepInputCompatibility(pipeline)
		require.ErrorIs(t, err, ErrContractMismatch)
		require.ErrorContains(t, err, "fetch-users->normalize-users")
		require.ErrorContains(t, err, ContractReasonMissingContract.String())
	})

	t.Run("AllowsConsumerOnlyContractByInferringProducerContract", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-users",
					},
				},
				{
					Kind: "transform",
					Slug: "normalize-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{"input": {FromStep: "fetch-users"}}, SQL: "SELECT email FROM input",
						InputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
						OutputKind: model.TransformOutputKindRows,
					},
				},
				{
					Kind: "sink",
					Slug: "acme-users-sync",
					Sink: testSinkSpec(
						"normalize-users",
						&payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: false,
							}},
						},
					),
				},
			},
		}

		err := validateStepInputCompatibility(pipeline)
		require.NoError(t, err)
	})

	t.Run("RejectsConsumerOnlyContractWhenProducerOutputKindConflicts", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-users",
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "transform",
					Slug: "normalize-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs:     map[string]pipelinespec.StepInput{"input": {FromStep: "fetch-users"}},
						SQL:        "SELECT email FROM input",
						OutputKind: model.TransformOutputKindRows,
					},
				},
				{
					Kind: "sink",
					Slug: "acme-users-sync",
					Sink: testSinkSpec(
						"normalize-users",
						&payload.Contract{
							Representation: model.ContractRepresentationJSON,
							RootShape:      model.ContractRootShapeObject,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					),
				},
			},
		}

		err := validateStepInputCompatibility(pipeline)
		require.ErrorIs(t, err, ErrContractMismatch)
		require.ErrorContains(t, err, "normalize-users->acme-users-sync")
		require.ErrorContains(t, err, ContractReasonRepresentationMismatch.String())
	})

	t.Run("RejectsExplicitProducerContractWhenOutputKindConflicts", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-users",
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "transform",
					Slug: "normalize-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs:     map[string]pipelinespec.StepInput{"input": {FromStep: "fetch-users"}},
						SQL:        "SELECT email FROM input",
						OutputKind: model.TransformOutputKindJSON,
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "sink",
					Slug: "acme-users-sync",
					Sink: testSinkSpec(
						"normalize-users",
						&payload.Contract{
							Representation: model.ContractRepresentationJSON,
							RootShape:      model.ContractRootShapeArray,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					),
				},
			},
		}

		err := validateStepInputCompatibility(pipeline)
		require.ErrorIs(t, err, ErrContractMismatch)
		require.ErrorContains(t, err, "normalize-users->acme-users-sync")
		require.ErrorContains(t, err, ContractReasonRepresentationMismatch.String())
		require.ErrorContains(t, err, "producer_representation")
	})

	t.Run("RejectsConsumerOnlyContractWhenQueryOperationRepresentationConflicts", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-mongo",
						OperationType:  "mongodb",
						OperationRef:   "list-users",
					},
				},
				{
					Kind: "sink",
					Slug: "acme-users-sync",
					Sink: testSinkSpec(
						"fetch-users",
						&payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					),
				},
			},
		}

		err := validateStepInputCompatibility(pipeline)
		require.ErrorIs(t, err, ErrContractMismatch)
		require.ErrorContains(t, err, "fetch-users->acme-users-sync")
		require.ErrorContains(t, err, ContractReasonRepresentationMismatch.String())
	})

	t.Run("AllowsMongoDBQueryDocumentsAsTransformRowInput", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-mongo",
						OperationType:  "mongodb",
						OperationRef:   "list-users",
					},
				},
				{
					Kind: "transform",
					Slug: "normalize-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{"input": {FromStep: "fetch-users"}},
						SQL:    "SELECT email FROM input",
						InputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
						OutputKind: model.TransformOutputKindRows,
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "sink",
					Slug: "acme-users-sync",
					Sink: testSinkSpec(
						"normalize-users",
						&payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					),
				},
			},
		}

		require.NoError(t, validateStepInputCompatibility(pipeline))
	})

	t.Run("AllowsMongoDBQueryFanOutToTransformRowsAndJSONSink", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-mongo",
						OperationType:  "mongodb",
						OperationRef:   "list-users",
					},
				},
				{
					Kind: "transform",
					Slug: "normalize-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{"input": {FromStep: "fetch-users"}},
						SQL:    "SELECT email FROM input",
						InputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
						OutputKind: model.TransformOutputKindRows,
					},
				},
				{
					Kind: "sink",
					Slug: "archive-users",
					Sink: testSinkSpec(
						"fetch-users",
						&payload.Contract{
							Representation: model.ContractRepresentationJSON,
							RootShape:      model.ContractRootShapeArray,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					),
				},
			},
		}

		require.NoError(t, validateStepInputCompatibility(pipeline))
	})

	t.Run("RejectsOpaqueFileQueryOutputToSQLTransformRowsInput", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-file-sync", Name: "ACME File Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users-file",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-ftp",
						OperationType:  "ftp",
						OperationInline: map[string]any{
							"path":   "/exports/users.pdf",
							"format": "file",
						},
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationFile,
						},
					},
				},
				{
					Kind: "transform",
					Slug: "normalize-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{
							"input": {FromStep: "fetch-users-file"},
						},
						SQL:        "SELECT email FROM input",
						OutputKind: model.TransformOutputKindRows,
						InputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					},
				},
			},
		}

		err := validateStepInputCompatibility(pipeline)
		require.ErrorIs(t, err, ErrContractMismatch)
		require.ErrorContains(t, err, "fetch-users-file->normalize-users")
		require.ErrorContains(t, err, ContractReasonRepresentationMismatch.String())
	})

	t.Run("RejectsMultipartFormQueryOutputToSQLTransformRowsInput", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-form-sync", Name: "ACME Form Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "receive-form",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-http",
						OperationType:  "http",
						OperationRef:   "receive-form",
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationMultipartForm,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "transform",
					Slug: "normalize-form",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{
							"input": {FromStep: "receive-form"},
						},
						SQL:        "SELECT email FROM input",
						OutputKind: model.TransformOutputKindRows,
						InputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					},
				},
			},
		}

		err := validateStepInputCompatibility(pipeline)
		require.ErrorIs(t, err, ErrContractMismatch)
		require.ErrorContains(t, err, "receive-form->normalize-form")
		require.ErrorContains(t, err, ContractReasonRepresentationMismatch.String())
	})

	t.Run("AllowsConsumerOnlyContractWhenHTTPQueryMediaTypeIsDynamic", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-http",
						OperationType:  "http",
						OperationRef:   "list-users",
					},
				},
				{
					Kind: "sink",
					Slug: "acme-users-sync",
					Sink: testSinkSpec(
						"fetch-users",
						&payload.Contract{
							Representation: model.ContractRepresentationJSON,
							RootShape:      model.ContractRootShapeObject,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					),
				},
			},
		}

		require.NoError(t, validateStepInputCompatibility(pipeline))
	})

	for _, operationType := range []string{"ftp", "git"} {
		t.Run("RejectsSaved"+operationType+"QueryConsumerOnlyFileContract", func(t *testing.T) {
			t.Parallel()

			pipeline := &pipelinespec.Pipeline{
				Metadata: pipelinespec.Metadata{Slug: "acme-file-sync", Name: "ACME File Sync"},
				Steps: []pipelinespec.Step{
					{
						Kind: "query",
						Slug: "fetch-users",
						Query: &pipelinespec.QueryRef{
							DataSourceSlug: "acme-files",
							OperationType:  operationType,
							OperationRef:   "download-users",
						},
					},
					{
						Kind: "sink",
						Slug: "upload-users",
						Sink: testSinkSpec(
							"fetch-users",
							&payload.Contract{
								Representation: model.ContractRepresentationFile,
							},
						),
					},
				},
			}

			err := validateStepInputCompatibility(pipeline)
			require.ErrorIs(t, err, ErrContractMismatch)
			require.ErrorContains(t, err, "fetch-users->upload-users")
			require.ErrorContains(t, err, ContractReasonRepresentationMismatch.String())
		})
	}

	for _, operationType := range []string{"ftp", "git"} {
		t.Run("AllowsSaved"+operationType+"QueryExplicitFileContract", func(t *testing.T) {
			t.Parallel()

			pipeline := &pipelinespec.Pipeline{
				Metadata: pipelinespec.Metadata{Slug: "acme-file-sync", Name: "ACME File Sync"},
				Steps: []pipelinespec.Step{
					{
						Kind: "query",
						Slug: "fetch-users",
						Query: &pipelinespec.QueryRef{
							DataSourceSlug: "acme-files",
							OperationType:  operationType,
							OperationRef:   "download-users",
							OutputContract: &payload.Contract{
								Representation: model.ContractRepresentationFile,
							},
						},
					},
					{
						Kind: "sink",
						Slug: "upload-users",
						Sink: testSinkSpec(
							"fetch-users",
							&payload.Contract{
								Representation: model.ContractRepresentationFile,
							},
						),
					},
				},
			}

			require.NoError(t, validateStepInputCompatibility(pipeline))
		})
	}

	t.Run("AllowsProducerOnlyContractByInferringConsumerContract", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-users",
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "transform",
					Slug: "normalize-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{"input": {FromStep: "fetch-users"}}, SQL: "SELECT email FROM input",
						OutputKind: model.TransformOutputKindRows,
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "sink",
					Slug: "acme-users-sync",
					Sink: testSinkSpec(
						"normalize-users",
						&payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					),
				},
			},
		}

		err := validateStepInputCompatibility(pipeline)
		require.NoError(t, err)
	})

	t.Run("DetectsConflictingFanOutInferredProducerContracts", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-users",
					},
				},
				{
					Kind: "sink",
					Slug: "write-integer-ids",
					Sink: testSinkSpec(
						"fetch-users",
						&payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "id",
								Type:     model.ContractFieldTypeValueInteger,
								Required: true,
							}},
						},
					),
				},
				{
					Kind: "sink",
					Slug: "write-string-ids",
					Sink: testSinkSpec(
						"fetch-users",
						&payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "id",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					),
				},
			},
		}

		err := validateStepInputCompatibility(pipeline)
		require.ErrorIs(t, err, ErrContractMismatch)
		require.ErrorContains(t, err, "fetch-users")
		require.ErrorContains(t, err, "write-integer-ids")
		require.ErrorContains(t, err, "write-string-ids")
		require.ErrorContains(t, err, "conflicting inferred output contract")
	})

	t.Run("ReportsAggregateFanOutConflictAsEdgeDiagnostic", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-users",
					},
				},
				{
					Kind: "sink",
					Slug: "write-integer-ids",
					Sink: testSinkSpec(
						"fetch-users",
						&payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "id",
								Type:     model.ContractFieldTypeValueInteger,
								Required: true,
							}},
						},
					),
				},
				{
					Kind: "sink",
					Slug: "write-string-ids",
					Sink: testSinkSpec(
						"fetch-users",
						&payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "id",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					),
				},
			},
		}

		diagnostics := AggregateStepInputCompatibilityDiagnostics(pipeline)
		require.Len(t, diagnostics, 1)
		require.False(t, diagnostics[0].Compatible)
		require.Equal(t, must.Slug("fetch-users"), diagnostics[0].FromStepSlug)
		require.Equal(t, must.Slug("write-string-ids"), diagnostics[0].ToStepSlug)
		require.Equal(t, ContractReasonTypeMismatch, diagnostics[0].ReasonCode)
		require.Equal(t, "id", diagnostics[0].Expected["field"])
	})

	t.Run("AllowsConflictingOptionalFanOutFieldsToBeOmitted", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-users",
					},
				},
				{
					Kind: "sink",
					Slug: "write-string-metadata",
					Sink: testSinkSpec(
						"fetch-users",
						&payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "metadata",
								Type:     model.ContractFieldTypeValueString,
								Required: false,
							}},
						},
					),
				},
				{
					Kind: "sink",
					Slug: "write-json-metadata",
					Sink: testSinkSpec(
						"fetch-users",
						&payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "metadata",
								Type:     model.ContractFieldTypeValueObject,
								Required: false,
							}},
						},
					),
				},
			},
		}

		require.NoError(t, validateStepInputCompatibility(pipeline))
	})

	t.Run("DetectsOptionalFanOutFieldConflictWhenAnotherEdgeRequiresIt", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-users",
					},
				},
				{
					Kind: "sink",
					Slug: "write-string-metadata",
					Sink: testSinkSpec(
						"fetch-users",
						&payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "metadata",
								Type:     model.ContractFieldTypeValueString,
								Required: false,
							}},
						},
					),
				},
				{
					Kind: "sink",
					Slug: "write-json-metadata",
					Sink: testSinkSpec(
						"fetch-users",
						&payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "metadata",
								Type:     model.ContractFieldTypeValueObject,
								Required: false,
							}},
						},
					),
				},
				{
					Kind: "sink",
					Slug: "require-json-metadata",
					Sink: testSinkSpec(
						"fetch-users",
						&payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "metadata",
								Type:     model.ContractFieldTypeValueObject,
								Required: true,
							}},
						},
					),
				},
			},
		}

		err := validateStepInputCompatibility(pipeline)
		require.ErrorIs(t, err, ErrContractMismatch)
		require.ErrorContains(t, err, "fetch-users")
		require.ErrorContains(t, err, "write-string-metadata")
		require.ErrorContains(t, err, "require-json-metadata")
		require.ErrorContains(t, err, "conflicting inferred output contract")
	})

	t.Run("AllowsMultiInputTransformConsumerOnlyContractsPerNamedEdge", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-orders-sync", Name: "ACME Orders Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-users",
					},
				},
				{
					Kind: "query",
					Slug: "fetch-orders",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-orders",
					},
				},
				{
					Kind: "transform",
					Slug: "join-orders",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{
							"orders": {FromStep: "fetch-orders"},
							"users":  {FromStep: "fetch-users"},
						},
						SQL:        "SELECT users.email, orders.total FROM users JOIN orders ON users.id = orders.user_id",
						OutputKind: model.TransformOutputKindRows,
						InputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{
								{Name: "orders.total", Type: model.ContractFieldTypeValueNumber, Required: true},
								{Name: "users.email", Type: model.ContractFieldTypeValueString, Required: true},
							},
						},
					},
				},
			},
		}

		err := validateStepInputCompatibility(pipeline)
		require.NoError(t, err)
	})

	t.Run("RejectsMultiInputTransformInputContractWithoutNamedPrefix", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-orders-sync", Name: "ACME Orders Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-users",
					},
				},
				{
					Kind: "query",
					Slug: "fetch-orders",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-orders",
					},
				},
				{
					Kind: "transform",
					Slug: "join-orders",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{
							"orders": {FromStep: "fetch-orders"},
							"users":  {FromStep: "fetch-users"},
						},
						SQL:        "SELECT users.email, orders.total FROM users JOIN orders ON users.id = orders.user_id",
						OutputKind: model.TransformOutputKindRows,
						InputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{
								{Name: "email", Type: model.ContractFieldTypeValueString, Required: true},
							},
						},
					},
				},
			},
		}

		err := Validate(pipeline)
		require.Error(t, err)
		require.ErrorContains(t, err, `step[2] "join-orders" input_contract`)
		require.ErrorContains(t, err, `field "email" must be prefixed`)
		require.ErrorContains(t, err, "orders.")
		require.ErrorContains(t, err, "users.")
	})
}

func TestValidatePipeline_QueryParameterReferences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		pipeline        *pipelinespec.Pipeline
		wantErrContains string
	}{
		{
			name: "AcceptsPriorQueryReference",
			pipeline: &pipelinespec.Pipeline{
				Metadata: pipelinespec.Metadata{Slug: "acme-source-inspection", Name: "ACME Source Inspection"},
				Steps: []pipelinespec.Step{
					{
						Kind: "query",
						Slug: "fetch-users",
						Query: &pipelinespec.QueryRef{
							DataSourceSlug: "acme-postgres",
							OperationType:  "sql",
							OperationRef:   "list-users",
							OutputContract: &payload.Contract{
								Fields: []payload.ContractField{{
									Name:     "id",
									Type:     model.ContractFieldTypeValueInteger,
									Required: true,
								}},
							},
						},
					},
					{
						Kind: "query",
						Slug: "fetch-profiles",
						Query: &pipelinespec.QueryRef{
							DataSourceSlug: "acme-crm",
							OperationType:  "http",
							OperationInline: map[string]any{
								"method": "GET",
								"path":   "/profiles",
							},
							Parameters: map[string]any{
								"user_rows": "$steps.fetch-users.rows",
							},
						},
					},
				},
			},
		},
		{
			name: "RejectsMissingReference",
			pipeline: &pipelinespec.Pipeline{
				Metadata: pipelinespec.Metadata{Slug: "acme-source-inspection", Name: "ACME Source Inspection"},
				Steps: []pipelinespec.Step{
					{
						Kind: "query",
						Slug: "fetch-users",
						Query: &pipelinespec.QueryRef{
							DataSourceSlug: "acme-postgres",
							OperationType:  "sql",
							OperationRef:   "list-users",
							Parameters: map[string]any{
								"user_rows": "$steps.missing-users.rows",
							},
						},
					},
				},
			},
			wantErrContains: "must reference step",
		},
		{
			name: "AcceptsLaterReference",
			pipeline: &pipelinespec.Pipeline{
				Metadata: pipelinespec.Metadata{Slug: "acme-source-inspection", Name: "ACME Source Inspection"},
				Steps: []pipelinespec.Step{
					{
						Kind: "query",
						Slug: "fetch-profiles",
						Query: &pipelinespec.QueryRef{
							DataSourceSlug: "acme-crm",
							OperationType:  "http",
							OperationInline: map[string]any{
								"method": "GET",
								"path":   "/profiles",
							},
							Parameters: map[string]any{
								"user_rows": "$steps.fetch-users.rows",
							},
						},
					},
					{
						Kind: "query",
						Slug: "fetch-users",
						Query: &pipelinespec.QueryRef{
							DataSourceSlug: "acme-postgres",
							OperationType:  "sql",
							OperationRef:   "list-users",
							OutputContract: &payload.Contract{
								Fields: []payload.ContractField{{
									Name:     "id",
									Type:     model.ContractFieldTypeValueInteger,
									Required: true,
								}},
							},
						},
					},
					{
						Kind: "sink",
						Slug: "load-users",
						Sink: &pipelinespec.SinkSpec{
							IdempotencyClass: pipelinespec.SinkIdempotencyClassIdempotentWithKey,
							Input:            &pipelinespec.StepInput{FromStep: "fetch-users"},
							DataSourceSlug:   "acme-sink",
							OperationType:    "sql",
							OperationRef:     "upsert-users",
						},
					},
				},
			},
		},
		{
			name: "RejectsSinkReference",
			pipeline: &pipelinespec.Pipeline{
				Metadata: pipelinespec.Metadata{Slug: "acme-source-inspection", Name: "ACME Source Inspection"},
				Steps: []pipelinespec.Step{
					{
						Kind: "query",
						Slug: "fetch-users",
						Query: &pipelinespec.QueryRef{
							DataSourceSlug: "acme-postgres",
							OperationType:  "sql",
							OperationRef:   "list-users",
						},
					},
					{
						Kind: "sink",
						Slug: "load-users",
						Sink: &pipelinespec.SinkSpec{
							IdempotencyClass: pipelinespec.SinkIdempotencyClassIdempotentWithKey,
							Input:            &pipelinespec.StepInput{FromStep: "fetch-users"},
							DataSourceSlug:   "acme-sink",
							OperationType:    "sql",
							OperationRef:     "upsert-users",
						},
					},
					{
						Kind: "query",
						Slug: "fetch-audit",
						Query: &pipelinespec.QueryRef{
							DataSourceSlug: "acme-audit-api",
							OperationType:  "http",
							OperationInline: map[string]any{
								"method": "GET",
								"path":   "/audit",
							},
							Parameters: map[string]any{
								"loaded_rows": "$steps.load-users.rows",
							},
						},
					},
				},
			},
			wantErrContains: "must be a query or transform step",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := Validate(tt.pipeline)
			if tt.wantErrContains == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantErrContains)
		})
	}
}

func TestValidatePipeline_KindCompatibility(t *testing.T) {
	t.Parallel()

	t.Run("KIND-001_QueryRowsToTransformRows", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-active-users",
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "id",
								Type:     model.ContractFieldTypeValueInteger,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "transform",
					Slug: "normalize-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{"input": {FromStep: "fetch-users"}}, SQL: "SELECT id AS id FROM input",
						OutputKind: model.TransformOutputKindRows,
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "id",
								Type:     model.ContractFieldTypeValueInteger,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "sink",
					Slug: "acme-users-sync",
					Sink: testSinkSpec("normalize-users", nil),
				},
			},
		}

		err := Validate(pipeline)
		require.NoError(t, err)
	})

	t.Run("QueryRowsToJSONSink", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-active-users",
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "id",
								Type:     model.ContractFieldTypeValueInteger,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "sink",
					Slug: "acme-users-sync",
					Sink: testSinkSpec(
						"fetch-users",
						&payload.Contract{
							Representation: model.ContractRepresentationJSON,
							RootShape:      model.ContractRootShapeObject,
							Fields: []payload.ContractField{
								{Name: "id", Type: model.ContractFieldTypeValueString},
							},
						},
					),
				},
			},
		}

		err := validateStepInputCompatibility(pipeline)
		require.ErrorIs(t, err, ErrContractMismatch)
		require.ErrorContains(t, err, ContractReasonRepresentationMismatch.String())
	})

	t.Run("KIND-003_TransformJSONArrayToJSONSink", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-active-users",
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "id",
								Type:     model.ContractFieldTypeValueInteger,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "transform",
					Slug: "normalize-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{"input": {FromStep: "fetch-users"}}, SQL: "SELECT id AS id FROM input",
						OutputKind: model.TransformOutputKindJSON,
					},
				},
				{
					Kind: "sink",
					Slug: "acme-users-sync",
					Sink: testSinkSpec(
						"normalize-users",
						&payload.Contract{
							Representation: model.ContractRepresentationJSON,
							RootShape:      model.ContractRootShapeArray,
							Fields: []payload.ContractField{
								{Name: "id", Type: model.ContractFieldTypeValueString},
							},
						},
					),
				},
			},
		}

		err := Validate(pipeline)
		require.NoError(t, err)
	})

	t.Run("KIND-003B_TransformJSONArrayRejectsJSONObjectSink", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-active-users",
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "id",
								Type:     model.ContractFieldTypeValueInteger,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "transform",
					Slug: "normalize-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs:     map[string]pipelinespec.StepInput{"input": {FromStep: "fetch-users"}},
						SQL:        "SELECT id AS id FROM input",
						OutputKind: model.TransformOutputKindJSON,
					},
				},
				{
					Kind: "sink",
					Slug: "acme-users-sync",
					Sink: testSinkSpec(
						"normalize-users",
						&payload.Contract{
							Representation: model.ContractRepresentationJSON,
							RootShape:      model.ContractRootShapeObject,
							Fields: []payload.ContractField{
								{Name: "id", Type: model.ContractFieldTypeValueString},
							},
						},
					),
				},
			},
		}

		err := validateStepInputCompatibility(pipeline)
		require.ErrorIs(t, err, ErrContractMismatch)
		require.ErrorContains(t, err, ContractReasonRootShapeMismatch.String())
	})

	t.Run("KIND-004_TransformRowsToJSONSink", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-active-users",
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "id",
								Type:     model.ContractFieldTypeValueInteger,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "transform",
					Slug: "normalize-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{"input": {FromStep: "fetch-users"}}, SQL: "SELECT id AS id FROM input",
						OutputKind: model.TransformOutputKindRows,
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "id",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "sink",
					Slug: "acme-users-sync",
					Sink: testSinkSpec(
						"normalize-users",
						&payload.Contract{
							Representation: model.ContractRepresentationJSON,
							RootShape:      model.ContractRootShapeObject,
							Fields: []payload.ContractField{
								{Name: "id", Type: model.ContractFieldTypeValueString},
							},
						},
					),
				},
			},
		}

		err := validateStepInputCompatibility(pipeline)
		require.ErrorIs(t, err, ErrContractMismatch)
		require.ErrorContains(t, err, ContractReasonRepresentationMismatch.String())
	})

	t.Run("KIND-005_TransformCSVToCSVSink", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-active-users",
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "id",
								Type:     model.ContractFieldTypeValueInteger,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "transform",
					Slug: "normalize-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{"input": {FromStep: "fetch-users"}}, SQL: "SELECT id AS id FROM input",
						OutputKind: model.TransformOutputKindCSV,
					},
				},
				{
					Kind: "sink",
					Slug: "acme-users-sync",
					Sink: testSinkSpec(
						"normalize-users",
						&payload.Contract{
							Representation: model.ContractRepresentationCSV,
							Fields: []payload.ContractField{
								{Name: "id", Type: model.ContractFieldTypeValueString},
							},
						},
					),
				},
			},
		}

		err := Validate(pipeline)
		require.NoError(t, err)
	})

	t.Run("KIND-006_TransformCSVToTransformRows", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-active-users",
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "id",
								Type:     model.ContractFieldTypeValueInteger,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "transform",
					Slug: "normalize-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{"input": {FromStep: "fetch-users"}}, SQL: "SELECT id AS id FROM input",
						OutputKind: model.TransformOutputKindCSV,
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationCSV,
							Fields: []payload.ContractField{{
								Name:     "id",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "transform",
					Slug: "filter-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{"input": {FromStep: "normalize-users"}}, SQL: "SELECT id AS id FROM input",
						OutputKind: model.TransformOutputKindRows,
						InputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "id",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "sink",
					Slug: "acme-users-sync",
					Sink: testSinkSpec("filter-users", nil),
				},
			},
		}

		err := validateStepInputCompatibility(pipeline)
		require.ErrorIs(t, err, ErrContractMismatch)
		require.ErrorContains(t, err, ContractReasonRepresentationMismatch.String())
	})

	t.Run("KIND-007_TransformCSVToJSONSink", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-active-users",
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "id",
								Type:     model.ContractFieldTypeValueInteger,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "transform",
					Slug: "normalize-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{"input": {FromStep: "fetch-users"}}, SQL: "SELECT id AS id FROM input",
						OutputKind: model.TransformOutputKindCSV,
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationCSV,
							Fields: []payload.ContractField{{
								Name:     "id",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "sink",
					Slug: "acme-users-sync",
					Sink: testSinkSpec(
						"normalize-users",
						&payload.Contract{
							Representation: model.ContractRepresentationJSON,
							RootShape:      model.ContractRootShapeObject,
							Fields: []payload.ContractField{
								{Name: "id", Type: model.ContractFieldTypeValueString},
							},
						},
					),
				},
			},
		}

		err := validateStepInputCompatibility(pipeline)
		require.ErrorIs(t, err, ErrContractMismatch)
		require.ErrorContains(t, err, ContractReasonRepresentationMismatch.String())
	})

	t.Run("KIND-008_TransformRowsToCSVSink", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-active-users",
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "id",
								Type:     model.ContractFieldTypeValueInteger,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "transform",
					Slug: "normalize-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{"input": {FromStep: "fetch-users"}}, SQL: "SELECT id AS id FROM input",
						OutputKind: model.TransformOutputKindRows,
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "id",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "sink",
					Slug: "acme-users-sync",
					Sink: testSinkSpec(
						"normalize-users",
						&payload.Contract{
							Representation: model.ContractRepresentationCSV,
							Fields: []payload.ContractField{
								{Name: "id", Type: model.ContractFieldTypeValueString},
							},
						},
					),
				},
			},
		}

		err := validateStepInputCompatibility(pipeline)
		require.ErrorIs(t, err, ErrContractMismatch)
		require.ErrorContains(t, err, ContractReasonRepresentationMismatch.String())
	})
}

func TestEvaluateEdgeDiagnostics(t *testing.T) {
	t.Parallel()

	newPipeline := func(
		producerKind model.TransformOutputKind,
		producerType model.ContractFieldType,
		consumerRep model.ContractRepresentation,
		consumerRoot model.ContractRootShape,
		consumerType model.ContractFieldType,
	) *pipelinespec.Pipeline {
		return &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "transform",
					Slug: "normalize-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{"input": {FromStep: "fetch-users"}}, OutputKind: producerKind,
						OutputContract: &payload.Contract{
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     producerType,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "sink",
					Slug: "acme-users-sync",
					Sink: &pipelinespec.SinkSpec{
						IdempotencyClass: pipelinespec.SinkIdempotencyClassIdempotentWithKey,
						Input:            &pipelinespec.StepInput{FromStep: "normalize-users"},
						InputContract: &payload.Contract{
							Name:           "sink-input",
							Representation: consumerRep,
							RootShape:      consumerRoot,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     consumerType,
								Required: true,
							}},
						},
					},
				},
			},
		}
	}

	t.Run("EDGE-DIAG-001_CompatibleTransformToSinkContracts", func(t *testing.T) {
		t.Parallel()

		pipeline := newPipeline(
			model.TransformOutputKindRows,
			model.ContractFieldTypeValueString,
			model.ContractRepresentationRows,
			model.ContractRootShapeUnknown,
			model.ContractFieldTypeValueString,
		)

		diagnostic := evaluateEdgeDiagnosticsForSteps(pipeline.Steps[0], pipeline.Steps[1])
		require.NotNil(t, diagnostic)
		require.True(t, diagnostic.Compatible, "%+v", diagnostic)
		require.Empty(t, diagnostic.ReasonCode)
	})

	t.Run("EDGE-DIAG-002_ReportsRepresentationMismatch", func(t *testing.T) {
		t.Parallel()

		pipeline := newPipeline(
			model.TransformOutputKindRows,
			model.ContractFieldTypeValueString,
			model.ContractRepresentationJSON,
			model.ContractRootShapeObject,
			model.ContractFieldTypeValueString,
		)

		diagnostic := evaluateEdgeDiagnosticsForSteps(pipeline.Steps[0], pipeline.Steps[1])
		require.NotNil(t, diagnostic)
		require.False(t, diagnostic.Compatible)
		require.Equal(t, ContractReasonRepresentationMismatch, diagnostic.ReasonCode)
	})

	t.Run("EDGE-DIAG-003_AllowsProducerOnlyContractWhenSinkContractMissing", func(t *testing.T) {
		t.Parallel()

		pipeline := newPipeline(
			model.TransformOutputKindRows,
			model.ContractFieldTypeValueString,
			model.ContractRepresentationRows,
			model.ContractRootShapeUnknown,
			model.ContractFieldTypeValueString,
		)
		pipeline.Steps[1].Sink.InputContract = nil

		diagnostic := evaluateEdgeDiagnosticsForSteps(pipeline.Steps[0], pipeline.Steps[1])
		require.NotNil(t, diagnostic)
		require.True(t, diagnostic.Compatible)
	})

	t.Run("EDGE-DIAG-003B_ReportsMissingExplicitEdgeContract", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-users",
					},
				},
				{
					Kind: "sink",
					Slug: "load-users",
					Sink: testSinkSpec("fetch-users", nil),
				},
			},
		}

		diagnostic := evaluateEdgeDiagnosticsForSteps(pipeline.Steps[0], pipeline.Steps[1])

		require.NotNil(t, diagnostic)
		require.False(t, diagnostic.Compatible)
		require.Equal(t, "fetch-users->load-users", diagnostic.EdgeSlug)
		require.Equal(t, pipeline.Steps[0].Slug, diagnostic.FromStepSlug)
		require.Equal(t, pipeline.Steps[1].Slug, diagnostic.ToStepSlug)
		require.Equal(t, ContractReasonMissingContract, diagnostic.ReasonCode)
	})

	t.Run("EDGE-DIAG-004_ReportsTransformToTransformTypeMismatch", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "transform",
					Slug: "normalize-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{"input": {FromStep: "fetch-users"}}, OutputKind: model.TransformOutputKindRows,
						OutputContract: &payload.Contract{
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "transform",
					Slug: "filter-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{"input": {FromStep: "fetch-users"}}, InputContract: &payload.Contract{
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueInteger,
								Required: true,
							}},
						},
					},
				},
			},
		}

		diagnostic := evaluateEdgeDiagnosticsForSteps(pipeline.Steps[0], pipeline.Steps[1])

		require.NotNil(t, diagnostic)
		require.False(t, diagnostic.Compatible)
		require.Equal(t, ContractReasonTypeMismatch, diagnostic.ReasonCode)
	})

	t.Run("EDGE-DIAG-005_ReportsQueryToTransformTypeMismatch", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-active-users",
						OutputContract: &payload.Contract{
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "transform",
					Slug: "normalize-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{"input": {FromStep: "fetch-users"}}, InputContract: &payload.Contract{
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueInteger,
								Required: true,
							}},
						},
					},
				},
			},
		}

		diagnostic := evaluateEdgeDiagnosticsForSteps(pipeline.Steps[0], pipeline.Steps[1])

		require.NotNil(t, diagnostic)
		require.False(t, diagnostic.Compatible)
		require.Equal(t, ContractReasonTypeMismatch, diagnostic.ReasonCode)
	})

	t.Run("EDGE-DIAG-006_ConstrainsExplicitTransformOutputFacets", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "transform",
					Slug: "normalize-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{"input": {FromStep: "fetch-users"}}, OutputKind: model.TransformOutputKindRows,
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationJSON,
							RootShape:      model.ContractRootShapeObject,
							Fields: []payload.ContractField{{
								Name:     "payload",
								Type:     model.ContractFieldTypeValueObject,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "sink",
					Slug: "acme-users-sync",
					Sink: &pipelinespec.SinkSpec{
						IdempotencyClass: pipelinespec.SinkIdempotencyClassIdempotentWithKey,
						InputContract: &payload.Contract{
							Name:           "sink-input",
							Representation: model.ContractRepresentationJSON,
							RootShape:      model.ContractRootShapeObject,
							Fields: []payload.ContractField{{
								Name:     "payload",
								Type:     model.ContractFieldTypeValueObject,
								Required: true,
							}},
						},
					},
				},
			},
		}

		diagnostic := evaluateEdgeDiagnosticsForSteps(pipeline.Steps[0], pipeline.Steps[1])

		require.NotNil(t, diagnostic)
		require.False(t, diagnostic.Compatible)
		require.Equal(t, ContractReasonRepresentationMismatch, diagnostic.ReasonCode)
	})

	t.Run("EDGE-DIAG-007_AllowsJSONSinkWithoutRootShapeToAcceptObjectProducer", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-http",
						OperationType:  "http",
						OperationRef:   "get-user",
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationJSON,
							RootShape:      model.ContractRootShapeObject,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "sink",
					Slug: "load-users",
					Sink: &pipelinespec.SinkSpec{
						IdempotencyClass: pipelinespec.SinkIdempotencyClassIdempotentWithKey,
						Input:            &pipelinespec.StepInput{FromStep: "fetch-users"},
						InputContract: &payload.Contract{
							Representation: model.ContractRepresentationJSON,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					},
				},
			},
		}

		diagnostic := evaluateEdgeDiagnosticsForSteps(pipeline.Steps[0], pipeline.Steps[1])

		require.NotNil(t, diagnostic)
		require.True(t, diagnostic.Compatible, "%+v", diagnostic)
		require.Empty(t, diagnostic.ReasonCode)
	})

	t.Run("EDGE-DIAG-008_ConstrainsExplicitMongoDBOutputFacets", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-mongodb",
						OperationType:  "mongodb",
						OperationRef:   "list-users",
						OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					},
				},
				{
					Kind: "sink",
					Slug: "load-users",
					Sink: &pipelinespec.SinkSpec{
						IdempotencyClass: pipelinespec.SinkIdempotencyClassIdempotentWithKey,
						Input:            &pipelinespec.StepInput{FromStep: "fetch-users"},
						InputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					},
				},
			},
		}

		diagnostic := evaluateEdgeDiagnosticsForSteps(pipeline.Steps[0], pipeline.Steps[1])

		require.NotNil(t, diagnostic)
		require.False(t, diagnostic.Compatible)
		require.Equal(t, ContractReasonRepresentationMismatch, diagnostic.ReasonCode)
	})

	t.Run("EDGE-DIAG-009_PropagatesNullableFieldInTransformContracts", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "transform",
					Slug: "normalize-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs: map[string]pipelinespec.StepInput{"input": {FromStep: "fetch-users"}}, OutputKind: model.TransformOutputKindRows,
						OutputContract: &payload.Contract{Fields: []payload.ContractField{{
							Name:     "email",
							Type:     model.ContractFieldTypeValueString,
							Required: true,
							Nullable: true,
						}}},
					},
				},
				{
					Kind: "sink",
					Slug: "acme-users-sync",
					Sink: &pipelinespec.SinkSpec{
						IdempotencyClass: pipelinespec.SinkIdempotencyClassIdempotentWithKey,
						InputContract: &payload.Contract{
							Name:           "sink-input",
							Representation: model.ContractRepresentationRows,
							RootShape:      model.ContractRootShapeUnknown,
							Fields: []payload.ContractField{{
								Name:     "email",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
								Nullable: false,
							}},
						},
					},
				},
			},
		}

		diagnostic := evaluateEdgeDiagnosticsForSteps(pipeline.Steps[0], pipeline.Steps[1])

		require.NotNil(t, diagnostic)
		require.False(t, diagnostic.Compatible)
		require.Equal(t, ContractReasonNullabilityMismatch, diagnostic.ReasonCode)
	})

	t.Run("EDGE-DIAG-010_ReportsJSONTransformRowsAsArrayWhenSinkExpectsObject", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "transform",
					Slug: "serialize-users",
					Transform: &pipelinespec.TransformSpec{
						Inputs:     map[string]pipelinespec.StepInput{"input": {FromStep: "fetch-users"}},
						SQL:        "SELECT id, email FROM input",
						OutputKind: model.TransformOutputKindJSON,
						OutputContract: &payload.Contract{Fields: []payload.ContractField{
							{Name: "id", Type: model.ContractFieldTypeValueInteger, Required: true},
							{Name: "email", Type: model.ContractFieldTypeValueString, Required: true},
						}},
					},
				},
				{
					Kind: "sink",
					Slug: "load-users",
					Sink: &pipelinespec.SinkSpec{
						IdempotencyClass: pipelinespec.SinkIdempotencyClassIdempotentWithKey,
						Input:            &pipelinespec.StepInput{FromStep: "serialize-users"},
						InputContract: &payload.Contract{
							Representation: model.ContractRepresentationJSON,
							RootShape:      model.ContractRootShapeObject,
							Fields: []payload.ContractField{
								{Name: "id", Type: model.ContractFieldTypeValueInteger, Required: true},
								{Name: "email", Type: model.ContractFieldTypeValueString, Required: true},
							},
						},
					},
				},
			},
		}

		diagnostic := evaluateEdgeDiagnosticsForSteps(pipeline.Steps[0], pipeline.Steps[1])

		require.NotNil(t, diagnostic)
		require.False(t, diagnostic.Compatible)
		require.Equal(t, ContractReasonRootShapeMismatch, diagnostic.ReasonCode)
		require.Equal(t, "object", diagnostic.Expected["root_shape"])
		require.Equal(t, "array", diagnostic.Actual["root_shape"])
	})
}
