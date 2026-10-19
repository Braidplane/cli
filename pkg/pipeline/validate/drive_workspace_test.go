package validate

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/model"
	spec "github.com/Braidplane/cli/pkg/pipeline/spec"
	"github.com/Braidplane/cli/pkg/slug"
)

func TestDriveWorkspaceInlineRolesAndFacets(t *testing.T) {
	read := &spec.QueryRef{OperationType: spec.OperationTypeGoogleWorkspace, OperationInline: map[string]any{"type": "google_workspace", "resource": "file", "operation": "read", "file_id": "report", "format": "file"}}
	list := &spec.QueryRef{OperationType: spec.OperationTypeGoogleWorkspace, OperationInline: map[string]any{"type": "google_workspace", "resource": "file", "operation": "list", "folder_id": "folder"}}
	require.NoError(t, validateQueryOperationContract(read))
	require.NoError(t, validateQueryOperationContract(list))
	representation, _ := queryOutputFacets(read)
	require.Equal(t, model.ContractRepresentationFile, representation, "opaque files must not claim rows")

	update := &spec.SinkSpec{DataSourceSlug: slug.Slug("workspace"), OperationType: spec.OperationTypeGoogleWorkspace, IdempotencyClass: spec.SinkIdempotencyClassNaturallyIdempotent, OperationInline: map[string]any{"type": "google_workspace", "resource": "file", "operation": "update", "file_id": "report", "format": "json", "content": "{{input}}"}}
	create := &spec.SinkSpec{DataSourceSlug: slug.Slug("workspace"), OperationType: spec.OperationTypeGoogleWorkspace, IdempotencyClass: spec.SinkIdempotencyClassUnsafe, OperationInline: map[string]any{"type": "google_workspace", "resource": "file", "operation": "create", "folder_id": "folder", "name": "crm.json", "format": "json", "content": "{{input}}"}}
	require.NoError(t, validateSinkOperationContract(update))
	require.NoError(t, validateSinkOperationContract(create))

	create.IdempotencyClass = spec.SinkIdempotencyClassNaturallyIdempotent
	require.ErrorContains(t, validateSinkOperationContract(create), "unsafe replay")
	create.IdempotencyClass = spec.SinkIdempotencyClassUnsafe
	create.OperationInline["content"] = []any{}
	require.ErrorContains(t, validateSinkOperationContract(create), "{{input}}")

	missingTarget := &spec.QueryRef{OperationType: spec.OperationTypeGoogleWorkspace, OperationInline: map[string]any{"type": "google_workspace", "resource": "spreadsheet", "operation": "read", "worksheet_id": 0, "range": "A1:A1"}}
	require.Error(t, validateQueryOperationContract(missingTarget), "worksheets require an explicit spreadsheet ID")
}
