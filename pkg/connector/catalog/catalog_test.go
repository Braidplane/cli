package catalog

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/model"
)

func TestResourceScopeCapabilitiesFailClosed(t *testing.T) {
	t.Parallel()
	for _, descriptor := range AllDescriptors() {
		capability := descriptor.ResourceScopeCapability()
		if descriptor.Type == model.DataSourceTypeS3 {
			require.Equal(t, "s3/relative-key/v1", capability.PolicyID)
			require.Equal(t, []model.ContractRepresentation{model.ContractRepresentationRows}, capability.Representations)
			require.Equal(t, []string{"key"}, capability.DynamicSelectors)
			require.Contains(t, capability.FixedFields, "bucket")
			require.Empty(t, capability.UnsupportedReason)
		} else {
			require.Empty(t, capability.PolicyID)
			require.Empty(t, capability.Representations)
			require.Empty(t, capability.DynamicSelectors)
			require.NotEmpty(t, capability.UnsupportedReason)
		}
	}
}

func TestDescriptorForDefinesConnectorDomainContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		dsType           model.DataSourceType
		schemaFamily     SchemaFamily
		queryOpType      string
		sinkBackend      model.SinkBackend
		source           bool
		sink             bool
		replayStrategies []SinkReplayStrategy
		incremental      []model.IncrementalCheckpointKind
		forcedReadOnly   bool
	}{
		{
			name:         "postgres",
			dsType:       model.DataSourceTypePostgres,
			schemaFamily: SchemaFamilySQL,
			queryOpType:  OperationTypeSQL,
			sinkBackend:  model.SinkBackendDB,
			source:       true,
			sink:         true,
			replayStrategies: []SinkReplayStrategy{
				SinkReplayStrategyNaturallyIdempotent,
				SinkReplayStrategyDeduplicatedByLedger,
				SinkReplayStrategyUnsafe,
			},
			incremental: []model.IncrementalCheckpointKind{model.IncrementalCheckpointKindWatermark},
		},
		{
			name:         "mysql",
			dsType:       model.DataSourceTypeMySQL,
			schemaFamily: SchemaFamilySQL,
			queryOpType:  OperationTypeSQL,
			sinkBackend:  model.SinkBackendDB,
			source:       true,
			sink:         true,
			replayStrategies: []SinkReplayStrategy{
				SinkReplayStrategyNaturallyIdempotent,
				SinkReplayStrategyDeduplicatedByLedger,
				SinkReplayStrategyUnsafe,
			},
			incremental: []model.IncrementalCheckpointKind{model.IncrementalCheckpointKindWatermark},
		},
		{
			name:         "BigQuery",
			dsType:       model.DataSourceTypeBigQuery,
			schemaFamily: SchemaFamilySQL,
			queryOpType:  OperationTypeBigQuery,
			sinkBackend:  model.SinkBackendDB,
			source:       true,
			sink:         true,
			replayStrategies: []SinkReplayStrategy{
				SinkReplayStrategyNaturallyIdempotent,
				SinkReplayStrategyIdempotentWithKey,
				SinkReplayStrategyDeduplicatedByLedger,
				SinkReplayStrategyUnsafe,
			},
			incremental: []model.IncrementalCheckpointKind{model.IncrementalCheckpointKindWatermark},
		},
		{
			name:         "snowflake",
			dsType:       model.DataSourceTypeSnowflake,
			schemaFamily: SchemaFamilySQL,
			queryOpType:  OperationTypeSnowflake,
			sinkBackend:  model.SinkBackendDB,
			source:       true,
			sink:         true,
			replayStrategies: []SinkReplayStrategy{
				SinkReplayStrategyDeduplicatedByLedger,
				SinkReplayStrategyUnsafe,
			},
			incremental: []model.IncrementalCheckpointKind{model.IncrementalCheckpointKindWatermark},
		},
		{
			name:         "http",
			dsType:       model.DataSourceTypeHTTP,
			schemaFamily: SchemaFamilyHTTP,
			queryOpType:  OperationTypeHTTP,
			sinkBackend:  model.SinkBackendHTTP,
			source:       true,
			sink:         true,
			replayStrategies: []SinkReplayStrategy{
				SinkReplayStrategyIdempotentWithKey,
				SinkReplayStrategyDeduplicatedByLedger,
				SinkReplayStrategyUnsafe,
			},
			incremental: []model.IncrementalCheckpointKind{model.IncrementalCheckpointKindCursor, model.IncrementalCheckpointKindWatermark},
		},
		{
			name:         "mongodb",
			dsType:       model.DataSourceTypeMongoDB,
			schemaFamily: SchemaFamilyDocument,
			queryOpType:  OperationTypeMongoDB,
			sinkBackend:  model.SinkBackendMongo,
			source:       true,
			sink:         true,
			replayStrategies: []SinkReplayStrategy{
				SinkReplayStrategyNaturallyIdempotent,
				SinkReplayStrategyDeduplicatedByLedger,
				SinkReplayStrategyUnsafe,
			},
			incremental: []model.IncrementalCheckpointKind{model.IncrementalCheckpointKindWatermark},
		},
		{
			name:             "hubspot",
			dsType:           model.DataSourceTypeHubSpot,
			schemaFamily:     SchemaFamilyObjectAPI,
			queryOpType:      OperationTypeHubSpot,
			sinkBackend:      model.SinkBackendHTTP,
			source:           true,
			sink:             true,
			replayStrategies: []SinkReplayStrategy{SinkReplayStrategyIdempotentWithKey},
			incremental:      []model.IncrementalCheckpointKind{model.IncrementalCheckpointKindCursor},
			forcedReadOnly:   false,
		},
		{
			name:         "ftp",
			dsType:       model.DataSourceTypeFTP,
			schemaFamily: SchemaFamilyFile,
			queryOpType:  OperationTypeFTP,
			sinkBackend:  model.SinkBackendFTP,
			source:       true,
			sink:         true,
			replayStrategies: []SinkReplayStrategy{
				SinkReplayStrategyDeduplicatedByLedger,
				SinkReplayStrategyUnsafe,
			},
		},
		{
			name:         "git",
			dsType:       model.DataSourceTypeGit,
			schemaFamily: SchemaFamilyFile,
			queryOpType:  OperationTypeGit,
			sinkBackend:  model.SinkBackendGit,
			source:       true,
			sink:         true,
			replayStrategies: []SinkReplayStrategy{
				SinkReplayStrategyNaturallyIdempotent,
				SinkReplayStrategyDeduplicatedByLedger,
				SinkReplayStrategyUnsafe,
			},
		},
		{
			name:         "github",
			dsType:       model.DataSourceTypeGithub,
			schemaFamily: SchemaFamilyFile,
			queryOpType:  OperationTypeGit,
			sinkBackend:  model.SinkBackendGit,
			source:       true,
			sink:         true,
			replayStrategies: []SinkReplayStrategy{
				SinkReplayStrategyDeduplicatedByLedger,
				SinkReplayStrategyUnsafe,
			},
		},
		{
			name:         "gitlab",
			dsType:       model.DataSourceTypeGitlab,
			schemaFamily: SchemaFamilyFile,
			queryOpType:  OperationTypeGit,
			sinkBackend:  model.SinkBackendGit,
			source:       true,
			sink:         true,
			replayStrategies: []SinkReplayStrategy{
				SinkReplayStrategyDeduplicatedByLedger,
				SinkReplayStrategyUnsafe,
			},
		},
		{
			name:         "s3",
			dsType:       model.DataSourceTypeS3,
			schemaFamily: SchemaFamilyObjectStore,
			queryOpType:  OperationTypeS3,
			sinkBackend:  model.SinkBackendS3,
			source:       true,
			sink:         true,
			replayStrategies: []SinkReplayStrategy{
				SinkReplayStrategyNaturallyIdempotent,
				SinkReplayStrategyDeduplicatedByLedger,
				SinkReplayStrategyUnsafe,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			descriptor, ok := DescriptorFor(tt.dsType)

			require.True(t, ok)
			require.Equal(t, tt.dsType, descriptor.Type)
			require.Equal(t, tt.schemaFamily, descriptor.SchemaFamily)
			require.Equal(t, tt.queryOpType, descriptor.QueryOperationType)
			require.Equal(t, tt.sinkBackend, descriptor.SinkBackend)
			require.Equal(t, tt.source, descriptor.SupportsPipelineSource)
			require.Equal(t, tt.sink, descriptor.SupportsPipelineSink)
			require.ElementsMatch(t, tt.replayStrategies, descriptor.SupportedSinkReplayStrategies)
			require.Equal(t, tt.incremental, descriptor.IncrementalSourceCheckpointKinds)
			require.Equal(t, tt.forcedReadOnly, descriptor.ForcedReadOnly)
		})
	}
}

