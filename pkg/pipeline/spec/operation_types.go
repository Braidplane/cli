package spec

import (
	"github.com/Braidplane/cli/pkg/connector/catalog"
	"github.com/Braidplane/cli/pkg/model"
)

const (
	OperationTypeSQL       = catalog.OperationTypeSQL
	OperationTypeBigQuery  = catalog.OperationTypeBigQuery
	OperationTypeSnowflake = catalog.OperationTypeSnowflake
	OperationTypeHTTP      = catalog.OperationTypeHTTP
	OperationTypeFTP       = catalog.OperationTypeFTP
	OperationTypeGit       = catalog.OperationTypeGit
	OperationTypeMongoDB   = catalog.OperationTypeMongoDB
	OperationTypeHubSpot   = catalog.OperationTypeHubSpot
	OperationTypeS3        = catalog.OperationTypeS3
)

// ConnectorCapability is the pipeline-facing subset of connector catalog facts.
// It lets same-repository frontend and builder packages consume connector
// behavior without importing the connector implementation boundary directly.
// If the frontend becomes a separately versioned artifact or repository, this
// descriptor contract belongs in OpenAPI or generated API metadata instead.
type ConnectorCapability struct {
	DataSourceType                   model.DataSourceType
	SchemaFamily                     string
	QueryOperationType               string
	SinkBackend                      model.SinkBackend
	SupportsPipelineSource           bool
	SupportsPipelineSink             bool
	SupportedSinkReplayStrategies    []SinkIdempotencyClass
	IncrementalSourceCheckpointKinds []model.IncrementalCheckpointKind
	ForcedReadOnly                   bool
}

// AllConnectorCapabilities returns catalog connector facts in a pipeline-owned shape.
// Slice fields are copied so callers can sort or filter without mutating catalog state.
func AllConnectorCapabilities() []ConnectorCapability {
	descriptors := catalog.AllDescriptors()
	capabilities := make([]ConnectorCapability, 0, len(descriptors))
	for _, descriptor := range descriptors {
		incrementalKinds := append(
			[]model.IncrementalCheckpointKind(nil),
			descriptor.IncrementalSourceCheckpointKinds...,
		)
		replayStrategies := sinkIdempotencyClasses(descriptor.SupportedSinkReplayStrategies)
		capabilities = append(capabilities, ConnectorCapability{
			DataSourceType:                   descriptor.Type,
			SchemaFamily:                     string(descriptor.SchemaFamily),
			QueryOperationType:               descriptor.QueryOperationType,
			SinkBackend:                      descriptor.SinkBackend,
			SupportsPipelineSource:           descriptor.SupportsPipelineSource,
			SupportsPipelineSink:             descriptor.SupportsPipelineSink,
			SupportedSinkReplayStrategies:    replayStrategies,
			IncrementalSourceCheckpointKinds: incrementalKinds,
			ForcedReadOnly:                   descriptor.ForcedReadOnly,
		})
	}

	return capabilities
}

// sinkIdempotencyClasses translates catalog-owned mechanisms into the neutral
// pipeline contract exposed to builders and frontend-facing callers.
func sinkIdempotencyClasses(strategies []catalog.SinkReplayStrategy) []SinkIdempotencyClass {
	classes := make([]SinkIdempotencyClass, 0, len(strategies))
	for _, strategy := range strategies {
		class, err := SinkIdempotencyClassString(strategy.String())
		if err == nil && class != SinkIdempotencyClassUnknown {
			classes = append(classes, class)
		}
	}
	return classes
}

// IsSupportedQueryOperationType reports whether the catalog knows the operation type.
func IsSupportedQueryOperationType(operationType string) bool {
	return catalog.IsSupportedQueryOperationType(operationType)
}

// IsSupportedSinkOperationType reports whether the operation type can write to a sink.
func IsSupportedSinkOperationType(operationType string) bool {
	_, supported := SinkBackendForOperationType(operationType)
	return supported
}

// SinkBackendForOperationType returns the sink backend attached to an operation type.
func SinkBackendForOperationType(operationType string) (model.SinkBackend, bool) {
	return catalog.SinkBackendForOperationType(operationType)
}

// SupportsSinkIdempotencyClass reports whether the concrete sink connector
// supports the replay mechanism declared by a pipeline sink step.
func SupportsSinkIdempotencyClass(
	dataSourceType model.DataSourceType,
	idempotencyClass SinkIdempotencyClass,
) bool {
	strategy, err := catalog.SinkReplayStrategyString(idempotencyClass.String())
	if err != nil {
		return false
	}

	return catalog.SupportsSinkReplayStrategy(
		dataSourceType,
		strategy,
	)
}

// SupportsIncrementalSourceCheckpointKind parses a checkpoint kind before capability lookup.
func SupportsIncrementalSourceCheckpointKind(operationType, checkpointKind string) bool {
	kind, err := model.IncrementalCheckpointKindString(checkpointKind)
	if err != nil {
		return false
	}

	return SupportsIncrementalSourceCheckpoint(operationType, kind)
}

// SupportsIncrementalSourceCheckpoint reports whether an operation type accepts the checkpoint kind.
func SupportsIncrementalSourceCheckpoint(
	operationType string,
	checkpointKind model.IncrementalCheckpointKind,
) bool {
	return catalog.SupportsIncrementalSourceCheckpointKind(
		operationType,
		checkpointKind,
	)
}
