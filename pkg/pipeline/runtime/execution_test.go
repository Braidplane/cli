package runtime

import (
	"testing"
	"time"

	"github.com/Braidplane/cli/pkg/model"
	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"

	"github.com/Braidplane/cli/pkg/xerror"
	"github.com/stretchr/testify/require"
)

func TestExecutionExpressions(t *testing.T) {
	at := time.Date(2024, 3, 31, 0, 30, 0, 0, time.FixedZone("east", 3600))
	snapshot := ExecutionContext{At: at}
	for _, tc := range []struct {
		expression string
		want       any
	}{
		{"{{runtime.run.date}}", "2024-03-30"},
		{"{{runtime.run.datetime | offset:2h}}", "2024-03-31T01:30:00Z"},
		{"{{runtime.run.date | add_days:-30}}", "2024-02-29"},
		{"{{runtime.run.date | add_months:-1}}", "2024-02-29"},
		{"{{runtime.run.datetime | offset:-24h | format:date}}", "2024-03-29"},
		{"{{runtime.run.datetime | format:unix}}", at.Unix()},
		{"prefix/{{ runtime.run.datetime | format:date }}", "prefix/2024-03-30"},
		{"{{runtime.previous_success.exists}}", false},
		{"{{runtime.previous_success.finished_at}}", nil},
		{"{{runtime.env.key}}", "{{runtime.env.key}}"},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			got, err := ResolveExecutionValue(tc.expression, snapshot)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestExecutionExpressionsRejectInvalidOperations(t *testing.T) {
	snapshot := ExecutionContext{At: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}
	for _, expression := range []string{
		"{{runtime.run.date | eval:1}}", "{{runtime.run.date | offset:1d}}",
		"{{runtime.run.date | add_days:1.2}}", "{{runtime.run.date | add_months:120001}}",
		"{{runtime.run.date | format:local}}", "{{runtime.run.date | format:date | offset:1h}}",
		"{{runtime.previous_success.finished_at | offset:1h}}", "prefix/{{runtime.previous_success.finished_at}}",
		"{{runtime.previous_success.exists | format:date}}", "{{runtime.previous_success.unknown}}",
		"{{runtime.run.date | add_days:1 | add_days:1 | add_days:1 | add_days:1 | add_days:1}}",
		"{{runtime.run.date | add_months:120000 | format:unix}}",
	} {
		t.Run(expression, func(t *testing.T) {
			_, err := ResolveExecutionValue(expression, snapshot)
			require.True(t, xerror.Is(err, xerror.InvalidInput(xerror.PipelineRun)), "%v", err)
		})
	}
}

func TestExecutionHistoryReferences(t *testing.T) {
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	snapshot := ExecutionContext{At: at, PreviousSuccess: &SuccessfulRun{RunSlug: "run-one", StartedAt: at.Add(-time.Hour), FinishedAt: at}}
	got, err := ResolveExecutionValue(map[string]any{"id": "{{runtime.previous_success.run_slug}}", "exists": "{{runtime.previous_success.exists}}", "since": "{{runtime.previous_success.started_at | offset:-1h}}"}, snapshot)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"id": "run-one", "exists": true, "since": "2023-12-31T22:00:00Z"}, got)
}

func TestExecutionMonthOffsetClampsToTargetMonth(t *testing.T) {
	for _, tc := range []struct {
		date   string
		months string
		want   string
	}{
		{"2024-03-31", "-1", "2024-02-29"},
		{"2025-03-31", "-1", "2025-02-28"},
		{"2024-01-31", "1", "2024-02-29"},
		{"2024-02-29", "12", "2025-02-28"},
		{"2024-01-31", "-1", "2023-12-31"},
		{"2024-03-31", "0", "2024-03-31"},
	} {
		at, err := time.Parse(time.DateOnly, tc.date)
		require.NoError(t, err)
		value, err := ResolveExecutionValue("{{runtime.run.date | add_months:"+tc.months+"}}", ExecutionContext{At: at})
		require.NoError(t, err)
		require.Equal(t, tc.want, value)
	}
}