func TestSupportsSinkReplayStrategyUsesConnectorContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		dsType   model.DataSourceType
		strategy SinkReplayStrategy
		want     bool
	}{
		{name: "S3 rejects provider key", dsType: model.DataSourceTypeS3, strategy: SinkReplayStrategyIdempotentWithKey, want: false},
		{name: "S3 accepts deterministic overwrite", dsType: model.DataSourceTypeS3, strategy: SinkReplayStrategyNaturallyIdempotent, want: true},
		{name: "HTTP accepts provider key", dsType: model.DataSourceTypeHTTP, strategy: SinkReplayStrategyIdempotentWithKey, want: true},
		{name: "unknown connector rejected", dsType: model.DataSourceTypeUnknown, strategy: SinkReplayStrategyUnsafe, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, SupportsSinkReplayStrategy(tc.dsType, tc.strategy))
		})
	}
}

func TestSinkCapabilitiesUseConnectorCatalog(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		SinkPayloadBindingRowObject,
		SinkPayloadBindingForOperationType(OperationTypeBigQuery),
	)
	require.Equal(
		t,
		SinkPayloadBindingRowObject,
		SinkPayloadBindingForOperationType(OperationTypeHubSpot),
	)
	require.Equal(
		t,
		SinkPayloadBindingEncoded,
		SinkPayloadBindingForOperationType(OperationTypeSQL),
	)
	require.Equal(t, SinkResultCapability{
		Kind:      SinkResultKindObjectRows,
		Operation: "upsert",
	}, SinkResultForOperationType(OperationTypeHubSpot))
	require.Equal(t, SinkResultCapability{
		Kind: SinkResultKindHTTP,
	}, SinkResultForOperationType(OperationTypeHTTP))
}

