package runtime

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Braidplane/cli/pkg/model"
	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"
	"github.com/stretchr/testify/require"
)

func TestMixedSuppliedTextSurvivesPersistenceAndAuthoredResolution(t *testing.T) {
	for _, supplied := range []string{"{{runtime.run.date | add_days:1}}", "{{runtime.input.unavailable}}", "{{customer}}", "{{runtime .broken", "żółć/{{runtime.run.date}}"} {
		p := &pipelinespec.Pipeline{Steps: []pipelinespec.Step{{Slug: "read", Query: &pipelinespec.QueryRef{Parameters: map[string]any{
			"path": "exports/{{runtime.env.prefix}}/{{runtime.run.date}}/{{runtime.input.suffix}}",
		}}}}}
		require.NoError(t, ResolvePipeline(p, []model.RuntimeVariable{{Key: "prefix", ValueType: model.RuntimeVariableValueTypeText, Value: supplied}}))
		locations := LiteralLocations(p)
		raw, err := json.Marshal(p)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &p))
		require.True(t, RestoreLiteralLocations(p, locations))
		require.NoError(t, ValidatePipelineExecution(p))
		require.NoError(t, ResolvePipelineExecution(p, ExecutionContext{At: time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC)}))
		require.NoError(t, ResolvePipelineInput(p, map[string]any{"suffix": "{{runtime.secrets.hidden}}"}))
		require.NoError(t, ValidatePipelineExecution(p))
		require.Equal(t, "exports/"+supplied+"/2024-03-31/{{runtime.secrets.hidden}}", LiteralValue(p.Steps[0].Query.Parameters["path"]))
	}
}

func TestLiteralLocationsKeepNestedSiblingsAndRejectInvalidRanges(t *testing.T) {
	p := &pipelinespec.Pipeline{Steps: []pipelinespec.Step{{Query: &pipelinespec.QueryRef{Parameters: map[string]any{
		"nested": map[string]any{"a": LiteralString("{{one}}"), "b": []any{LiteralString("{{two}}"), LiteralTemplate{Text: "x{{three}}y", Ranges: [][2]int{{1, 10}}}}},
	}}}}}
	locations := LiteralLocations(p)
	require.Len(t, locations, 3)
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	var restored pipelinespec.Pipeline
	require.NoError(t, json.Unmarshal(raw, &restored))
	require.True(t, RestoreLiteralLocations(&restored, locations))
	require.Equal(t, locations, LiteralLocations(&restored))
	locations[2].Ranges = [][2]int{{1, 10000}}
	require.NoError(t, json.Unmarshal(raw, &restored))
	require.False(t, RestoreLiteralLocations(&restored, locations))
}

func TestMixedSuppliedAndAuthoredSecretReferences(t *testing.T) {
	p := &pipelinespec.Pipeline{Steps: []pipelinespec.Step{{Query: &pipelinespec.QueryRef{Parameters: map[string]any{
		"header": "{{runtime.input.prefix}}/{{runtime.secrets.SIGNATURE}}",
	}}}}}
	require.NoError(t, ResolvePipelineInput(p, map[string]any{"prefix": "{{runtime.secrets.NOT_REQUESTED}}"}))
	require.True(t, HasEnvironmentSecretReferenceInPipeline(p))
	require.NoError(t, ResolvePipelineSecrets(p, []model.RuntimeSecretValue{{Key: "SIGNATURE", Value: "{{customer}}"}}))
	require.False(t, HasEnvironmentSecretReferenceInPipeline(p))
	require.Equal(t, "{{runtime.secrets.NOT_REQUESTED}}/{{customer}}", LiteralValue(p.Steps[0].Query.Parameters["header"]))
}

func TestIncrementalBindingsDeferOnlyAuthoredText(t *testing.T) {
	p := &pipelinespec.Pipeline{Steps: []pipelinespec.Step{{Query: &pipelinespec.QueryRef{Parameters: map[string]any{
		"checkpoint": "{{runtime.incremental.lower_bound.provider_field}}",
		"input":      LiteralString("{{runtime.incremental.lower_bound.provider_field}}"),
	}}}}}
	FreezeLiteralOperationFields(p)
	parameters := map[string]any{"runtime.incremental.lower_bound.provider_field": nil}
	value, err := ResolveIncrementalValue(p.Steps[0].Query.Parameters["checkpoint"], parameters)
	require.NoError(t, err)
	require.Nil(t, value)
	parameters["runtime.incremental.lower_bound.provider_field"] = int64(42)
	value, err = ResolveIncrementalValue(p.Steps[0].Query.Parameters["checkpoint"], parameters)
	require.NoError(t, err)
	require.Equal(t, int64(42), value)
	value, err = ResolveIncrementalValue(p.Steps[0].Query.Parameters["input"], nil)
	require.NoError(t, err)
	require.Equal(t, "{{runtime.incremental.lower_bound.provider_field}}", value)
	_, err = ResolveIncrementalValue(p.Steps[0].Query.Parameters["checkpoint"], nil)
	require.ErrorContains(t, err, "unavailable in this source checkpoint")
}

func TestNullInterpolationRequiresWholeValueBinding(t *testing.T) {
	for _, expression := range []string{"prefix/{{runtime.input.value}}", "{{runtime.input.value}}/suffix"} {
		p := &pipelinespec.Pipeline{Steps: []pipelinespec.Step{{Query: &pipelinespec.QueryRef{Parameters: map[string]any{"value": expression}}}}}
		require.ErrorContains(t, ResolvePipelineInput(p, map[string]any{"value": nil}), "null cannot be interpolated")
	}
	p := &pipelinespec.Pipeline{Steps: []pipelinespec.Step{{Query: &pipelinespec.QueryRef{Parameters: map[string]any{"value": "{{runtime.input.value}}"}}}}}
	require.NoError(t, ResolvePipelineInput(p, map[string]any{"value": nil}))
	require.Nil(t, p.Steps[0].Query.Parameters["value"])
}
