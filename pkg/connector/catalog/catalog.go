package catalog

import "github.com/Braidplane/cli/pkg/model"

type SchemaFamily string

type SinkPayloadBinding string

const (
	SinkPayloadBindingEncoded   SinkPayloadBinding = "encoded"
	SinkPayloadBindingRowObject SinkPayloadBinding = "row_object"
)

type SinkResultKind string

const (
	SinkResultKindHTTP       SinkResultKind = "http"
	SinkResultKindObjectRows SinkResultKind = "object_rows"
)

type SinkResultCapability struct {
	Kind      SinkResultKind
	Operation string
}

// SinkReplayStrategy is the connector-level replay mechanism a sink operation
// may declare. Individual saved operations still select one strategy in the
// pipeline spec; this catalog list prevents UI and documentation from implying
// provider support that the adapter does not have.
//
//go:generate -command enumer go tool enumer
//go:generate enumer -type=SinkReplayStrategy -json -text -linecomment -output=sink_replay_strategy_enumer.go
type SinkReplayStrategy int

const (
	SinkReplayStrategyUnknown              SinkReplayStrategy = iota // unknown
	SinkReplayStrategyNaturallyIdempotent                            // naturally_idempotent
	SinkReplayStrategyIdempotentWithKey                              // idempotent_with_key
	SinkReplayStrategyDeduplicatedByLedger                           // deduplicated_by_ledger
	SinkReplayStrategyUnsafe                                         // unsafe
)

const (
	SchemaFamilyUnknown     SchemaFamily = "unknown"
	SchemaFamilySQL         SchemaFamily = "sql"
	SchemaFamilyHTTP        SchemaFamily = "http"
	SchemaFamilyDocument    SchemaFamily = "document"
	SchemaFamilyFile        SchemaFamily = "file"
	SchemaFamilyObjectAPI   SchemaFamily = "object_api"
	SchemaFamilyObjectStore SchemaFamily = "object_store"
)

const (
	OperationTypeSQL       = "sql"
	OperationTypeBigQuery  = "bigquery"
	OperationTypeHTTP      = "http"
	OperationTypeFTP       = "ftp"
	OperationTypeGit       = "git"
	OperationTypeMongoDB   = "mongodb"
	OperationTypeHubSpot   = "hubspot"
	OperationTypeS3        = "s3"
	OperationTypeSnowflake = "snowflake"
)

type Descriptor struct {
	Type                             model.DataSourceType
	SchemaFamily                     SchemaFamily
	QueryOperationType               string
	SinkBackend                      model.SinkBackend
	SupportsPipelineSource           bool
	SupportsPipelineSink             bool
	SupportedSinkReplayStrategies    []SinkReplayStrategy
	SinkPayloadBinding               SinkPayloadBinding
	SinkResult                       SinkResultCapability
	IncrementalSourceCheckpointKinds []model.IncrementalCheckpointKind
	ForcedReadOnly                   bool
}

