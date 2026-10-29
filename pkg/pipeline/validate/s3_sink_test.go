package validate

import (
	"testing"

	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"
	"github.com/Braidplane/cli/pkg/xerror"
	"github.com/stretchr/testify/require"
)

func TestS3SinkOperationConfiguration(t *testing.T) {
	for _, config := range []map[string]any{
		nil,
		{"key": ""},
		{"key": "  "},
		{"key": 42},
		{"key": "out.json", "bucket": false},
		{"key": "out.json", "overwrite": "true"},
		{"key": "out.json", "metadata": map[string]any{"private-secret-name": 42}},
	} {
		sink := &pipelinespec.SinkSpec{OperationType: "s3", DataSourceSlug: "objects", OperationInline: map[string]any{"operation": "write", "key": "inline-does-not-replace-config"}, Config: config}
		err := validateSinkOperationContract(sink)
		require.ErrorIs(t, err, xerror.InvalidInput(xerror.Pipeline))
		require.Contains(t, xerror.PublicDetail(err), "sink.config.")
		require.NotContains(t, err.Error(), "private-secret-name")
		require.NotContains(t, err.Error(), "inline-does-not-replace-config")
	}
	require.NoError(t, validateSinkOperationContract(&pipelinespec.SinkSpec{OperationType: "s3", DataSourceSlug: "objects", OperationRef: "write", Config: map[string]any{"key": "{{runtime.run.slug}}/out.json", "overwrite": true}}))
}
