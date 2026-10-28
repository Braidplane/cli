package graph

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/model"
	pipelineruntime "github.com/Braidplane/cli/pkg/pipeline/runtime"
	"github.com/Braidplane/cli/pkg/pipeline/spec"
	"github.com/Braidplane/cli/pkg/slug"
	"github.com/Braidplane/cli/pkg/xerror"
)

func TestNormalizeStepInputEdges(t *testing.T) {
	t.Parallel()

	pipeline := &spec.Pipeline{
		Metadata: spec.Metadata{Slug: "acme-users-join", Name: "ACME Users Join"},
		Steps: []spec.Step{
			{
				Kind: "query",
				Slug: "fetch-left-users",
				Query: &spec.QueryRef{
					DataSourceSlug: "acme-postgres",
					OperationType:  "sql",
					OperationRef:   "list-left-users",
				},
			},
			{
				Kind: "query",
				Slug: "fetch-right-users",
				Query: &spec.QueryRef{
					DataSourceSlug: "acme-postgres",
					OperationType:  "sql",
					OperationRef:   "list-right-users",
				},
			},
			{
				Kind: "transform",
				Slug: "join-users",
				Transform: &spec.TransformSpec{
					SQL:        "SELECT left.id FROM left JOIN right ON left.id = right.id",
					OutputKind: model.TransformOutputKindRows,
					Inputs: map[string]spec.StepInput{
						"right": {FromStep: "fetch-right-users"},
						"left":  {FromStep: "fetch-left-users"},
					},
				},
			},
			{
				Kind: "sink",
				Slug: "load-users",
				Sink: &spec.SinkSpec{Input: &spec.StepInput{FromStep: "join-users"}},
			},
		},
	}

	normalized, err := Normalize(pipeline)
	require.NoError(t, err)

	edges := normalized.StepInputEdges()
	require.Len(t, edges, 3)
	require.Equal(t, EdgeKindStepInput, edges[0].Kind)
	require.Equal(t, "fetch-left-users", edges[0].FromStepSlug.String())
	require.Equal(t, "join-users", edges[0].ToStepSlug.String())
	require.Equal(t, "left", edges[0].InputName)
	require.Equal(t, "fetch-right-users", edges[1].FromStepSlug.String())
	require.Equal(t, "join-users", edges[1].ToStepSlug.String())
	require.Equal(t, "right", edges[1].InputName)
	require.Equal(t, "join-users", edges[2].FromStepSlug.String())
	require.Equal(t, "load-users", edges[2].ToStepSlug.String())

	inputs, ok := normalized.TransformInputStepSlugs("join-users")
	require.NoError(t, ValidateConnectivity(normalized))
	require.True(t, ok)
	require.Equal(t, map[string]string{
		"left":  "fetch-left-users",
		"right": "fetch-right-users",
	}, map[string]string{
		"left":  inputs["left"].String(),
		"right": inputs["right"].String(),
	})

	sinkInput, ok := normalized.SinkInputStepSlug("load-users")
	require.True(t, ok)
	require.Equal(t, "join-users", sinkInput.String())
}

func TestValidateConnectivityRejectsStandaloneInput(t *testing.T) {
	t.Parallel()

	normalized, err := Normalize(&spec.Pipeline{
		Metadata: spec.Metadata{Slug: "event-only", Name: "Event only"},
		Steps: []spec.Step{{
			Kind:  "input",
			Slug:  "event",
			Input: &spec.InputSpec{},
		}},
	})
	require.NoError(t, err)
	require.ErrorContains(t, ValidateConnectivity(normalized), "not connected")
}

func TestNormalizeAllowsOneProducerToMultipleSinks(t *testing.T) {
	t.Parallel()

	pipeline := &spec.Pipeline{
		Metadata: spec.Metadata{Slug: "acme-users-fan-out", Name: "ACME Users Fan Out"},
		Steps: []spec.Step{
			{
				Kind: "query",
				Slug: "fetch-users",
				Query: &spec.QueryRef{
					DataSourceSlug: "acme-postgres",
					OperationType:  "sql",
					OperationRef:   "list-users",
				},
			},
			{
				Kind: "sink",
				Slug: "load-warehouse",
				Sink: &spec.SinkSpec{Input: &spec.StepInput{FromStep: "fetch-users"}},
			},
			{
				Kind: "sink",
				Slug: "call-webhook",
				Sink: &spec.SinkSpec{Input: &spec.StepInput{FromStep: "fetch-users"}},
			},
		},
	}

	normalized, err := Normalize(pipeline)
	require.NoError(t, err)

	edges := normalized.StepInputEdges()
	require.Len(t, edges, 2)
	require.Equal(t, "fetch-users->load-warehouse", edges[0].Slug())
	require.Equal(t, "fetch-users->call-webhook", edges[1].Slug())

	ordered := normalized.TopologicalSteps()
	require.NoError(t, ValidateConnectivity(normalized))
	require.Len(t, ordered, 3)
	require.Equal(t, "fetch-users", ordered[0].Slug.String())
	require.Equal(t, "load-warehouse", ordered[1].Slug.String())
	require.Equal(t, "call-webhook", ordered[2].Slug.String())
}