var descriptors = []Descriptor{
	{
		Type:                   model.DataSourceTypePostgres,
		SchemaFamily:           SchemaFamilySQL,
		QueryOperationType:     OperationTypeSQL,
		SinkBackend:            model.SinkBackendDB,
		SupportsPipelineSource: true,
		SupportsPipelineSink:   true,
		SupportedSinkReplayStrategies: []SinkReplayStrategy{
			SinkReplayStrategyNaturallyIdempotent,
			SinkReplayStrategyDeduplicatedByLedger,
			SinkReplayStrategyUnsafe,
		},
		IncrementalSourceCheckpointKinds: []model.IncrementalCheckpointKind{
			model.IncrementalCheckpointKindWatermark,
		},
	},
	{
		Type:                   model.DataSourceTypeMySQL,
		SchemaFamily:           SchemaFamilySQL,
		QueryOperationType:     OperationTypeSQL,
		SinkBackend:            model.SinkBackendDB,
		SupportsPipelineSource: true,
		SupportsPipelineSink:   true,
		SupportedSinkReplayStrategies: []SinkReplayStrategy{
			SinkReplayStrategyNaturallyIdempotent,
			SinkReplayStrategyDeduplicatedByLedger,
			SinkReplayStrategyUnsafe,
		},
		IncrementalSourceCheckpointKinds: []model.IncrementalCheckpointKind{
			model.IncrementalCheckpointKindWatermark,
		},
	},
	{
		Type:                   model.DataSourceTypeBigQuery,
		SchemaFamily:           SchemaFamilySQL,
		QueryOperationType:     OperationTypeBigQuery,
		SinkBackend:            model.SinkBackendDB,
		SupportsPipelineSource: true,
		SupportsPipelineSink:   true,
		SupportedSinkReplayStrategies: []SinkReplayStrategy{
			SinkReplayStrategyNaturallyIdempotent,
			SinkReplayStrategyIdempotentWithKey,
			SinkReplayStrategyDeduplicatedByLedger,
			SinkReplayStrategyUnsafe,
		},
		SinkPayloadBinding: SinkPayloadBindingRowObject,
		IncrementalSourceCheckpointKinds: []model.IncrementalCheckpointKind{
			model.IncrementalCheckpointKindWatermark,
		},
	},
	{
		Type:                   model.DataSourceTypeSnowflake,
		SchemaFamily:           SchemaFamilySQL,
		QueryOperationType:     OperationTypeSnowflake,
		SinkBackend:            model.SinkBackendDB,
		SupportsPipelineSource: true,
		SupportsPipelineSink:   true,
		SupportedSinkReplayStrategies: []SinkReplayStrategy{
			SinkReplayStrategyDeduplicatedByLedger,
			SinkReplayStrategyUnsafe,
		},
		IncrementalSourceCheckpointKinds: []model.IncrementalCheckpointKind{
			model.IncrementalCheckpointKindWatermark,
		},
	},
	{
		Type:                   model.DataSourceTypeHTTP,
		SchemaFamily:           SchemaFamilyHTTP,
		QueryOperationType:     OperationTypeHTTP,
		SinkBackend:            model.SinkBackendHTTP,
		SupportsPipelineSource: true,
		SupportsPipelineSink:   true,
		SupportedSinkReplayStrategies: []SinkReplayStrategy{
			SinkReplayStrategyIdempotentWithKey,
			SinkReplayStrategyDeduplicatedByLedger,
			SinkReplayStrategyUnsafe,
		},
		IncrementalSourceCheckpointKinds: []model.IncrementalCheckpointKind{
			model.IncrementalCheckpointKindCursor,
		},
	},
	{
		Type:                   model.DataSourceTypeMongoDB,
		SchemaFamily:           SchemaFamilyDocument,
		QueryOperationType:     OperationTypeMongoDB,
		SinkBackend:            model.SinkBackendMongo,
		SupportsPipelineSource: true,
		SupportsPipelineSink:   true,
		SupportedSinkReplayStrategies: []SinkReplayStrategy{
			SinkReplayStrategyNaturallyIdempotent,
			SinkReplayStrategyDeduplicatedByLedger,
			SinkReplayStrategyUnsafe,
		},
		IncrementalSourceCheckpointKinds: []model.IncrementalCheckpointKind{
			model.IncrementalCheckpointKindWatermark,
		},
	},
	{
		Type:                   model.DataSourceTypeHubSpot,
		SchemaFamily:           SchemaFamilyObjectAPI,
		QueryOperationType:     OperationTypeHubSpot,
		SinkBackend:            model.SinkBackendHTTP,
		SupportsPipelineSource: true,
		SupportsPipelineSink:   true,
		SupportedSinkReplayStrategies: []SinkReplayStrategy{
			SinkReplayStrategyIdempotentWithKey,
		},
		SinkPayloadBinding: SinkPayloadBindingRowObject,
		SinkResult: SinkResultCapability{
			Kind:      SinkResultKindObjectRows,
			Operation: "upsert",
		},
		ForcedReadOnly: false,
		IncrementalSourceCheckpointKinds: []model.IncrementalCheckpointKind{
			model.IncrementalCheckpointKindCursor,
		},
	},
	{
		Type:                   model.DataSourceTypeFTP,
		SchemaFamily:           SchemaFamilyFile,
		QueryOperationType:     OperationTypeFTP,
		SinkBackend:            model.SinkBackendFTP,
		SupportsPipelineSource: true,
		SupportsPipelineSink:   true,
		SupportedSinkReplayStrategies: []SinkReplayStrategy{
			SinkReplayStrategyDeduplicatedByLedger,
			SinkReplayStrategyUnsafe,
		},
	},
	{
		Type:                   model.DataSourceTypeGit,
		SchemaFamily:           SchemaFamilyFile,
		QueryOperationType:     OperationTypeGit,
		SinkBackend:            model.SinkBackendGit,
		SupportsPipelineSource: true,
		SupportsPipelineSink:   true,
		SupportedSinkReplayStrategies: []SinkReplayStrategy{
			SinkReplayStrategyNaturallyIdempotent,
			SinkReplayStrategyDeduplicatedByLedger,
			SinkReplayStrategyUnsafe,
		},
	},
	{
		Type:                   model.DataSourceTypeGithub,
		SchemaFamily:           SchemaFamilyFile,
		QueryOperationType:     OperationTypeGit,
		SinkBackend:            model.SinkBackendGit,
		SupportsPipelineSource: true,
		SupportsPipelineSink:   true,
		SupportedSinkReplayStrategies: []SinkReplayStrategy{
			SinkReplayStrategyDeduplicatedByLedger,
			SinkReplayStrategyUnsafe,
		},
	},
	{
		Type:                   model.DataSourceTypeGitlab,
		SchemaFamily:           SchemaFamilyFile,
		QueryOperationType:     OperationTypeGit,
		SinkBackend:            model.SinkBackendGit,
		SupportsPipelineSource: true,
		SupportsPipelineSink:   true,
		SupportedSinkReplayStrategies: []SinkReplayStrategy{
			SinkReplayStrategyDeduplicatedByLedger,
			SinkReplayStrategyUnsafe,
		},
	},
	{
		Type:                   model.DataSourceTypeS3,
		SchemaFamily:           SchemaFamilyObjectStore,
		QueryOperationType:     OperationTypeS3,
		SinkBackend:            model.SinkBackendS3,
		SupportsPipelineSource: true,
		SupportsPipelineSink:   true,
		SupportedSinkReplayStrategies: []SinkReplayStrategy{
			SinkReplayStrategyNaturallyIdempotent,
			SinkReplayStrategyDeduplicatedByLedger,
			SinkReplayStrategyUnsafe,
		},
	},
}

