package xerror

// ---------------------------------------------------------------------------
// Entity-based template constructors
// ---------------------------------------------------------------------------

// NotFound creates a KindNotFound error for the given entity.
func NotFound(entity Entity) *Error {
	return newFromEntity(KindNotFound, entity)
}

// AlreadyExists creates a KindAlreadyExists error for the given entity.
// Use fine-grained entities for conflict specificity (e.g. UserEmail).
func AlreadyExists(entity Entity) *Error {
	return newFromEntity(KindAlreadyExists, entity)
}

// InvalidInput creates a KindInvalidInput error for the given entity.
func InvalidInput(entity Entity) *Error {
	return newFromEntity(KindInvalidInput, entity)
}

// InvalidRequired creates a KindInvalidInput error for a missing required value.
func InvalidRequired(entity Entity) *Error {
	return newStandalone(
		KindInvalidInput,
		entity.code+"_REQUIRED",
		entity.label+" is required",
	)
}

// UnexpectedRequired creates a KindInternal error for a missing internal dependency.
func UnexpectedRequired(entity Entity) *Error {
	return newStandalone(
		KindInternal,
		entity.code+"_REQUIRED",
		entity.label+" is required",
	)
}

// ConnectionFailed creates a KindConnectionFailed error for the given entity.
func ConnectionFailed(entity Entity) *Error {
	return newFromEntity(KindConnectionFailed, entity)
}

// Timeout creates a KindTimeout error for the given entity.
func Timeout(entity Entity) *Error {
	return newFromEntity(KindTimeout, entity)
}

// Unavailable creates a KindUnavailable error for the given entity.
func Unavailable(entity Entity) *Error {
	return newFromEntity(KindUnavailable, entity)
}

// Unsupported creates a KindUnsupported error for the given entity.
func Unsupported(entity Entity) *Error {
	return newFromEntity(KindUnsupported, entity)
}

// Forbidden creates a KindForbidden error for the given entity.
func Forbidden(entity Entity) *Error {
	return newFromEntity(KindForbidden, entity)
}

// ---------------------------------------------------------------------------
// Standalone template constructors (no entity)
// ---------------------------------------------------------------------------

// Unexpected creates a generic KindInternal error.
func Unexpected() *Error {
	return newStandalone(KindInternal, "INTERNAL_ERROR", "internal error")
}

// ValidationFailed creates a generic KindInvalidInput error.
func ValidationFailed() *Error {
	return newStandalone(KindInvalidInput, "VALIDATION_FAILED", "validation failed")
}

// InvalidCredentials creates a KindUnauthorized error for bad login attempts.
func InvalidCredentials() *Error {
	return newStandalone(KindUnauthorized, "INVALID_CREDENTIALS", "invalid credentials")
}

// EmailNotVerified creates a KindUnauthorized error for unverified accounts.
func EmailNotVerified() *Error {
	return newStandalone(KindUnauthorized, "EMAIL_NOT_VERIFIED", "email not verified")
}

// AlreadyVerified creates a KindInvalidInput error for re-verification attempts.
func AlreadyVerified() *Error {
	return newStandalone(KindInvalidInput, "ALREADY_VERIFIED", "already verified")
}

// InvalidToken creates a KindInvalidInput error for token validation failures.
func InvalidToken() *Error {
	return newStandalone(KindInvalidInput, "INVALID_TOKEN", "invalid token")
}

// CannotRemoveOwner creates a KindInvalidInput error when attempting to remove
// the organization owner.
func CannotRemoveOwner() *Error {
	return newStandalone(KindInvalidInput, "CANNOT_REMOVE_OWNER", "cannot remove owner")
}

// CannotDemoteOwner creates a KindInvalidInput error when attempting to change
// the organization owner's role.
func CannotDemoteOwner() *Error {
	return newStandalone(KindInvalidInput, "CANNOT_DEMOTE_OWNER", "cannot demote owner")
}

// CannotDeleteDefault creates a KindInvalidInput error when attempting to
// delete a default/system entity (e.g. a default role).
func CannotDeleteDefault(entity Entity) *Error {
	code := entity.code + "_CANNOT_DELETE_DEFAULT"
	message := "cannot delete default " + entity.label
	return newStandalone(KindInvalidInput, code, message)
}

