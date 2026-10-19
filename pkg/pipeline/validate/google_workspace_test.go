package validate

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/model"
	spec "github.com/Braidplane/cli/pkg/pipeline/spec"
	"github.com/Braidplane/cli/pkg/slug"
)

func TestWorkspaceInlinePipelineRoles(t *testing.T) {
	read := map[string]any{"type": "google_workspace", "resource": "spreadsheet", "spreadsheet_id": "book", "operation": "read", "worksheet_id": 0, "range": "A1:B3"}
	update := map[string]any{"type": "google_workspace", "resource": "spreadsheet", "spreadsheet_id": "book", "operation": "update", "worksheet_id": 0, "range": "A2:B3", "columns": []string{"a", "b"}, "rows": "{{input}}"}
	query := &spec.QueryRef{OperationType: spec.OperationTypeGoogleWorkspace, OperationInline: read}
	require.NoError(t, validateQueryOperationContract(query))
	representation, _ := queryOutputFacets(query)
	require.Equal(t, model.ContractRepresentationRows, representation)
	sink := &spec.SinkSpec{DataSourceSlug: slug.Slug("sheet"), OperationType: spec.OperationTypeGoogleWorkspace, OperationInline: update}
	require.NoError(t, validateSinkOperationContract(sink))
	update["rows"] = []map[string]any{{"a": 1, "b": 2}, {"a": 3, "b": 4}}
	require.ErrorContains(t, validateSinkOperationContract(sink), "{{input}}")
	update["rows"] = "{{input}}"
	query.OperationInline = map[string]any{"type": "google_workspace", "resource": "spreadsheet", "spreadsheet_id": "book", "invalid": make(chan int)}
	require.Error(t, validateQueryOperationContract(query))
	sink.OperationInline = map[string]any{"type": "google_workspace", "resource": "spreadsheet", "spreadsheet_id": "book", "invalid": make(chan int)}
	require.Error(t, validateSinkOperationContract(sink))
	sink.OperationInline = update
	query.OperationInline = update
	require.Error(t, validateQueryOperationContract(query))
	sink.OperationInline = read
	require.Error(t, validateSinkOperationContract(sink))
	require.True(t, spec.SupportsSinkIdempotencyClass(model.DataSourceTypeGoogleWorkspace, spec.SinkIdempotencyClassNaturallyIdempotent))
	require.False(t, spec.SupportsSinkIdempotencyClass(model.DataSourceTypeGoogleWorkspace, spec.SinkIdempotencyClassIdempotentWithKey))
}
