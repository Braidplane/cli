package validate

import (
	"testing"

	"github.com/Braidplane/cli/pkg/model"
	"github.com/Braidplane/cli/pkg/pipeline/payload"
	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"
	"github.com/stretchr/testify/require"
)

func compositionContract(names ...string) *payload.Contract {
	c := &payload.Contract{Representation: model.ContractRepresentationRows}
	for _, name := range names {
		c.Fields = append(c.Fields, payload.ContractField{Name: name, Type: model.ContractFieldTypeValueInteger, Required: true})
	}
	return c
}

func compositionPipeline() *pipelinespec.Pipeline {
	return &pipelinespec.Pipeline{Metadata: pipelinespec.Metadata{Slug: "settlement", Name: "Settlement"}, Steps: []pipelinespec.Step{
		{Kind: "query", Slug: "orders", Query: &pipelinespec.QueryRef{DataSourceSlug: "db", OperationType: "sql", OperationRef: "orders", OutputContract: compositionContract("id", "amount")}},
		{Kind: "query", Slug: "accounts", Query: &pipelinespec.QueryRef{DataSourceSlug: "db", OperationType: "sql", OperationRef: "accounts", OutputContract: compositionContract("id")}},
		{Kind: "transform", Slug: "settle", Transform: &pipelinespec.TransformSpec{SQL: "SELECT orders.id, orders.amount FROM orders JOIN accounts ON orders.id = accounts.id", OutputKind: model.TransformOutputKindRows, Inputs: map[string]pipelinespec.StepInput{"orders": {FromStep: "orders"}, "accounts": {FromStep: "accounts"}}, InputContracts: map[string]*pipelinespec.InputContractAttachment{"orders": {Contract: compositionContract("id", "amount")}, "accounts": {Contract: compositionContract("id")}}}},
		{Kind: "transform", Slug: "audit", Transform: &pipelinespec.TransformSpec{SQL: "SELECT id FROM orders", OutputKind: model.TransformOutputKindRows, Inputs: map[string]pipelinespec.StepInput{"orders": {FromStep: "orders"}}, InputContracts: map[string]*pipelinespec.InputContractAttachment{"orders": {Contract: compositionContract("id")}}}},
	}}
}

