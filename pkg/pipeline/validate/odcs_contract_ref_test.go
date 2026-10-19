package validate

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/model"
	"github.com/Braidplane/cli/pkg/pipeline/payload"
	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"
	"github.com/Braidplane/cli/pkg/slug"
)

func TestODCSContractRefsLoadAndDeferCompatibility(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		path string
		data string
	}{
		{
			name: "YAML",
			path: "contracts.pipeline.yaml",
			data: `metadata:
  slug: contract-refs
  name: Contract refs
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: warehouse
      operation_type: sql
      operation_ref: list-users
      output_contract_ref: {contract_slug: users, revision_slug: v1, schema_object_id: users-row, representation: rows}
  - kind: transform
    slug: shape-users
    transform:
      inputs: {input: fetch-users}
      sql: SELECT id FROM input
      output_kind: rows
      input_contract_ref: {contract_slug: users, revision_slug: v1, schema_object_id: users-row}
      output_contract_ref: {contract_slug: users, revision_slug: v1, schema_object_id: shaped-users, representation: rows}
  - kind: sink
    slug: write-users
    sink:
      input: shape-users
      data_source_ref: warehouse
      operation_type: sql
      operation_ref: upsert-users
      idempotency_class: idempotent_with_key
      input_contract_ref: {contract_slug: users, revision_slug: v1, schema_object_id: shaped-users}
`,
		},
		{
			name: "JSON",
			path: "contracts.pipeline.json",
			data: `{"metadata":{"slug":"contract-refs","name":"Contract refs"},"steps":[{"kind":"query","slug":"fetch-users","query":{"data_source_ref":"warehouse","operation_type":"sql","operation_ref":"list-users","output_contract_ref":{"contract_slug":"users","revision_slug":"v1","schema_object_id":"users-row","representation":"rows"}}},{"kind":"transform","slug":"shape-users","transform":{"inputs":{"input":"fetch-users"},"sql":"SELECT id FROM input","output_kind":"rows","input_contract_ref":{"contract_slug":"users","revision_slug":"v1","schema_object_id":"users-row"},"output_contract_ref":{"contract_slug":"users","revision_slug":"v1","schema_object_id":"shaped-users"}}},{"kind":"sink","slug":"write-users","sink":{"input":"shape-users","data_source_ref":"warehouse","operation_type":"sql","operation_ref":"upsert-users","idempotency_class":"idempotent_with_key","input_contract_ref":{"contract_slug":"users","revision_slug":"v1","schema_object_id":"shaped-users"}}}]}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pipeline, err := pipelinespec.Load(tc.path, []byte(tc.data))
			require.NoError(t, err)
			require.NoError(t, Validate(pipeline))
			require.NoError(t, ValidateStepInputCompatibility(pipeline))
		})
	}
}

func TestODCSContractRefDeclarationValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ref  *pipelinespec.ODCSContractRef
		want string
	}{
		{name: "MissingContractSlug", ref: &pipelinespec.ODCSContractRef{RevisionSlug: "v1", SchemaObjectID: "users"}, want: "contract_slug"},
		{name: "LatestRevision", ref: &pipelinespec.ODCSContractRef{ContractSlug: "users", RevisionSlug: "latest", SchemaObjectID: "users"}, want: "exact revision_slug"},
		{name: "MissingSchemaObject", ref: &pipelinespec.ODCSContractRef{ContractSlug: "users", RevisionSlug: "v1"}, want: "schema_object_id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.ErrorContains(t, validateContractDeclaration(nil, tc.ref), tc.want)
		})
	}

	inline := &pipelinespec.Pipeline{Metadata: pipelinespec.Metadata{Slug: "contract-refs", Name: "Contract refs"}, Steps: []pipelinespec.Step{{
		Kind: "query", Slug: "fetch-users", Query: &pipelinespec.QueryRef{
			DataSourceSlug: "warehouse", OperationType: "sql", OperationRef: "list-users",
			OutputContract: &payload.Contract{}, OutputContractRef: &pipelinespec.ODCSContractRef{ContractSlug: "users", RevisionSlug: "v1", SchemaObjectID: "users"},
		},
	}}}
	require.ErrorContains(t, Validate(inline), "mutually exclusive")
}

func TestODCSContractRefsPreserveInlineCompatibilityChecks(t *testing.T) {
	t.Parallel()

	pipeline := &pipelinespec.Pipeline{Metadata: pipelinespec.Metadata{Slug: "contract-refs", Name: "Contract refs"}, Steps: []pipelinespec.Step{
		{
			Kind: "query", Slug: "fetch-users", Query: &pipelinespec.QueryRef{
				DataSourceSlug: "warehouse", OperationType: "sql", OperationRef: "list-users",
				OutputContract: &payload.Contract{Representation: model.ContractRepresentationRows, Fields: []payload.ContractField{{Name: "id", Type: model.ContractFieldTypeValueString}}},
			},
		},
		{
			Kind: "sink", Slug: "write-users", Sink: &pipelinespec.SinkSpec{
				Input: &pipelinespec.StepInput{FromStep: slug.Slug("fetch-users")}, DataSourceSlug: "warehouse", OperationType: "sql", OperationRef: "upsert-users", IdempotencyClass: pipelinespec.SinkIdempotencyClassIdempotentWithKey,
				InputContract: &payload.Contract{Representation: model.ContractRepresentationRows, Fields: []payload.ContractField{{Name: "id", Type: model.ContractFieldTypeValueInteger}}},
			},
		},
	}}

	require.NoError(t, Validate(pipeline))
	require.ErrorIs(t, ValidateStepInputCompatibility(pipeline), ErrContractMismatch)
}
