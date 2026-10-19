package sql

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"testing"

	"github.com/Braidplane/cli/pkg/decimal"
	"github.com/Braidplane/cli/pkg/model"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
	"github.com/stretchr/testify/require"
)

func TestJoinKeyMatchesEquality(t *testing.T) {
	t.Parallel()
	values := []any{-1, int64(9007199254740993), uint64(math.MaxUint64), json.Number("9007199254740993"), "18446744073709551615", "01", "1.0", 1.0, float32(0.1), 0.1, "0.1", "capture", "", " 1 "}
	for _, a := range values {
		for _, b := range values {
			equal, err := whereValuesEqual(a, b)
			require.NoError(t, err)
			ka, aOK := transformJoinKey(a)
			kb, bOK := transformJoinKey(b)
			require.True(t, aOK && bOK)
			require.Equal(t, equal, ka == kb, "%#v / %#v", a, b)
		}
	}
	typedDecimal, err := decimal.Parse("1.00")
	require.NoError(t, err)
	for _, value := range []any{typedDecimal, true, map[string]any{"id": 1}, math.NaN(), math.Inf(1), json.Number("1e999"), "1e999"} {
		_, supported := transformJoinKey(value)
		require.False(t, supported, "%#v", value)
	}
}

func TestIndexedJoinMatchesReference(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		left, right []any
		on          string
	}{
		{"DuplicatesAndMissing", []any{1, 2, 3}, []any{1, 1, 4}, "l.id = r.id"},
		{"ExactNumbers", []any{int64(9007199254740993), uint64(math.MaxUint64), -2}, []any{"9007199254740993", json.Number("18446744073709551615"), "-2"}, "l.id = r.id"},
		{"NullKeys", []any{nil, 1, 2}, []any{nil, 1, 3}, "l.id = r.id"},
		{"BooleanFallback", []any{true, 1, "true"}, []any{true, "true", "1"}, "l.id = r.id"},
		{"CompositeAndResidual", []any{1, 2, 3}, []any{1, 1, 3}, "l.id = r.id AND l.tag = r.tag AND r.keep = 1"},
		{"ReversedEquality", []any{1, 2, 3}, []any{1, 1, 4}, "r.id = l.id"},
		{"NullResidualFallback", []any{nil, 1, 2}, []any{nil, 1, 3}, "l.id = r.id AND r.keep = 1"},
		{"NonEquijoinFallback", []any{1, 2, 3}, []any{1, 1, 4}, "l.id < r.id"},
		{"EmptyRight", []any{1, 2, 3}, nil, "l.id = r.id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inputs := map[string]*pipepayload.Payload{}
			for side, ids := range map[string][]any{"left_rows": tc.left, "right_rows": tc.right} {
				if len(ids) > 0 {
					extra := 1000
					if side == "left_rows" {
						extra = -1000
					}
					ids = append(append([]any(nil), ids...), extra, extra+1, extra+2)
				}
				rows := make([]map[string]any, len(ids))
				for i, id := range ids {
					rows[i] = map[string]any{"id": id, "tag": i % 2, "keep": i % 2}
				}
				inputs[side] = &pipepayload.Payload{Representation: model.ContractRepresentationRows, Rows: rows, ColumnNames: []string{"id", "tag", "keep"}, RowCount: len(rows)}
			}
			for _, kind := range []string{"JOIN", "LEFT JOIN"} {
				statement := "SELECT l.id AS id, r.id AS other FROM left_rows l " + kind + " right_rows r ON "
				actual, err := NewRuntime().Execute(Transform{SQL: statement + tc.on}, inputs)
				require.NoError(t, err)
				reference, err := NewRuntime().Execute(Transform{SQL: statement + "1 = 1 AND (" + tc.on + ")"}, inputs)
				require.NoError(t, err)
				require.Equal(t, reference.Rows, actual.Rows)
			}
		})
	}
}

func TestIndexedJoinPreservesErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, on string
		ids      []any
	}{
		{"ResidualBeforeEquality", "r.missing = 1 AND l.id = r.id", []any{1, 2, 3}},
		{"NullStillEvaluatesResidual", "l.id = r.id AND r.missing = 1", []any{nil, 2, 3}},
		{"InvalidNumericFallback", "l.id = r.id", []any{"1e999", 2, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ids := append(append([]any(nil), tc.ids...), 1000, 1001, 1002)
			rows := make([]map[string]any, len(ids))
			for i, id := range ids {
				rows[i] = map[string]any{"id": id}
			}
			inputs := map[string]*pipepayload.Payload{"a": {Representation: model.ContractRepresentationRows, Rows: rows}, "b": {Representation: model.ContractRepresentationRows, Rows: rows}}
			prefix := "SELECT l.id AS id FROM a l LEFT JOIN b r ON "
			_, actual := NewRuntime().Execute(Transform{SQL: prefix + tc.on}, inputs)
			_, reference := NewRuntime().Execute(Transform{SQL: prefix + "1 = 1 AND (" + tc.on + ")"}, inputs)
			require.Error(t, reference)
			require.EqualError(t, actual, reference.Error())
		})
	}
}