// CannotUpdateDefault creates a KindInvalidInput error when attempting to
// update a default/system entity (e.g. a default role).
func CannotUpdateDefault(entity Entity) *Error {
	code := entity.code + "_CANNOT_UPDATE_DEFAULT"
	message := "cannot update default " + entity.label
	return newStandalone(KindInvalidInput, code, message)
}

// ParameterValidationFailed creates a KindInvalidInput error for query
// parameter validation failures.
func ParameterValidationFailed() *Error {
	return newStandalone(
		KindInvalidInput,
		"PARAMETER_VALIDATION_FAILED",
		"parameter validation failed",
	)
}

// InvalidSchemaURL creates a KindInvalidInput error for invalid schema URLs.
func InvalidSchemaURL() *Error {
	return newStandalone(KindInvalidInput, "INVALID_SCHEMA_URL", "invalid schema url")
}

// SchemaFetchFailed creates a KindUnavailable error when a schema cannot be
// retrieved.
func SchemaFetchFailed() *Error {
	return newStandalone(KindUnavailable, "SCHEMA_FETCH_FAILED", "schema fetch failed")
}

// InvalidOpenAPI creates a KindInvalidInput error for invalid OpenAPI specs.
func InvalidOpenAPI() *Error {
	return newStandalone(KindInvalidInput, "INVALID_OPENAPI", "invalid openapi")
}

// DataSourceSchemaIntrospectionUnsupported creates a KindUnsupported error when a
// datasource connector does not support schema introspection.
func DataSourceSchemaIntrospectionUnsupported() *Error {
	return newStandalone(
		KindUnsupported,
		"DATA_SOURCE_SCHEMA_INTROSPECTION_UNSUPPORTED",
		"data source schema introspection unsupported",
	)
}

// InvalidSlugFormat creates a KindInvalidInput error for invalid slug syntax.
func InvalidSlugFormat() *Error {
	return newStandalone(
		KindInvalidInput,
		"SLUG_INVALID_FORMAT",
		"invalid slug format: must be 1-100 lowercase alphanumeric characters with hyphens",
	)
}

// BuilderStateInvalid creates a KindInvalidInput error for invalid builder state payloads.
func BuilderStateInvalid() *Error {
	return newStandalone(KindInvalidInput, "BUILDER_STATE_INVALID", "builder state is invalid")
}

// UnsupportedURIScheme creates a KindUnsupported error for unsupported datasource URI schemes.
func UnsupportedURIScheme() *Error {
	return newStandalone(KindUnsupported, "UNSUPPORTED_URI_SCHEME", "unsupported URI scheme")
}

// ReplayRunNotFound creates a KindNotFound error for missing replay source runs.
func ReplayRunNotFound() *Error {
	return newStandalone(
		KindNotFound,
		"PIPELINE_REPLAY_RUN_NOT_FOUND",
		"replay source run not found",
	)
}

// ReplayStepNotFound creates a KindNotFound error for missing replay checkpoint steps.
func ReplayStepNotFound() *Error {
	return newStandalone(
		KindNotFound,
		"PIPELINE_REPLAY_STEP_NOT_FOUND",
		"replay step not found in checkpoint",
	)
}

// ReplayUnsupportedMode creates a KindUnsupported error for unsupported replay modes.
func ReplayUnsupportedMode() *Error {
	return newStandalone(
		KindUnsupported,
		"PIPELINE_REPLAY_UNSUPPORTED_MODE",
		"unsupported replay input mode",
	)
}

// ReplayUnsafe creates a KindInvalidInput error when automatic replay would
// repeat a non-replay-safe side effect.
func ReplayUnsafe() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_REPLAY_UNSAFE",
		"pipeline replay is unsafe",
	)
}

// TransformUpstreamRequired creates a KindInvalidInput error when a transform has no upstream
// result.
func TransformUpstreamRequired() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_TRANSFORM_UPSTREAM_REQUIRED",
		"transform step requires an upstream result",
	)
}

// SinkUpstreamRequired creates a KindInvalidInput error when a sink has no upstream result.
func SinkUpstreamRequired() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_SINK_UPSTREAM_REQUIRED",
		"sink step requires an upstream result",
	)
}

// TransformBindingMissing creates a KindInvalidInput error for missing transform input bindings.
func TransformBindingMissing() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_TRANSFORM_BINDING_MISSING",
		"transform step rows input binding not found",
	)
}

