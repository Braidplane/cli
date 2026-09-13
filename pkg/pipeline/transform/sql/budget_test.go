package sql

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/model"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
	"github.com/Braidplane/cli/pkg/xerror"
)

func TestRuntimeJoinWorkBudgetCountsCandidatesBeforeFiltersAndLimit(t *testing.T) {
	t.Parallel()

	inputs := map[string]*pipepayload.Payload{
		"left_rows":  budgetRowsPayload("id", 1, 2),
		"right_rows": budgetRowsPayload("id", 1, 2),
	}
	transform := Transform{
		SQL: "SELECT left_rows.id AS id FROM left_rows JOIN right_rows " +
			"ON left_rows.id = right_rows.id LIMIT 1",
	}

	_, err := NewRuntime(Limits{MaxWork: 3}).Execute(transform, inputs)
	require.True(t, xerror.Is(err, ErrWorkBudgetExceeded), err)
	require.ErrorContains(t, err, "maximum work is 3 candidate operations")

	output, err := NewRuntime(Limits{MaxWork: 4}).Execute(transform, inputs)
	require.NoError(t, err)
	require.Len(t, output.Rows, 1)
}

func TestRuntimeWorkBudgetIsCumulativeAcrossCTEsAndChainedJoins(t *testing.T) {
	t.Parallel()

	transform := Transform{SQL: "WITH matched AS (" +
		"SELECT left_rows.id AS id FROM left_rows JOIN right_rows ON left_rows.id = right_rows.id" +
		") SELECT matched.id AS id FROM matched JOIN final_rows ON matched.id = final_rows.id"}
	inputs := map[string]*pipepayload.Payload{
		"left_rows":  budgetRowsPayload("id", 1, 2),
		"right_rows": budgetRowsPayload("id", 1, 2),
		"final_rows": budgetRowsPayload("id", 1, 2),
	}

	_, err := NewRuntime(Limits{MaxWork: 7}).Execute(transform, inputs)
	require.True(t, xerror.Is(err, ErrWorkBudgetExceeded), err)

	output, err := NewRuntime(Limits{MaxWork: 8}).Execute(transform, inputs)
	require.NoError(t, err)
	require.Len(t, output.Rows, 2)
}

func TestRuntimeIntermediateBudgetsAreCumulative(t *testing.T) {
	t.Parallel()

	transform := Transform{SQL: "SELECT id AS id FROM input"}
	input := map[string]*pipepayload.Payload{"input": budgetRowsPayload("id", 1)}

	_, err := NewRuntime(Limits{MaxIntermediateRows: 2}).Execute(transform, input)
	require.True(t, xerror.Is(err, ErrIntermediateRowsExceeded), err)

	output, err := NewRuntime(Limits{MaxIntermediateRows: 3}).Execute(transform, input)
	require.NoError(t, err)
	require.Equal(t, []map[string]any{{"id": 1}}, output.Rows)

	_, err = NewRuntime(Limits{MaxIntermediateBytes: 39}).Execute(transform, input)
	require.True(t, xerror.Is(err, ErrIntermediateBytesExceeded), err)

	output, err = NewRuntime(Limits{MaxIntermediateBytes: 40}).Execute(transform, input)
	require.NoError(t, err)
	require.Equal(t, []map[string]any{{"id": 1}}, output.Rows)

	largeInput := map[string]*pipepayload.Payload{
		"input": {
			Representation: model.ContractRepresentationRows,
			ColumnNames:    []string{"value"},
			RowCount:       1,
			Rows:           []map[string]any{{"value": strings.Repeat("x", 128)}},
		},
	}
	_, err = NewRuntime(Limits{MaxIntermediateBytes: 64}).Execute(
		Transform{SQL: "SELECT value AS value FROM input"},
		largeInput,
	)
	require.True(t, xerror.Is(err, ErrIntermediateBytesExceeded), err)
	require.NotContains(t, err.Error(), strings.Repeat("x", 16))
}

