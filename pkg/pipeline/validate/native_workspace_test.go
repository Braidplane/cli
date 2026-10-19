package validate

import (
	"testing"

	"github.com/Braidplane/cli/pkg/model"
	spec "github.com/Braidplane/cli/pkg/pipeline/spec"
	"github.com/Braidplane/cli/pkg/slug"
	"github.com/stretchr/testify/require"
)

func TestNativeWorkspacePipelineRolesAndReplay(t *testing.T) {
	for _, resource := range []string{"document", "presentation"} {
		for _, op := range []string{"read", "export"} {
			query := &spec.QueryRef{OperationType: spec.OperationTypeGoogleWorkspace, OperationInline: map[string]any{"type": "google_workspace", "resource": resource, "operation": op, "file_id": "native"}}
			require.NoError(t, validateQueryOperationContract(query))
			representation, _ := queryOutputFacets(query)
			expected := model.ContractRepresentationRows
			if op == "export" {
				expected = model.ContractRepresentationFile
			}
			require.Equal(t, expected, representation)
		}
		for _, op := range []string{"create", "update"} {
			inline := map[string]any{"type": "google_workspace", "resource": resource, "operation": op}
			if op == "create" {
				inline["folder_id"] = "folder"
				inline["name"] = "Blank"
			} else {
				inline["file_id"] = "native"
				inline["match_text"] = "x"
				inline["content"] = "{{input}}"
			}
			sink := &spec.SinkSpec{DataSourceSlug: slug.Slug("workspace"), OperationType: spec.OperationTypeGoogleWorkspace, OperationInline: inline, IdempotencyClass: spec.SinkIdempotencyClassUnsafe}
			require.NoError(t, validateSinkOperationContract(sink))
			sink.IdempotencyClass = spec.SinkIdempotencyClassNaturallyIdempotent
			require.Error(t, validateSinkOperationContract(sink))
			require.Error(t, validateQueryOperationContract(&spec.QueryRef{OperationType: spec.OperationTypeGoogleWorkspace, OperationInline: inline}))
		}
	}
}