func TestPipelineExecutionValidationAndLiteralBoundary(t *testing.T) {
	pipeline := &pipelinespec.Pipeline{Steps: []pipelinespec.Step{{Slug: "read", Query: &pipelinespec.QueryRef{Parameters: map[string]any{"since": "{{runtime.run.date | add_days:-1}}"}, OperationInline: map[string]any{"statement": "select {{since}}"}}}}}
	require.NoError(t, ValidatePipelineExecution(pipeline))
	pipeline.Steps = append(pipeline.Steps, pipelinespec.Step{Slug: "write", Sink: &pipelinespec.SinkSpec{Config: map[string]any{"key": "{{runtime.run.slug}}/{{runtime.sink.replay_identity}}"}}})
	require.NoError(t, ValidatePipelineExecution(pipeline), "sink identity placeholders remain connector-owned")
	for _, invalid := range []string{"{{runtime.run.date.addDate(-1)}}", "{{\truntime.date}}", "{{runtime.run.date | add_days:1.5}}", "{{runtime.env.date | offset:1d}}", "{{runtime.previous_success.id}}", "{{runtime.run.date"} {
		pipeline.Steps[0].Query.Parameters["since"] = invalid
		require.ErrorContains(t, ValidatePipelineExecution(pipeline), "runtime date expression")
	}
	pipeline.Steps[0].Query.Parameters["since"] = "{{runtime.run.date | offset:-24h}}"
	pipeline.Steps[0].Query.OperationInline["statement"] = "select {{runtime.run.date | offset:-24h}}"
	require.ErrorContains(t, ValidatePipelineExecution(pipeline), "named connector parameters")
	pipeline.Steps[0].Query.OperationInline["statement"] = "select {{runtime.run.date}}"
	pipeline.Steps[0].Query.Parameters["since"] = "{{runtime.env.value}}"
	require.NoError(t, ResolvePipeline(pipeline, []model.RuntimeVariable{{Key: "value", Value: "{{runtime.run.datetime | offset:1h}}", ValueType: model.RuntimeVariableValueTypeText}}))
	require.NoError(t, ResolvePipelineExecution(pipeline, ExecutionContext{RunSlug: "run-one", At: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}))
	require.Equal(t, LiteralString("{{runtime.run.datetime | offset:1h}}"), pipeline.Steps[0].Query.Parameters["since"])
}

func TestLiteralLocationsRestoreSuppliedRuntimeSyntax(t *testing.T) {
	pipeline := &pipelinespec.Pipeline{Steps: []pipelinespec.Step{{Query: &pipelinespec.QueryRef{Parameters: map[string]any{
		"value": []any{map[string]any{"literal": LiteralString("{{runtime.run.date}}")}},
	}}}}}
	paths := LiteralLocations(pipeline)
	require.Equal(t, []LiteralLocation{{Path: []string{"steps", "0", "query", "parameters", "value", "0", "literal"}}}, paths)
	pipeline.Steps[0].Query.Parameters["value"].([]any)[0].(map[string]any)["literal"] = "{{runtime.run.date}}"
	require.True(t, RestoreLiteralLocations(pipeline, paths))
	require.Equal(t, LiteralString("{{runtime.run.date}}"), pipeline.Steps[0].Query.Parameters["value"].([]any)[0].(map[string]any)["literal"])
}

func TestExecutionValidationDoesNotAssumeSyntheticDateRange(t *testing.T) {
	expression := "{{runtime.run.date | add_months:-24000}}"
	pipeline := &pipelinespec.Pipeline{Steps: []pipelinespec.Step{{Slug: "read", Query: &pipelinespec.QueryRef{Parameters: map[string]any{"since": expression}}}}}
	require.NoError(t, ValidatePipelineExecution(pipeline))
	value, err := ResolveExecutionValue(expression, ExecutionContext{At: time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	require.Equal(t, "0026-10-22", value)
	_, err = ResolveExecutionValue(expression, ExecutionContext{At: time.Date(1999, 10, 22, 0, 0, 0, 0, time.UTC)})
	require.ErrorContains(t, err, "years 0001 through 9999")
}