func TestNamedInputContractRequirements(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		change                 func(*pipelinespec.Pipeline)
		structural, compatible bool
	}{
		{name: "Orders reused for Settlement and Audit subsets", structural: true, compatible: true},
		{name: "missing amount", change: func(p *pipelinespec.Pipeline) { p.Steps[0].Query.OutputContract = compositionContract("id") }, structural: true},
		{name: "renamed ID", change: func(p *pipelinespec.Pipeline) { p.Steps[1].Query.OutputContract = compositionContract("account_id") }, structural: true},
		{name: "unknown input", change: func(p *pipelinespec.Pipeline) {
			p.Steps[2].Transform.InputContracts["missing"] = &pipelinespec.InputContractAttachment{Contract: compositionContract("id")}
		}},
		{name: "inline reference conflict", change: func(p *pipelinespec.Pipeline) {
			p.Steps[2].Transform.InputContracts["orders"].ContractRef = &pipelinespec.ODCSContractRef{ContractSlug: "orders", RevisionSlug: "v1", SchemaObjectID: "row"}
		}},
		{name: "combined inline conflict", change: func(p *pipelinespec.Pipeline) { p.Steps[2].Transform.InputContract = compositionContract("orders.id") }},
		{name: "combined reference conflict", change: func(p *pipelinespec.Pipeline) {
			p.Steps[2].Transform.InputContractRef = &pipelinespec.ODCSContractRef{ContractSlug: "orders", RevisionSlug: "v1", SchemaObjectID: "row"}
		}},
		{name: "single input combined conflict", change: func(p *pipelinespec.Pipeline) { p.Steps[3].Transform.InputContract = compositionContract("id") }},
		{name: "empty attachment", change: func(p *pipelinespec.Pipeline) {
			p.Steps[2].Transform.InputContracts["orders"] = &pipelinespec.InputContractAttachment{}
		}},
		{name: "null attachment", change: func(p *pipelinespec.Pipeline) { p.Steps[2].Transform.InputContracts["orders"] = nil }},
		{name: "file reference", change: func(p *pipelinespec.Pipeline) {
			p.Steps[2].Transform.InputContracts["orders"] = &pipelinespec.InputContractAttachment{ContractRef: &pipelinespec.ODCSContractRef{ContractSlug: "orders", RevisionSlug: "v1", SchemaObjectID: "row", Representation: model.ContractRepresentationFile}}
		}},
		{name: "multipart reference", change: func(p *pipelinespec.Pipeline) {
			p.Steps[2].Transform.InputContracts["orders"] = &pipelinespec.InputContractAttachment{ContractRef: &pipelinespec.ODCSContractRef{ContractSlug: "orders", RevisionSlug: "v1", SchemaObjectID: "row", Representation: model.ContractRepresentationMultipartForm}}
		}},
		{name: "empty map", change: func(p *pipelinespec.Pipeline) {
			p.Steps[2].Transform.InputContracts = map[string]*pipelinespec.InputContractAttachment{}
		}},
		{name: "producer only", change: func(p *pipelinespec.Pipeline) { delete(p.Steps[2].Transform.InputContracts, "accounts") }, structural: true, compatible: true},
		{name: "consumer only unknown provider shape", change: func(p *pipelinespec.Pipeline) { p.Steps[0].Query.OutputContract = nil }, structural: true, compatible: true},
		{name: "neither side", change: func(p *pipelinespec.Pipeline) {
			p.Steps[1].Query.OutputContract = nil
			delete(p.Steps[2].Transform.InputContracts, "accounts")
		}, structural: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := compositionPipeline()
			if tc.change != nil {
				tc.change(p)
			}
			if !tc.structural {
				require.Error(t, Validate(p))
				return
			}
			require.NoError(t, Validate(p))
			err := ValidateStepInputCompatibility(p)
			if tc.compatible {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	p := compositionPipeline()
	contexts, err := StepEdgeContexts(p)
	require.NoError(t, err)
	require.Len(t, contexts, 3)
	for _, ctx := range contexts {
		d := EvaluateEdgeDiagnosticsForSteps(ctx.FromStep, ctx.ToStep)
		require.True(t, d.Compatible)
		require.Contains(t, d.EdgeSlug, ":")
	}
	require.Nil(t, p.Steps[2].Transform.InputContract, "edge adaptation must not mutate authored contracts")
}

func TestEventInputOutputReference(t *testing.T) {
	ref := &pipelinespec.ODCSContractRef{ContractSlug: "orders", RevisionSlug: "v1", SchemaObjectID: "row", Representation: model.ContractRepresentationJSON, RootShape: model.ContractRootShapeObject}
	p := &pipelinespec.Pipeline{Metadata: pipelinespec.Metadata{Slug: "events", Name: "Events"}, Steps: []pipelinespec.Step{{Kind: "input", Slug: "event", Input: &pipelinespec.InputSpec{OutputContractRef: ref}}}}
	p.Steps = append(p.Steps, pipelinespec.Step{Kind: "transform", Slug: "audit", Transform: &pipelinespec.TransformSpec{SQL: "SELECT id FROM orders", OutputKind: model.TransformOutputKindRows, Inputs: map[string]pipelinespec.StepInput{"orders": {FromStep: "event"}}, InputContracts: map[string]*pipelinespec.InputContractAttachment{"orders": {ContractRef: ref}}}})
	require.NoError(t, Validate(p))
	require.NoError(t, ValidateStepInputCompatibility(p))
	ref.RootShape = model.ContractRootShapeUnknown
	require.Error(t, Validate(p))
	ref.RootShape = model.ContractRootShapeObject
	ref.Representation = model.ContractRepresentationRows
	require.Error(t, Validate(p))
	ref.Representation = model.ContractRepresentationJSON
	p.Steps[0].Input.OutputContract = compositionContract("id")
	require.Error(t, Validate(p))
	p.Steps[0].Input.OutputContract = nil
	p.Steps = append(p.Steps, p.Steps[0])
	p.Steps[len(p.Steps)-1].Slug = "second"
	require.Error(t, Validate(p))
}
