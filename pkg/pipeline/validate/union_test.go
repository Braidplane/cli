package validate

import (
	"testing"

	"github.com/Braidplane/cli/pkg/model"
	"github.com/Braidplane/cli/pkg/pipeline/payload"
	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"
	"github.com/stretchr/testify/require"
)

func TestValidateUnionAllRejectsKnownNullabilityBeforeExecution(t *testing.T) {
	contract := &payload.Contract{Name: "offers", Representation: model.ContractRepresentationRows, Fields: []payload.ContractField{{Name: "product", Type: model.ContractFieldTypeValueString, Required: true}}}
	other := payload.CloneContract(contract)
	pipeline := &pipelinespec.Pipeline{Metadata: pipelinespec.Metadata{Slug: "combined-offers", Name: "Combined offers"}, Steps: []pipelinespec.Step{
		{Kind: "query", Slug: "read-a", Query: &pipelinespec.QueryRef{DataSourceSlug: "files", OperationType: "sql", OperationRef: "read-a", OutputContract: contract}},
		{Kind: "query", Slug: "read-b", Query: &pipelinespec.QueryRef{DataSourceSlug: "files", OperationType: "sql", OperationRef: "read-b", OutputContract: other}},
		{Kind: "transform", Slug: "combine", Transform: &pipelinespec.TransformSpec{SQL: "SELECT product FROM a UNION ALL SELECT product FROM b", OutputKind: model.TransformOutputKindRows, Inputs: map[string]pipelinespec.StepInput{"a": {FromStep: "read-a"}, "b": {FromStep: "read-b"}}, OutputContract: contract}},
	}}
	require.NoError(t, Validate(pipeline))
	require.NoError(t, ValidateStepInputCompatibility(pipeline))
	other.Fields[0].Nullable = true
	require.ErrorContains(t, Validate(pipeline), `input "b" field "product"`)
}