func TestNormalizeAllowsForwardStepReferences(t *testing.T) {
	t.Parallel()

	pipeline := &spec.Pipeline{
		Metadata: spec.Metadata{Slug: "unordered-pipeline", Name: "Unordered Pipeline"},
		Steps: []spec.Step{
			{
				Kind: "sink",
				Slug: "load-users",
				Sink: &spec.SinkSpec{Input: &spec.StepInput{FromStep: "normalize-users"}},
			},
			{
				Kind: "transform",
				Slug: "normalize-users",
				Transform: &spec.TransformSpec{
					SQL:        "SELECT id FROM input",
					OutputKind: model.TransformOutputKindRows,
					Inputs:     map[string]spec.StepInput{"input": {FromStep: "fetch-users"}},
				},
			},
			{
				Kind: "query",
				Slug: "fetch-users",
				Query: &spec.QueryRef{
					DataSourceSlug: "acme-postgres",
					OperationType:  "sql",
					OperationRef:   "list-users",
				},
			},
		},
	}

	normalized, err := Normalize(pipeline)
	require.NoError(t, err)

	edges := normalized.StepInputEdges()
	require.Len(t, edges, 2)
	require.Equal(t, "fetch-users->normalize-users", edges[0].Slug())
	require.Equal(t, "normalize-users->load-users", edges[1].Slug())

	ordered := normalized.TopologicalSteps()
	require.Len(t, ordered, 3)
	require.Equal(t, "fetch-users", ordered[0].Slug.String())
	require.Equal(t, "normalize-users", ordered[1].Slug.String())
	require.Equal(t, "load-users", ordered[2].Slug.String())
}

func TestNormalizeQueryParameterEdges(t *testing.T) {
	t.Parallel()

	pipeline := &spec.Pipeline{
		Metadata: spec.Metadata{Slug: "acme-source-inspection", Name: "ACME Source Inspection"},
		Steps: []spec.Step{
			{
				Kind: "query",
				Slug: "fetch-users",
				Query: &spec.QueryRef{
					DataSourceSlug: "acme-postgres",
					OperationType:  "sql",
					OperationRef:   "list-users",
				},
			},
			{
				Kind: "query",
				Slug: "fetch-profiles",
				Query: &spec.QueryRef{
					DataSourceSlug:  "acme-crm",
					OperationType:   "http",
					OperationInline: map[string]any{"method": "GET", "path": "/profiles"},
					Parameters: map[string]any{
						"user_rows": "$steps.fetch-users.rows",
						"nested": map[string]any{
							"same_reference": "$steps.fetch-users.count",
						},
					},
				},
			},
		},
	}

	normalized, err := Normalize(pipeline)
	require.NoError(t, err)

	edges := normalized.QueryParameterEdges()
	require.NoError(t, ValidateConnectivity(normalized))
	require.Len(t, edges, 1)
	require.Equal(t, EdgeKindQueryParameter, edges[0].Kind)
	require.Equal(t, "fetch-users", edges[0].FromStepSlug.String())
	require.Equal(t, "fetch-profiles", edges[0].ToStepSlug.String())
}

func TestNormalizeIgnoresLiteralStepLikeQueryParameterValues(t *testing.T) {
	t.Parallel()

	pipeline := &spec.Pipeline{
		Metadata: spec.Metadata{Slug: "runtime-input-paths", Name: "Runtime Input Paths"},
		Steps: []spec.Step{
			{
				Kind: "query",
				Slug: "fetch-users",
				Query: &spec.QueryRef{
					DataSourceSlug: "acme-postgres",
					OperationType:  "sql",
					OperationRef:   "list-users",
					Parameters: map[string]any{
						"archive_path": pipelineruntime.LiteralString("$steps.archive/users"),
					},
				},
			},
		},
	}

	normalized, err := Normalize(pipeline)
	require.NoError(t, err)
	require.Empty(t, normalized.QueryParameterEdges())
}