func TestAllDescriptorsCoversEverySupportedDataSourceType(t *testing.T) {
	t.Parallel()

	descriptors := AllDescriptors()
	seen := make(map[model.DataSourceType]struct{}, len(descriptors))
	for _, descriptor := range descriptors {
		require.NotEqual(t, model.DataSourceTypeUnknown, descriptor.Type)
		require.NotEmpty(t, descriptor.QueryOperationType)
		seen[descriptor.Type] = struct{}{}
	}

	for _, dsType := range model.DataSourceTypeValues() {
		if dsType == model.DataSourceTypeUnknown {
			continue
		}
		_, ok := seen[dsType]
		require.Truef(t, ok, "missing catalog descriptor for %s", dsType)
	}
}

func TestSupportsIncrementalSourceCheckpointKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		operationType string
		kind          model.IncrementalCheckpointKind
		want          bool
	}{
		{
			name:          "SQLWatermark",
			operationType: OperationTypeSQL,
			kind:          model.IncrementalCheckpointKindWatermark,
			want:          true,
		},
		{
			name:          "HTTPCursor",
			operationType: OperationTypeHTTP,
			kind:          model.IncrementalCheckpointKindCursor,
			want:          true,
		},
		{
			name:          "BigQueryWatermark",
			operationType: OperationTypeBigQuery,
			kind:          model.IncrementalCheckpointKindWatermark,
			want:          true,
		},
		{
			name:          "BigQueryCursorUnsupported",
			operationType: OperationTypeBigQuery,
			kind:          model.IncrementalCheckpointKindCursor,
			want:          false,
		},
		{
			name:          "HubSpotCursor",
			operationType: OperationTypeHubSpot,
			kind:          model.IncrementalCheckpointKindCursor,
			want:          true,
		},
		{
			name:          "FTPSupportsNoWatermark",
			operationType: OperationTypeFTP,
			kind:          model.IncrementalCheckpointKindWatermark,
			want:          false,
		},
		{
			name:          "FTPSupportsNoCursor",
			operationType: OperationTypeFTP,
			kind:          model.IncrementalCheckpointKindCursor,
			want:          false,
		},
		{
			name:          "GitSupportsNoWatermark",
			operationType: OperationTypeGit,
			kind:          model.IncrementalCheckpointKindWatermark,
			want:          false,
		},
		{
			name:          "GitSupportsNoCursor",
			operationType: OperationTypeGit,
			kind:          model.IncrementalCheckpointKindCursor,
			want:          false,
		},
		{
			name:          "S3SupportsNoWatermark",
			operationType: OperationTypeS3,
			kind:          model.IncrementalCheckpointKindWatermark,
			want:          false,
		},
		{
			name:          "S3SupportsNoCursor",
			operationType: OperationTypeS3,
			kind:          model.IncrementalCheckpointKindCursor,
			want:          false,
		},
		{
			name:          "HTTPWatermarkSupported",
			operationType: OperationTypeHTTP,
			kind:          model.IncrementalCheckpointKindWatermark,
			want:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := SupportsIncrementalSourceCheckpointKind(tt.operationType, tt.kind)

			require.Equal(t, tt.want, got)
		})
	}
}

func TestEveryConnectorDeclaresFreshProofMetadataScope(t *testing.T) {
	for _, descriptor := range AllDescriptors() {
		supported := len(descriptor.ProofMetadata.Operations) > 0
		explained := descriptor.ProofMetadata.UnsupportedReason != ""
		require.NotEqual(t, supported, explained, descriptor.Type.String())
	}
}