func TestRuntimeOutputBudgetsApplyAfterSQLLimit(t *testing.T) {
	t.Parallel()

	input := map[string]*pipepayload.Payload{"input": budgetRowsPayload("id", 1, 2)}

	_, err := NewRuntime(Limits{MaxOutputRows: 1}).Execute(
		Transform{SQL: "SELECT id AS id FROM input"},
		input,
	)
	require.True(t, xerror.Is(err, ErrOutputRowsExceeded), err)

	output, err := NewRuntime(Limits{MaxOutputRows: 1}).Execute(
		Transform{SQL: "SELECT id AS id FROM input LIMIT 1"},
		input,
	)
	require.NoError(t, err)
	require.Len(t, output.Rows, 1)

	jsonTransform := Transform{
		SQL:        "SELECT id AS id FROM input LIMIT 1",
		OutputKind: model.TransformOutputKindJSON,
	}
	_, err = NewRuntime(Limits{MaxOutputBytes: 9}).Execute(
		jsonTransform,
		input,
	)
	require.True(t, xerror.Is(err, ErrOutputBytesExceeded), err)

	output, err = NewRuntime(Limits{MaxOutputBytes: 10}).Execute(
		jsonTransform,
		input,
	)
	require.NoError(t, err)
	require.JSONEq(t, `[{"id":1}]`, string(output.Document))

	_, err = NewRuntime(Limits{MaxOutputBytes: 8}).Execute(
		Transform{
			SQL:        "SELECT id AS id FROM input LIMIT 1",
			OutputKind: model.TransformOutputKindJSON,
		},
		input,
	)
	require.True(t, xerror.Is(err, ErrOutputBytesExceeded), err)
}

func TestRuntimeDepthBudgetRejectsNestedJoin(t *testing.T) {
	t.Parallel()

	inputs := map[string]*pipepayload.Payload{
		"left_rows":  budgetRowsPayload("id", 1),
		"right_rows": budgetRowsPayload("id", 1),
	}
	transform := Transform{SQL: "SELECT left_rows.id AS id FROM left_rows " +
		"JOIN right_rows ON left_rows.id = right_rows.id"}

	_, err := NewRuntime(Limits{MaxDepth: 1}).Execute(transform, inputs)
	require.True(t, xerror.Is(err, ErrDepthBudgetExceeded), err)

	output, err := NewRuntime(Limits{MaxDepth: 2}).Execute(transform, inputs)
	require.NoError(t, err)
	require.Len(t, output.Rows, 1)
}

func TestRuntimeDepthBudgetRejectsRecursiveExpressions(t *testing.T) {
	t.Parallel()

	input := map[string]*pipepayload.Payload{"input": budgetRowsPayload("id", 1)}
	deepBinary := "id"
	deepWhere := "id = 1"
	deepFunction := "id"
	for range 32 {
		deepBinary += " + 1"
		deepWhere = "(id = 1 AND " + deepWhere + ")"
		deepFunction = "JSON_ARRAY(" + deepFunction + ")"
	}

	for name, sqlText := range map[string]string{
		"binary projection": "SELECT " + deepBinary + " AS value FROM input",
		"where predicate":   "SELECT id AS id FROM input WHERE " + deepWhere,
		"function call":     "SELECT " + deepFunction + " AS value FROM input",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewRuntime(Limits{MaxDepth: 8}).Execute(Transform{SQL: sqlText}, input)
			require.True(t, xerror.Is(err, ErrDepthBudgetExceeded), err)
		})
	}
}

func TestRecursiveExpressionEvaluationHonorsCancellation(t *testing.T) {
	t.Parallel()

	args := strings.TrimSuffix(strings.Repeat("id,", 512), ",")
	selectStmt, _, err := parseTransformSelect("SELECT JSON_ARRAY(" + args + ") AS items FROM input")
	require.NoError(t, err)

	ctx := newInterruptAfterChecksContext(16)
	budget := newExecutionBudget(ctx, Limits{MaxDepth: 8})
	_, err = evaluateTransformProjectionExpr(
		selectStmt.Fields.Fields[0].Expr,
		map[string]any{"id": 1},
		budget,
		1,
	)
	require.True(t, xerror.Is(err, ErrExecutionBudgetInterrupted), err)
	require.GreaterOrEqual(t, ctx.checks.Load(), int64(16))
}