func TestNormalizeRejectsInvalidGraph(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		pipeline        *spec.Pipeline
		wantErrContains string
	}{
		{
			name: "RejectsSinkAsProducer",
			pipeline: &spec.Pipeline{
				Metadata: spec.Metadata{Slug: "sink-producer", Name: "Sink Producer"},
				Steps: []spec.Step{
					{
						Kind: "query",
						Slug: "fetch-users",
						Query: &spec.QueryRef{
							DataSourceSlug: "acme-postgres",
							OperationType:  "sql",
							OperationRef:   "list-users",
						},
					},
					{
						Kind: "sink",
						Slug: "load-users",
						Sink: &spec.SinkSpec{Input: &spec.StepInput{FromStep: "fetch-users"}},
					},
					{
						Kind: "sink",
						Slug: "load-audit",
						Sink: &spec.SinkSpec{Input: &spec.StepInput{FromStep: "load-users"}},
					},
				},
			},
			wantErrContains: "must be a query or transform step",
		},
		{
			name: "RejectsOrphanQuery",
			pipeline: &spec.Pipeline{
				Metadata: spec.Metadata{Slug: "orphan-query", Name: "Orphan Query"},
				Steps: []spec.Step{
					{
						Kind: "query",
						Slug: "fetch-users",
						Query: &spec.QueryRef{
							DataSourceSlug: "acme-postgres",
							OperationType:  "sql",
							OperationRef:   "list-users",
						},
					},
					{
						Kind: "query",
						Slug: "fetch-audit",
						Query: &spec.QueryRef{
							DataSourceSlug:  "acme-http",
							OperationType:   "http",
							OperationInline: map[string]any{"method": "GET", "path": "/audit"},
						},
					},
				},
			},
			wantErrContains: "disconnected data-flow groups",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			normalized, err := Normalize(tt.pipeline)
			if err == nil && tt.name == "RejectsOrphanQuery" {
				err = ValidateConnectivity(normalized)
			}
			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantErrContains)
		})
	}
}

func TestNormalizeTopologicalSteps(t *testing.T) {
	t.Parallel()

	pipeline := &spec.Pipeline{
		Metadata: spec.Metadata{Slug: "acme-topology", Name: "ACME Topology"},
		Steps: []spec.Step{
			{
				Kind: "query",
				Slug: "fetch-users",
				Query: &spec.QueryRef{
					DataSourceSlug: "acme-postgres",
					OperationType:  "sql",
					OperationRef:   "list-users",
				},
			},
			{
				Kind: "query",
				Slug: "fetch-profiles",
				Query: &spec.QueryRef{
					DataSourceSlug:  "acme-http",
					OperationType:   "http",
					OperationInline: map[string]any{"method": "GET", "path": "/profiles"},
					Parameters:      map[string]any{"users": "$steps.fetch-users.rows"},
				},
			},
			{
				Kind: "sink",
				Slug: "load-profiles",
				Sink: &spec.SinkSpec{Input: &spec.StepInput{FromStep: "fetch-profiles"}},
			},
		},
	}

	normalized, err := Normalize(pipeline)
	require.NoError(t, err)

	ordered := normalized.TopologicalSteps()
	require.Len(t, ordered, 3)
	require.Equal(t, "fetch-users", ordered[0].Slug.String())
	require.Equal(t, "fetch-profiles", ordered[1].Slug.String())
	require.Equal(t, "load-profiles", ordered[2].Slug.String())
}

func TestValidateConnectivityComponents(t *testing.T) {
	t.Parallel()
	query := func(name slug.Slug) spec.Step {
		return spec.Step{Kind: "query", Slug: name, Query: &spec.QueryRef{DataSourceSlug: "shared-source"}}
	}
	sink := func(name, from slug.Slug) spec.Step {
		return spec.Step{Kind: "sink", Slug: name, Sink: &spec.SinkSpec{Input: &spec.StepInput{FromStep: from}}}
	}
	for _, tc := range []struct {
		name   string
		steps  []spec.Step
		groups string
	}{
		{name: "single query", steps: []spec.Step{query("a")}},
		{name: "two chains sharing a connector", steps: []spec.Step{query("a"), sink("x", "a"), query("b"), sink("y", "b")}, groups: `["a", "x"]; ["b", "y"]`},
		{name: "isolated first query", steps: []spec.Step{query("a"), query("b"), sink("y", "b")}, groups: `["a"]; ["b", "y"]`},
		{name: "isolated last query", steps: []spec.Step{query("b"), sink("y", "b"), query("a")}, groups: `["b", "y"]; ["a"]`},
		{name: "three groups", steps: []spec.Step{query("a"), query("b"), query("c")}, groups: `["a"]; ["b"]; ["c"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := Normalize(&spec.Pipeline{Steps: tc.steps})
			require.NoError(t, err)
			err = ValidateConnectivity(g)
			if tc.groups == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrOrphanStep)
			require.ErrorContains(t, err, tc.groups)
			require.Contains(t, xerror.PublicDetail(err), tc.groups)
			require.ErrorContains(t, err, "split independent flows into separate pipelines or connect them with an actual data dependency")
		})
	}
}