// TransformBindingSourceNotFound creates a KindInvalidInput error for unresolved transform binding
// sources.
func TransformBindingSourceNotFound() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_TRANSFORM_BINDING_SOURCE_NOT_FOUND",
		"transform binding source node does not map to any executed step",
	)
}

// TransformBindingSourceAmbiguous creates a KindInvalidInput error for ambiguous transform binding
// sources.
func TransformBindingSourceAmbiguous() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_TRANSFORM_BINDING_SOURCE_AMBIGUOUS",
		"transform binding source node maps to multiple executed steps",
	)
}

// TransformBindingNodeUnsupported creates a KindUnsupported error for unsupported transform binding
// node kinds.
func TransformBindingNodeUnsupported() *Error {
	return newStandalone(
		KindUnsupported,
		"PIPELINE_TRANSFORM_BINDING_NODE_UNSUPPORTED",
		"transform binding source node type is unsupported",
	)
}

// QueryOutputContractSchemaMismatch creates a KindInvalidInput error for mismatched cached query
// schemas.
func QueryOutputContractSchemaMismatch() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_QUERY_OUTPUT_CONTRACT_SCHEMA_MISMATCH",
		"query output contract does not match cached schema",
	)
}

// SinkSourceResultNotFound creates a KindNotFound error for missing sink source results.
func SinkSourceResultNotFound() *Error {
	return newStandalone(
		KindNotFound,
		"PIPELINE_SINK_SOURCE_RESULT_NOT_FOUND",
		"sink source step result not found",
	)
}

// StepResultExecutionRequired creates a KindInternal error for missing step result execution data.
func StepResultExecutionRequired() *Error {
	return newStandalone(
		KindInternal,
		"PIPELINE_STEP_RESULT_EXECUTION_REQUIRED",
		"step result execution is required",
	)
}

// NoPipelineSpecsFound creates a KindNotFound error when a repository scan finds no specs.
func NoPipelineSpecsFound() *Error {
	return newStandalone(KindNotFound, "PIPELINE_SPECS_NOT_FOUND", "no pipeline specs found")
}

// PipelineMissingField creates a KindInvalidInput error for missing required pipeline fields.
func PipelineMissingField() *Error {
	return newStandalone(KindInvalidInput, "PIPELINE_MISSING_FIELD", "missing required field")
}

// UnsupportedPipelineFormat creates a KindUnsupported error for unsupported pipeline file formats.
func UnsupportedPipelineFormat() *Error {
	return newStandalone(
		KindUnsupported,
		"PIPELINE_UNSUPPORTED_FORMAT",
		"unsupported pipeline file format",
	)
}

// OrphanStep creates a KindInvalidInput error for disconnected pipeline steps.
func OrphanStep() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_ORPHAN_STEP",
		"pipeline steps must form one connected data flow",
	)
}

// PipelineContractMismatch creates a KindInvalidInput error for incompatible binding contracts.
func PipelineContractMismatch() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_CONTRACT_MISMATCH",
		"pipeline binding contract mismatch",
	)
}

// InvalidFieldPath creates a KindInvalidInput error for invalid pipeline field paths.
func InvalidFieldPath() *Error {
	return newStandalone(KindInvalidInput, "PIPELINE_INVALID_FIELD_PATH", "invalid field path")
}

// TransformOutputContractViolation creates a KindInvalidInput error for runtime output contract
// violations.
func TransformOutputContractViolation() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_TRANSFORM_OUTPUT_CONTRACT_VIOLATION",
		"transform output contract violation",
	)
}

// TransformInputContractViolation creates a KindInvalidInput error when an upstream result does
// not satisfy the transform input contract.
func TransformInputContractViolation() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_TRANSFORM_INPUT_CONTRACT_VIOLATION",
		"transform input contract violation",
	)
}

// SinkInputContractViolation creates a KindInvalidInput error when an upstream result does
// not satisfy the sink step input contract.
func SinkInputContractViolation() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_SINK_INPUT_CONTRACT_VIOLATION",
		"sink input contract violation",
	)
}

// UnsupportedHTTPResponseFormat creates a KindUnsupported error when an HTTP response body cannot
// be interpreted as a tabular input for the transform layer.
func UnsupportedHTTPResponseFormat() *Error {
	return newStandalone(
		KindUnsupported,
		"PIPELINE_TRANSFORM_HTTP_RESPONSE_FORMAT_UNSUPPORTED",
		"HTTP response format unsupported: expected JSON array or object",
	)
}