func TestRuntimeExecuteContextHonorsCancellationAndTimeout(t *testing.T) {
	t.Parallel()

	transform := Transform{SQL: "SELECT id AS id FROM input"}
	input := map[string]*pipepayload.Payload{"input": budgetRowsPayload("id", 1)}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewRuntime().ExecuteContext(ctx, transform, input)
	require.True(t, xerror.Is(err, ErrExecutionBudgetInterrupted), err)
	require.ErrorContains(t, err, "context canceled")

	_, err = NewRuntime(Limits{Timeout: time.Nanosecond}).Execute(transform, input)
	require.True(t, xerror.Is(err, ErrExecutionBudgetInterrupted), err)
}

func TestRuntimeRejectsInvalidLimitsWithoutExecuting(t *testing.T) {
	t.Parallel()

	_, err := NewRuntime(Limits{MaxWork: -1}).Execute(
		Transform{SQL: "SELECT id AS id FROM input"},
		map[string]*pipepayload.Payload{"input": budgetRowsPayload("id", 1)},
	)
	require.True(t, xerror.Is(err, ErrInvalidRuntimeLimits), err)
	require.ErrorContains(t, err, "max work must be non-negative")
}

func TestRuntimeObserverReportsOnlyAggregatePeakUsageAndViolation(t *testing.T) {
	t.Parallel()

	var observed Usage
	var observedErr error
	runtime := NewRuntime(Limits{MaxWork: 3}).WithObserver(func(usage Usage, err error) {
		observed = usage
		observedErr = err
	})
	_, err := runtime.Execute(
		Transform{SQL: "SELECT left_rows.id AS id FROM left_rows JOIN right_rows ON left_rows.id = right_rows.id"},
		map[string]*pipepayload.Payload{
			"left_rows":  budgetRowsPayload("id", 1, 2),
			"right_rows": budgetRowsPayload("id", 1, 2),
		},
	)
	require.True(t, xerror.Is(err, ErrWorkBudgetExceeded), err)
	require.True(t, xerror.Is(observedErr, ErrWorkBudgetExceeded), observedErr)
	require.Positive(t, observed.IntermediateRows)
	require.Positive(t, observed.IntermediateBytes)
	require.Positive(t, observed.MaxDepth)
	require.Positive(t, observed.Duration)
	require.Zero(t, observed.OutputRows)
	require.NotContains(t, strings.ToLower(strings.TrimSpace(observedErr.Error())), "left_rows.id")
}

func TestCheckedProductFailsClosedOnOverflow(t *testing.T) {
	t.Parallel()

	_, err := checkedProduct(maxInt(), 2)
	if strconvIntSize() == 64 {
		require.True(t, xerror.Is(err, ErrWorkBudgetExceeded), err)
		return
	}
	require.NoError(t, err)
}

func budgetRowsPayload(column string, values ...int) *pipepayload.Payload {
	rows := make([]map[string]any, 0, len(values))
	for _, value := range values {
		rows = append(rows, map[string]any{column: value})
	}
	return &pipepayload.Payload{
		Representation: model.ContractRepresentationRows,
		ColumnNames:    []string{column},
		RowCount:       len(rows),
		Rows:           rows,
	}
}

func strconvIntSize() int {
	return 32 << (^uint(0) >> 63)
}

type interruptAfterChecksContext struct {
	context.Context
	checks      atomic.Int64
	interrupted atomic.Bool
	interruptAt int64
	done        chan struct{}
}

func newInterruptAfterChecksContext(interruptAt int64) *interruptAfterChecksContext {
	done := make(chan struct{})
	return &interruptAfterChecksContext{
		Context: context.Background(), interruptAt: interruptAt, done: done,
	}
}

func (c *interruptAfterChecksContext) Done() <-chan struct{} {
	if c.checks.Add(1) >= c.interruptAt && c.interrupted.CompareAndSwap(false, true) {
		close(c.done)
	}
	return c.done
}

func (c *interruptAfterChecksContext) Err() error {
	if c.interrupted.Load() {
		return context.Canceled
	}
	return nil
}
