package spec

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/connector/catalog"
	"github.com/Braidplane/cli/pkg/model"
)

func TestSinkBackendForOperationType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		operationType string
		wantBackend   model.SinkBackend
		wantSupported bool
	}{
		{name: "sql", operationType: OperationTypeSQL, wantBackend: model.SinkBackendDB, wantSupported: true},
		{name: "mongodb", operationType: OperationTypeMongoDB, wantBackend: model.SinkBackendMongo, wantSupported: true},
		{name: "http", operationType: OperationTypeHTTP, wantBackend: model.SinkBackendHTTP, wantSupported: true},
		{name: "ftp", operationType: OperationTypeFTP, wantBackend: model.SinkBackendFTP, wantSupported: true},
		{name: "git", operationType: OperationTypeGit, wantBackend: model.SinkBackendGit, wantSupported: true},
		{name: "s3", operationType: OperationTypeS3, wantBackend: model.SinkBackendS3, wantSupported: true},
		{name: "BigQuery", operationType: OperationTypeBigQuery, wantBackend: model.SinkBackendDB, wantSupported: true},
		{name: "hubspot", operationType: OperationTypeHubSpot, wantBackend: model.SinkBackendHTTP, wantSupported: true},
		{name: "unknown", operationType: "unknown", wantBackend: model.SinkBackendUnknown, wantSupported: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			backend, supported := SinkBackendForOperationType(tc.operationType)

			require.Equal(t, tc.wantBackend, backend)
			require.Equal(t, tc.wantSupported, supported)
			require.Equal(t, tc.wantSupported, IsSupportedSinkOperationType(tc.operationType))
		})
	}
}

func TestIsSupportedQueryOperationType(t *testing.T) {
	t.Parallel()

	require.True(t, IsSupportedQueryOperationType(OperationTypeSQL))
	require.True(t, IsSupportedQueryOperationType(OperationTypeHTTP))
	require.True(t, IsSupportedQueryOperationType(OperationTypeHubSpot))
	require.True(t, IsSupportedQueryOperationType(OperationTypeFTP))
	require.True(t, IsSupportedQueryOperationType(OperationTypeGit))
	require.True(t, IsSupportedQueryOperationType(OperationTypeS3))
	require.True(t, IsSupportedQueryOperationType(OperationTypeMongoDB))
	require.True(t, IsSupportedQueryOperationType(OperationTypeBigQuery))
	require.False(t, IsSupportedQueryOperationType("unknown"))
}

func TestAllConnectorCapabilitiesMirrorCatalogDescriptors(t *testing.T) {
	t.Parallel()

	capabilitiesByType := make(map[model.DataSourceType]ConnectorCapability)
	for _, capability := range AllConnectorCapabilities() {
		capabilitiesByType[capability.DataSourceType] = capability
	}

	for _, descriptor := range catalog.AllDescriptors() {
		capability, ok := capabilitiesByType[descriptor.Type]
		require.Truef(t, ok, "missing capability for %s", descriptor.Type)
		require.Equal(t, string(descriptor.SchemaFamily), capability.SchemaFamily)
		require.Equal(t, descriptor.QueryOperationType, capability.QueryOperationType)
		require.Equal(t, descriptor.SinkBackend, capability.SinkBackend)
		require.Equal(t, descriptor.SupportsPipelineSource, capability.SupportsPipelineSource)
		require.Equal(t, descriptor.SupportsPipelineSink, capability.SupportsPipelineSink)
		expectedReplayStrategies := make([]SinkIdempotencyClass, 0, len(descriptor.SupportedSinkReplayStrategies))
		for _, strategy := range descriptor.SupportedSinkReplayStrategies {
			idempotencyClass, err := SinkIdempotencyClassString(strategy.String())
			require.NoError(t, err)
			expectedReplayStrategies = append(expectedReplayStrategies, idempotencyClass)
		}
		require.ElementsMatch(t, expectedReplayStrategies, capability.SupportedSinkReplayStrategies)
		require.ElementsMatch(
			t,
			descriptor.IncrementalSourceCheckpointKinds,
			capability.IncrementalSourceCheckpointKinds,
		)
		require.Equal(t, descriptor.ForcedReadOnly, capability.ForcedReadOnly)
	}

	require.Len(t, capabilitiesByType, len(catalog.AllDescriptors()))
}