// UnsupportedHTTPContentType creates a KindUnsupported error when the HTTP response
// Content-Type is not a format the transform layer can consume.
// Callers should attach the specific content type via .Detailf(...).
func UnsupportedHTTPContentType() *Error {
	return newStandalone(
		KindUnsupported,
		"PIPELINE_TRANSFORM_HTTP_CONTENT_TYPE_UNSUPPORTED",
		"unsupported HTTP Content-Type",
	)
}

// RefUnknownStep creates a KindInvalidInput error for references to unknown steps.
func RefUnknownStep() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_REF_UNKNOWN_STEP",
		"step output reference points to unknown step",
	)
}

// RefUnknownField creates a KindInvalidInput error for references to unknown fields.
func RefUnknownField() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_REF_UNKNOWN_FIELD",
		"step output reference uses unknown field",
	)
}

// RefStepNoResult creates a KindInvalidInput error for references to steps with no result.
func RefStepNoResult() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_REF_STEP_NO_RESULT",
		"step output reference points to step with no result",
	)
}

// TransformSQLRequired creates a KindInvalidInput error for missing transform SQL.
func TransformSQLRequired() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_TRANSFORM_SQL_REQUIRED",
		"transform sql is required",
	)
}

// TransformSQLNotSelect creates a KindInvalidInput error for non-SELECT transform SQL.
func TransformSQLNotSelect() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_TRANSFORM_SQL_NOT_SELECT",
		"transform sql must start with SELECT",
	)
}

// TransformSQLMultiStatement creates a KindInvalidInput error for multiple transform statements.
func TransformSQLMultiStatement() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_TRANSFORM_SQL_MULTI_STATEMENT",
		"transform sql must be a single statement",
	)
}

// TransformSQLFeatureNotAllowed creates a KindUnsupported error for disallowed transform SQL
// features.
func TransformSQLFeatureNotAllowed() *Error {
	return newStandalone(
		KindUnsupported,
		"PIPELINE_TRANSFORM_SQL_FEATURE_NOT_ALLOWED",
		"transform sql contains a disallowed feature",
	)
}

// TransformContractInvalid creates a KindInvalidInput error for invalid transform output contracts.
func TransformContractInvalid() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_TRANSFORM_CONTRACT_INVALID",
		"transform output contract is invalid",
	)
}

// TransformContractTypeMismatch creates a KindInvalidInput error for transform output type
// mismatches.
func TransformContractTypeMismatch() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_TRANSFORM_CONTRACT_TYPE_MISMATCH",
		"transform output contract type mismatch",
	)
}

// TransformContractMissingField creates a KindInvalidInput error for missing transform contract
// fields.
func TransformContractMissingField() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_TRANSFORM_CONTRACT_MISSING_FIELD",
		"transform output contract missing required field",
	)
}

// TransformTypeValidationInput creates a KindInvalidInput error when type validation lacks
// datasource context.
func TransformTypeValidationInput() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_TRANSFORM_TYPE_VALIDATION_INPUT_REQUIRED",
		"transform type validation requires datasource context",
	)
}

// TransformRuntimeProjectionExpressionUnsupported creates a KindUnsupported error for unsupported
// transform runtime projection expressions.
func TransformRuntimeProjectionExpressionUnsupported() *Error {
	return newStandalone(
		KindUnsupported,
		"PIPELINE_TRANSFORM_RUNTIME_PROJECTION_EXPRESSION_UNSUPPORTED",
		"transform runtime projection expression is unsupported",
	)
}

// TransformRuntimeProjectionFieldNotFound creates a KindNotFound error for missing projection
// fields.
func TransformRuntimeProjectionFieldNotFound() *Error {
	return newStandalone(
		KindNotFound,
		"PIPELINE_TRANSFORM_RUNTIME_PROJECTION_FIELD_NOT_FOUND",
		"transform runtime projection field is missing",
	)
}

// TransformRuntimeAggregateExpressionUnsupported creates a KindUnsupported error for unsupported
// aggregate expressions.
func TransformRuntimeAggregateExpressionUnsupported() *Error {
	return newStandalone(
		KindUnsupported,
		"PIPELINE_TRANSFORM_RUNTIME_AGGREGATE_EXPRESSION_UNSUPPORTED",
		"transform runtime aggregate expression is unsupported",
	)
}

