package runtime

import (
	"testing"

	"github.com/Braidplane/cli/pkg/model"
	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"
	"github.com/stretchr/testify/require"
)

func TestRuntimeReferenceValidation(t *testing.T) {
	for _, expression := range []string{
		"{{runtime.rnu.date}}", "{{runtime.run.datetim}}", "{{runtime.run_date}}", "{{runtime.run_datetime}}",
		"{{runtime.run_slug}}", "{{runtime.sink.run_slug}}", "{{runtime.sink_idempotency_key}}", "{{runtime.sink_idempotency_class}}", "{{runtime.sink_replay_identity}}",
		"{{runtime.sink.unknown}}", "{{runtime.previous_success.id}}", "{{runtime.run.date",
		"{{runtime .run.date}}", "{{runtime}}",
		"{{runtime.env.key | add_months:no}}", "{{runtime.input.date | format:date | offset:1h}}",
		"{{runtime.secrets.key | format:date}}", "{{runtime.incremental.unknown}}",
	} {
		t.Run(expression, func(t *testing.T) {
			require.Error(t, ValidateRuntimeReferences(expression, true, true, true))
		})
	}
	for _, expression := range []string{
		"{{runtime.env.report_date | add_months:-1}}", "{{runtime.input.date | format:unix}}",
		"{{runtime.secrets.API_TOKEN}}", "{{runtime.incremental.lower_bound.provider_field}}",
		"{{runtime.run.slug}}", "{{runtime.run.datetime}}", "{{runtime.previous_success.exists}}",
	} {
		require.NoError(t, ValidateRuntimeReferences(expression, true, true, true), expression)
	}
	require.ErrorContains(t, ValidateRuntimeReferences("{{runtime.sink.replay_identity}}", true, false, true), "sink execution")
	require.ErrorContains(t, ValidateRuntimeReferences("{{runtime.incremental.lower_bound.cursor}}", true, false, false), "incremental source")
	require.ErrorContains(t, ValidateRuntimeReferences("select {{runtime.run.date | add_days:-1}}", false, false, false), "named connector parameters")
	pipeline := &pipelinespec.Pipeline{Steps: []pipelinespec.Step{{Slug: "transform", Transform: &pipelinespec.TransformSpec{SQL: "select '{{runtime.run.date}}'"}}}}
	require.ErrorContains(t, ValidatePipelineExecution(pipeline), "unavailable in transform SQL")
}

func TestDynamicDateSources(t *testing.T) {
	pipeline := func(expression string) *pipelinespec.Pipeline {
		return &pipelinespec.Pipeline{Steps: []pipelinespec.Step{{Slug: "read", Query: &pipelinespec.QueryRef{Parameters: map[string]any{"date": expression}}}}}
	}
	p := pipeline("{{runtime.env.report_date | add_months:-1}}")
	require.NoError(t, ValidatePipelineExecution(p))
	require.Equal(t, []string{"report_date"}, EnvironmentKeys(p))
	require.NoError(t, ResolvePipeline(p, []model.RuntimeVariable{{Key: "report_date", Value: "2024-03-31", ValueType: model.RuntimeVariableValueTypeDate}}))
	require.Equal(t, "2024-02-29", p.Steps[0].Query.Parameters["date"])
	require.ErrorContains(t, ResolvePipeline(pipeline("{{runtime.env.report_date | add_days:1}}"), []model.RuntimeVariable{{Key: "report_date", Value: "2024-03-31", ValueType: model.RuntimeVariableValueTypeText}}), "date-typed")
	for _, tc := range []struct {
		value any
		want  any
	}{
		{"2024-03-31", "2024-02-29"},
		{"2024-03-31T00:30:00+01:00", "2024-02-29T23:30:00Z"},
	} {
		p := pipeline("{{runtime.input.date | add_months:-1}}")
		require.NoError(t, ResolvePipelineInput(p, map[string]any{"date": tc.value}))
		require.Equal(t, tc.want, p.Steps[0].Query.Parameters["date"])
	}
	for _, value := range []any{nil, true, 42, map[string]any{}, "not-a-date", "{{runtime.run.date | add_days:1}}"} {
		require.Error(t, ResolvePipelineInput(pipeline("{{runtime.input.date | add_days:1}}"), map[string]any{"date": value}))
	}
	require.ErrorContains(t, ResolvePipelineInput(pipeline("{{runtime.input.date | add_days:1}}"), nil), "not defined")
}

func TestDateOperationsRejectOutOfRangeSources(t *testing.T) {
	for _, source := range []string{"0000-12-31", "0000-12-31T23:00:00Z", "0001-01-01T00:30:00+01:00"} {
		_, err := applyDateOperations(source, []string{"format:unix"}, true)
		require.ErrorContains(t, err, "years 0001 through 9999")
	}
}

func TestPlainPipelineMetadataReferencesNeedNoSinkContext(t *testing.T) {
	for _, name := range []string{RunSlugParamName, "runtime.previous_success.exists", "runtime.previous_success.finished_at"} {
		expression := "{{" + name + "}}"
		require.NoError(t, ValidateRuntimeReferences(expression, false, false, false))
		require.NoError(t, ValidateExecutionAvailability(expression, ExecutionContext{RunSlug: "attempt"}))
		require.ErrorContains(t, ValidateExecutionAvailability(expression, ExecutionContext{}), name+" requires pipeline execution")
	}
}