func TestSelectiveJoinDefaultLimits(t *testing.T) {
	t.Parallel()
	for _, size := range []int{128, 1000} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			rows := make([]map[string]any, size)
			for i := range rows {
				rows[i] = map[string]any{"id": i}
			}
			input := &pipepayload.Payload{Representation: model.ContractRepresentationRows, Rows: rows, RowCount: size}
			inputs := map[string]*pipepayload.Payload{"a": input, "b": input}
			var usage Usage
			runtime := NewRuntime(qualificationLimits()).WithObserver(func(u Usage, _ error) { usage = u })
			output, err := runtime.Execute(Transform{SQL: "SELECT a.id AS id FROM a LEFT JOIN b ON a.id = b.id"}, inputs)
			require.NoError(t, err)
			require.Equal(t, size, output.RowCount)
			require.Equal(t, rows, output.Rows)
			require.Equal(t, int64(5*size), usage.Work)
			require.Less(t, usage.Work, int64(size*size))
			require.LessOrEqual(t, usage.IntermediateRows, int64(qualificationLimits().MaxIntermediateRows))
			_, err = NewRuntime(Limits{MaxWork: usage.Work - 1}).Execute(Transform{SQL: "SELECT a.id AS id FROM a LEFT JOIN b ON a.id = b.id"}, inputs)
			require.ErrorIs(t, err, ErrWorkBudgetExceeded)
		})
	}
}

func TestJoinIndexResourceLimits(t *testing.T) {
	t.Parallel()
	inputs := map[string]*pipepayload.Payload{"a": budgetRowsPayload("id", 1, 1, 1, 1, 1, 1), "b": budgetRowsPayload("id", 1, 1, 1, 1, 1, 1)}
	statement := Transform{SQL: "SELECT a.id AS id FROM a LEFT JOIN b ON a.id = b.id"}
	for _, tc := range []struct {
		limits Limits
		want   error
	}{
		{Limits{MaxIntermediateBytes: 200}, ErrIntermediateBytesExceeded},
		{Limits{MaxIntermediateRows: 12}, ErrIntermediateRowsExceeded},
		{Limits{MaxOutputRows: 4}, ErrOutputRowsExceeded},
		{Limits{MaxWork: 10}, ErrWorkBudgetExceeded},
		{Limits{MaxDepth: 1}, ErrDepthBudgetExceeded},
	} {
		_, err := NewRuntime(tc.limits).Execute(statement, inputs)
		require.ErrorIs(t, err, tc.want)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewRuntime().ExecuteContext(ctx, statement, inputs)
	require.ErrorIs(t, err, ErrExecutionBudgetInterrupted)
}

func BenchmarkSelectiveJoinIndex(b *testing.B) {
	for _, size := range []int{128, 1000} {
		rows := make([]map[string]any, size)
		for i := range rows {
			rows[i] = map[string]any{"id": i}
		}
		input := &pipepayload.Payload{Representation: model.ContractRepresentationRows, Rows: rows, RowCount: size}
		inputs := map[string]*pipepayload.Payload{"a": input, "b": input}
		for _, path := range []string{"Indexed", "Reference"} {
			on := "a.id = b.id"
			limits := qualificationLimits()
			if path == "Reference" {
				on = "1 = 1 AND (a.id = b.id)"
				limits.MaxIntermediateRows = 2_000_000
				limits.MaxIntermediateBytes = 1 << 30
				limits.MaxWork = 2_000_000
			}
			b.Run(fmt.Sprintf("%s/%d", path, size), func(b *testing.B) {
				var usage Usage
				runtime := NewRuntime(limits).WithObserver(func(u Usage, _ error) { usage = u })
				b.ReportAllocs()
				for b.Loop() {
					output, err := runtime.Execute(Transform{SQL: "SELECT a.id AS id FROM a JOIN b ON " + on}, inputs)
					if err != nil || output.RowCount != size {
						b.Fatalf("rows/error: %v / %v", output, err)
					}
				}
				b.ReportMetric(float64(usage.Work), "work/op")
				b.ReportMetric(float64(usage.IntermediateRows), "intermediate-rows/op")
				b.ReportMetric(float64(usage.IntermediateBytes), "intermediate-B/op")
			})
		}
	}
}