// InvalidTransformRuntimeAggregateColumnReferenceRequired creates a KindInvalidInput error when an
// aggregate expression is missing a column reference.
func InvalidTransformRuntimeAggregateColumnReferenceRequired() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_TRANSFORM_RUNTIME_AGGREGATE_COLUMN_REFERENCE_REQUIRED",
		"transform runtime aggregate requires a column reference",
	)
}

// TransformRuntimeWhereExpressionUnsupported creates a KindUnsupported error for unsupported
// runtime WHERE expressions.
func TransformRuntimeWhereExpressionUnsupported() *Error {
	return newStandalone(
		KindUnsupported,
		"PIPELINE_TRANSFORM_RUNTIME_WHERE_EXPRESSION_UNSUPPORTED",
		"transform runtime where expression is unsupported",
	)
}

// TransformRuntimeOutputKindUnsupported creates a KindUnsupported error for unsupported transform
// output kinds.
func TransformRuntimeOutputKindUnsupported() *Error {
	return newStandalone(
		KindUnsupported,
		"PIPELINE_TRANSFORM_RUNTIME_OUTPUT_KIND_UNSUPPORTED",
		"transform runtime output kind is unsupported",
	)
}

// TransformRuntimeHeadlessCSVNamedAccessUnsupported creates a KindUnsupported error when named
// field access is used for headless CSV values.
func TransformRuntimeHeadlessCSVNamedAccessUnsupported() *Error {
	return newStandalone(
		KindUnsupported,
		"PIPELINE_TRANSFORM_RUNTIME_HEADLESS_CSV_NAMED_ACCESS_UNSUPPORTED",
		"named field access is not allowed for headless CSV values",
	)
}

// UnexpectedTransformSpecRequired creates a KindInternal error for missing transform specs.
func UnexpectedTransformSpecRequired() *Error {
	return newStandalone(
		KindInternal,
		"PIPELINE_TRANSFORM_SPEC_REQUIRED",
		"transform spec is required",
	)
}

// UnexpectedUpstreamResultRequired creates a KindInternal error for missing upstream step results.
func UnexpectedUpstreamResultRequired() *Error {
	return newStandalone(
		KindInternal,
		"PIPELINE_UPSTREAM_RESULT_REQUIRED",
		"upstream result is required",
	)
}

// UnsupportedTransformRuntimeInput creates a KindUnsupported error for unsupported runtime input
// sources.
func UnsupportedTransformRuntimeInput() *Error {
	return newStandalone(
		KindUnsupported,
		"PIPELINE_TRANSFORM_RUNTIME_INPUT_UNSUPPORTED",
		"transform runtime currently supports only SQL/FTP/HTTP/MongoDB tabular inputs",
	)
}

// InvalidTransformLimitNegative creates a KindInvalidInput error when LIMIT offset or count is
// negative.
func InvalidTransformLimitNegative() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_TRANSFORM_LIMIT_NEGATIVE",
		"LIMIT offset and count must be non-negative",
	)
}

// InvalidTransformDivisionByZero creates a KindInvalidInput error for division by zero in runtime
// transforms.
func InvalidTransformDivisionByZero() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_TRANSFORM_DIVISION_BY_ZERO",
		"division by zero",
	)
}

// InvalidTransformSumArgumentRequired creates a KindInvalidInput error when SUM is missing an
// argument.
func InvalidTransformSumArgumentRequired() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_TRANSFORM_SUM_ARGUMENT_REQUIRED",
		"sum aggregate requires an argument",
	)
}

// InvalidTransformAvgArgumentRequired creates a KindInvalidInput error when AVG is missing an
// argument.
func InvalidTransformAvgArgumentRequired() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_TRANSFORM_AVG_ARGUMENT_REQUIRED",
		"avg aggregate requires an argument",
	)
}

// InvalidTransformCastTargetRequired creates a KindInvalidInput error when CAST target type is
// missing.
func InvalidTransformCastTargetRequired() *Error {
	return newStandalone(
		KindInvalidInput,
		"PIPELINE_TRANSFORM_CAST_TARGET_REQUIRED",
		"cast expression target type is required",
	)
}