func AllDescriptors() []Descriptor {
	return append([]Descriptor(nil), descriptors...)
}

func DescriptorFor(dsType model.DataSourceType) (Descriptor, bool) {
	for _, descriptor := range descriptors {
		if descriptor.Type == dsType {
			return descriptor, true
		}
	}

	return Descriptor{}, false
}

// SupportsSinkReplayStrategy reports whether a sink connector permits the
// declared replay mechanism. Unknown and source-only connectors are rejected
// so callers cannot dispatch a side effect with an unverified contract.
func SupportsSinkReplayStrategy(dsType model.DataSourceType, strategy SinkReplayStrategy) bool {
	descriptor, found := DescriptorFor(dsType)
	if !found || !descriptor.SupportsPipelineSink {
		return false
	}

	for _, supported := range descriptor.SupportedSinkReplayStrategies {
		if supported == strategy {
			return true
		}
	}

	return false
}

func IsSupportedQueryOperationType(operationType string) bool {
	for _, descriptor := range descriptors {
		if descriptor.QueryOperationType == operationType {
			return true
		}
	}

	return false
}

func SinkBackendForOperationType(operationType string) (model.SinkBackend, bool) {
	for _, descriptor := range descriptors {
		if descriptor.QueryOperationType == operationType &&
			descriptor.SupportsPipelineSink &&
			descriptor.SinkBackend != model.SinkBackendUnknown {
			return descriptor.SinkBackend, true
		}
	}

	return model.SinkBackendUnknown, false
}

// SinkPayloadBindingForOperationType returns the public input shape expected by
// a sink connector. Connectors default to the encoded binding unless their
// catalog descriptor explicitly declares row-object input.
func SinkPayloadBindingForOperationType(operationType string) SinkPayloadBinding {
	for _, descriptor := range descriptors {
		if descriptor.QueryOperationType == operationType &&
			descriptor.SinkPayloadBinding != "" {
			return descriptor.SinkPayloadBinding
		}
	}

	return SinkPayloadBindingEncoded
}

// SinkResultForOperationType returns the result contract expected after sink
// dispatch. HTTP is the default for connectors without an explicit result shape.
func SinkResultForOperationType(operationType string) SinkResultCapability {
	for _, descriptor := range descriptors {
		if descriptor.QueryOperationType == operationType && descriptor.SinkResult.Kind != "" {
			return descriptor.SinkResult
		}
	}

	return SinkResultCapability{Kind: SinkResultKindHTTP}
}

func SupportsIncrementalSourceCheckpointKind(
	operationType string,
	kind model.IncrementalCheckpointKind,
) bool {
	for _, descriptor := range descriptors {
		if descriptor.QueryOperationType != operationType || !descriptor.SupportsPipelineSource {
			continue
		}
		for _, supportedKind := range descriptor.IncrementalSourceCheckpointKinds {
			if supportedKind == kind {
				return true
			}
		}
	}

	return false
}
