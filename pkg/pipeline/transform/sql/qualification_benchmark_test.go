package sql

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Braidplane/cli/pkg/model"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
	"github.com/Braidplane/cli/pkg/xerror"
)

func BenchmarkSQLQualification(b *testing.B) {
	for _, size := range []int{32, 64, 96, 128} {
		rows := make([]map[string]any, size)
		for i := range rows {
			rows[i] = map[string]any{"id": i}
		}
		input := &pipepayload.Payload{Representation: model.ContractRepresentationRows, Rows: rows, RowCount: size}
		for _, tc := range []struct {
			name, statement string
			wantRows        int
			wantError       error
		}{
			{"selective_join", fmt.Sprintf("SELECT orders.id AS v FROM orders JOIN customers ON orders.id = customers.id WHERE orders.id < %d", size/8), size / 8, nil},
			{"cte_prefilter_join", fmt.Sprintf("WITH chosen AS (SELECT id FROM orders WHERE id < %d) SELECT chosen.id AS v FROM chosen JOIN customers ON chosen.id = customers.id", size/8), size / 8, nil},
			{"two_ctes", "WITH first_rows AS (SELECT id FROM orders), second_rows AS (SELECT id FROM first_rows) SELECT id AS v FROM second_rows", size, nil},
		} {
			b.Run(fmt.Sprintf("%s/%d", tc.name, size), func(b *testing.B) {
				var usage Usage
				runtime := NewRuntime(qualificationLimits()).WithObserver(func(u Usage, _ error) { usage = u })
				inputs := map[string]*pipepayload.Payload{"orders": input, "customers": input}
				b.ReportAllocs()
				for b.Loop() {
					output, err := runtime.Execute(Transform{SQL: tc.statement}, inputs)
					if tc.wantError != nil {
						if !xerror.Is(err, tc.wantError) {
							b.Fatalf("expected %v, got %v", tc.wantError, err)
						}
					} else if err != nil || output.RowCount != tc.wantRows {
						b.Fatalf("rows/error: %v / %v", output, err)
					}
				}
				b.ReportMetric(float64(usage.Work), "work/op")
				b.ReportMetric(float64(usage.IntermediateRows), "intermediate-rows/op")
				b.ReportMetric(float64(usage.IntermediateBytes), "intermediate-B/op")
			})
		}
		b.Run(fmt.Sprintf("three_transforms/%d", size), func(b *testing.B) {
			runtime := NewRuntime(qualificationLimits())
			b.ReportAllocs()
			for b.Loop() {
				payload := input
				for range 3 {
					output, err := runtime.Execute(Transform{SQL: "SELECT id + 1 AS id FROM input"}, map[string]*pipepayload.Payload{"input": payload})
					if err != nil || output.RowCount != size {
						b.Fatalf("rows/error: %v / %v", output, err)
					}
					payload = &pipepayload.Payload{Representation: model.ContractRepresentationRows, Rows: output.Rows, RowCount: output.RowCount}
				}
				if fmt.Sprint(payload.Rows[0]["id"]) != "3" {
					b.Fatal("chain did not consume prior output")
				}
			}
		})
	}
	for _, tc := range []struct {
		name, value string
		wantError   bool
	}{
		{"decimal", "0.1", false},
		{"integer", "9007199254740993", false},
		{"hostile_precision", strings.Repeat("9", 129), true},
		{"hostile_exponent", "1e1000000000", true},
	} {
		b.Run("numeric/"+tc.name, func(b *testing.B) {
			input := map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationJSON, Document: json.RawMessage(`[{"n":` + tc.value + `}]`)}}
			runtime := NewRuntime(qualificationLimits())
			b.ReportAllocs()
			for b.Loop() {
				_, err := runtime.Execute(Transform{SQL: "SELECT n + n AS v FROM input"}, input)
				if tc.wantError {
					if !xerror.Is(err, ErrNumericConversion) {
						b.Fatalf("expected numeric rejection: %v", err)
					}
				} else if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