// ExecutionFailed creates a KindUnavailable error for pipeline execution failures.
func ExecutionFailed() *Error {
	return newStandalone(KindUnavailable, "PIPELINE_EXECUTION_FAILED", "pipeline execution failed")
}

// ExecutionRowLimitExceeded creates a KindInvalidInput error when execution returns too many rows.
func ExecutionRowLimitExceeded() *Error {
	return newStandalone(
		KindInvalidInput,
		"EXECUTION_ROW_LIMIT_EXCEEDED",
		"execution row limit exceeded",
	)
}

// ExecutionResponseSizeExceeded creates a KindInvalidInput error when execution response data is
// too large.
func ExecutionResponseSizeExceeded() *Error {
	return newStandalone(
		KindInvalidInput,
		"EXECUTION_RESPONSE_SIZE_EXCEEDED",
		"execution response size exceeded",
	)
}

// ExecutionRequestSizeExceeded creates a KindInvalidInput error when an outbound execution request
// body is too large.
func ExecutionRequestSizeExceeded() *Error {
	return newStandalone(
		KindInvalidInput,
		"EXECUTION_REQUEST_SIZE_EXCEEDED",
		"execution request size exceeded",
	)
}

// ExecutionRedirectLimitExceeded creates a KindInvalidInput error when an HTTP connector follows
// too many redirects.
func ExecutionRedirectLimitExceeded() *Error {
	return newStandalone(
		KindInvalidInput,
		"EXECUTION_REDIRECT_LIMIT_EXCEEDED",
		"execution redirect limit exceeded",
	)
}

// ExecutionConcurrencyLimitExceeded creates a KindUnavailable error when a scoped runtime
// concurrency limit is already saturated.
func ExecutionConcurrencyLimitExceeded() *Error {
	return newStandalone(
		KindUnavailable,
		"EXECUTION_CONCURRENCY_LIMIT_EXCEEDED",
		"execution concurrency limit exceeded",
	)
}

// ExecutionUnavailable creates a KindInternal error when pipeline execution is not configured.
func ExecutionUnavailable() *Error {
	return newStandalone(
		KindInternal,
		"PIPELINE_EXECUTION_UNAVAILABLE",
		"pipeline execution unavailable",
	)
}

// DataSourceReadOnly creates a KindForbidden error when a read-only data source would mutate a
// remote system.
func DataSourceReadOnly() *Error {
	return newStandalone(KindForbidden, "DATA_SOURCE_READ_ONLY", "data source is read-only")
}

// DataSourceRefIncompatible creates a KindInvalidInput error for a logical ref
// that cannot be satisfied by its connector type or concrete binding.
func DataSourceRefIncompatible() *Error {
	return newStandalone(
		KindInvalidInput,
		"DATA_SOURCE_REF_INCOMPATIBLE",
		"data source ref is incompatible",
	)
}

// DataSourceBindingUnavailable creates a KindUnavailable error when a binding
// points at a concrete data source that cannot currently be used.
func DataSourceBindingUnavailable() *Error {
	return newStandalone(
		KindUnavailable,
		"DATA_SOURCE_BINDING_UNAVAILABLE",
		"data source binding is unavailable",
	)
}

// HTTPAcknowledgementRejected reports an explicit negative response without asserting rollback.
func HTTPAcknowledgementRejected() *Error {
	return newStandalone(KindInvalidInput, "HTTP_ACKNOWLEDGEMENT_REJECTED", "HTTP acknowledgement rejected")
}

// HTTPAcknowledgementUncertain prevents missing delivery evidence from becoming success.
func HTTPAcknowledgementUncertain() *Error {
	return newStandalone(KindUnavailable, "HTTP_ACKNOWLEDGEMENT_UNCERTAIN", "HTTP acknowledgement uncertain")
}

// SourceQueryInvalid reports a provider-rejected query without provider details.
func SourceQueryInvalid() *Error {
	return newStandalone(KindInvalidInput, "SOURCE_QUERY_INVALID", "The source query is invalid.")
}

// SourceSchemaReferenceInvalid reports an unresolved query schema reference.
func SourceSchemaReferenceInvalid() *Error {
	return newStandalone(KindInvalidInput, "SOURCE_SCHEMA_REFERENCE_INVALID", "The source query references an unavailable schema object.")
}
