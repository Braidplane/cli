package validate

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	workspace "github.com/Braidplane/cli/pkg/connector/googleworkspace"
	"github.com/Braidplane/cli/pkg/model"
	pipelinegraph "github.com/Braidplane/cli/pkg/pipeline/graph"
	"github.com/Braidplane/cli/pkg/pipeline/payload"
	pipelineruntime "github.com/Braidplane/cli/pkg/pipeline/runtime"
	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"
	transformsql "github.com/Braidplane/cli/pkg/pipeline/transform/sql"
	"github.com/Braidplane/cli/pkg/slug"
	structvalidate "github.com/Braidplane/cli/pkg/structvalidate"
	"github.com/Braidplane/cli/pkg/xerror"
)

var (
	ErrMissingField     = xerror.PipelineMissingField()
	ErrOrphanStep       = xerror.OrphanStep()
	ErrContractMismatch = xerror.PipelineContractMismatch()
	ErrInvalidFieldPath = xerror.InvalidFieldPath()
)

const (
	nodeKindInput     = "input"
	nodeKindQuery     = "query"
	nodeKindTransform = "transform"
	nodeKindSink      = "sink"
)

// Validate performs structural validation for steps, graph inputs,
// connectivity, and connector-specific incremental source contracts.
func Validate(p *pipelinespec.Pipeline) error {
	return validate(p, true)
}

// ValidateArtifactStructure checks immutable artifact integrity without applying
// current connectivity admission. Deployment and execution must use Validate.
func ValidateArtifactStructure(p *pipelinespec.Pipeline) error {
	return validate(p, false)
}

func validate(p *pipelinespec.Pipeline, requireConnected bool) error {
	if err := structvalidate.Default().Struct(p); err != nil {
		return xerror.Wrapf("validate pipeline: %w", err)
	}
	if err := pipelineruntime.ValidatePipelineExecution(p); err != nil {
		return err
	}
	if err := validateSteps(p.Steps); err != nil {
		return xerror.Wrapf("validate steps: %w", err)
	}
	if err := validateStepInputs(p, requireConnected); err != nil {
		return err
	}
	return nil
}

type EdgeContext struct {
	FromStep pipelinespec.Step
	ToStep   pipelinespec.Step
}

type stepEdgeContext struct {
	fromStep pipelinespec.Step
	toStep   pipelinespec.Step
}

type namedStepInput struct {
	name  string
	input pipelinespec.StepInput
}

type inferredOutputRequirement struct {
	toStep   slug.Slug
	contract *payload.Contract
}

type inferredFieldRequirement struct {
	toStep slug.Slug
	field  payload.ContractField
}

// validateSteps verifies kind-specific invariants for each step.
func validateSteps(steps []pipelinespec.Step) error {
	inputCount := 0
	for i, step := range steps {
		blockCount := 0
		for _, present := range []bool{step.Input != nil, step.Query != nil, step.Transform != nil, step.Sink != nil} {
			if present {
				blockCount++
			}
		}
		expectedBlock := (step.Kind == nodeKindInput && step.Input != nil) ||
			(step.Kind == nodeKindQuery && step.Query != nil) ||
			(step.Kind == nodeKindTransform && step.Transform != nil) ||
			(step.Kind == nodeKindSink && step.Sink != nil)
		if blockCount > 0 && (!expectedBlock || blockCount != 1) {
			return xerror.InvalidInput(xerror.Pipeline).Detailf(
				"step[%d] %q: kind=%s requires exactly its matching block",
				i, step.Slug, step.Kind,
			)
		}
		switch step.Kind {
		case nodeKindInput:
			inputCount++
			if inputCount > 1 {
				return xerror.InvalidInput(xerror.Pipeline).Detail("pipeline supports exactly one input step")
			}
			if step.Input == nil {
				return xerror.Unexpected().Detailf(
					"step[%d] %q: kind=input requires an input block",
					i,
					step.Slug,
				)
			}
			if err := structvalidate.Default().Struct(*step.Input); err != nil {
				return xerror.Wrapf("step[%d] %q: %w", i, step.Slug, err)
			}
			if err := validateContractDeclaration(step.Input.OutputContract, step.Input.OutputContractRef); err != nil {
				return xerror.Wrapf("step[%d] %q output_contract: %w", i, step.Slug, err)
			}
			if step.Input.OutputContract == nil && step.Input.OutputContractRef == nil {
				return xerror.InvalidInput(xerror.Pipeline).Detail("input requires an output contract or reference")
			}
			if ref := step.Input.OutputContractRef; ref != nil {
				if ref.Representation != model.ContractRepresentationJSON || ref.RootShape == model.ContractRootShapeUnknown {
					return xerror.InvalidInput(xerror.Pipeline).Detail("input output_contract_ref requires json representation and root_shape")
				}
			}
			if contract := step.Input.OutputContract; contract != nil {
				if contract.Representation != model.ContractRepresentationJSON {
					return xerror.InvalidInput(xerror.Pipeline).Detail("input output_contract requires json representation")
				}
				if err := validateStepContract(contract, false, model.ContractRepresentationJSON, contract.RootShape); err != nil {
					return xerror.Wrapf("step[%d] %q output_contract: %w", i, step.Slug, err)
				}
			}
		case nodeKindQuery:
			if step.Query == nil {
				return xerror.Unexpected().Detailf(
					"step[%d] %q: kind=query requires a query block",
					i,
					step.Slug,
				)
			}
			if err := structvalidate.Default().Struct(*step.Query); err != nil {
				return xerror.Wrapf("step[%d] %q: %w", i, step.Slug, err)
			}
			if err := validateQueryOperationContract(step.Query); err != nil {
				return xerror.Wrapf("step[%d] %q: %w", i, step.Slug, err)
			}
			if err := validateIncrementalSourceContract(step.Query); err != nil {
				return xerror.Wrapf("step[%d] %q: %w", i, step.Slug, err)
			}
			if err := validateContractDeclaration(step.Query.OutputContract, step.Query.OutputContractRef); err != nil {
				return xerror.Wrapf("step[%d] %q output_contract: %w", i, step.Slug, err)
			}
			queryRuntimeRepresentation, queryRuntimeRootShape := producerOutputFacets(step)
			queryRepresentation, queryRootShape := producerContractFacets(
				step.Query.OutputContract,
				queryRuntimeRepresentation,
				queryRuntimeRootShape,
			)
			if err := validateStepContract(
				step.Query.OutputContract,
				true,
				queryRepresentation,
				queryRootShape,
			); err != nil {
				return xerror.Wrapf("step[%d] %q output_contract: %w", i, step.Slug, err)
			}
			if err := validateQueryOutputRepresentation(step.Query); err != nil {
				return xerror.Wrapf("step[%d] %q output_contract: %w", i, step.Slug, err)
			}
		case "transform":
			if step.Transform == nil {
				return xerror.Unexpected().Detailf(
					"step[%d] %q: kind=transform requires a transform block",
					i,
					step.Slug,
				)
			}
			if err := structvalidate.Default().Struct(*step.Transform); err != nil {
				return xerror.Wrapf("step[%d] %q: %w", i, step.Slug, err)
			}
			if _, err := transformsql.ValidateTransformSQL(step.Transform.SQL); err != nil {
				return xerror.Wrapf("step[%d] %q: %w", i, step.Slug, err)
			}
			if err := validateContractDeclaration(step.Transform.InputContract, step.Transform.InputContractRef); err != nil {
				return xerror.Wrapf("step[%d] %q input_contract: %w", i, step.Slug, err)
			}
			if err := validateContractDeclaration(step.Transform.OutputContract, step.Transform.OutputContractRef); err != nil {
				return xerror.Wrapf("step[%d] %q output_contract: %w", i, step.Slug, err)
			}
			transformInputRepresentation, transformInputRootShape := outputContractFacets(
				step.Transform.InputContract,
				model.ContractRepresentationRows,
				model.ContractRootShapeUnknown,
			)
			if err := validateStepContract(
				step.Transform.InputContract,
				false,
				transformInputRepresentation,
				transformInputRootShape,
			); err != nil {
				return xerror.Wrapf("step[%d] %q input_contract: %w", i, step.Slug, err)
			}
			if err := validateSQLTransformContractRepresentation(step.Transform.InputContract); err != nil {
				return xerror.Wrapf("step[%d] %q input_contract: %w", i, step.Slug, err)
			}
			if err := validateTransformInputAttachments(step.Transform); err != nil {
				return xerror.Wrapf("step[%d] %q input_contracts: %w", i, step.Slug, err)
			}
			if err := validateNamedTransformInputContractPrefixes(step.Transform); err != nil {
				return xerror.Wrapf("step[%d] %q input_contract: %w", i, step.Slug, err)
			}
			transformRuntimeRepresentation, transformRuntimeRootShape := producerOutputFacets(step)
			transformOutputRepresentation, transformOutputRootShape := producerContractFacets(
				step.Transform.OutputContract,
				transformRuntimeRepresentation,
				transformRuntimeRootShape,
			)
			if err := validateStepContract(
				step.Transform.OutputContract,
				false,
				transformOutputRepresentation,
				transformOutputRootShape,
			); err != nil {
				return xerror.Wrapf("step[%d] %q output_contract: %w", i, step.Slug, err)
			}
			if err := validateSQLTransformContractRepresentation(step.Transform.OutputContract); err != nil {
				return xerror.Wrapf("step[%d] %q output_contract: %w", i, step.Slug, err)
			}
			if err := transformsql.ValidateOutputContractStructure(
				step.Transform.SQL,
				step.Transform.OutputContract,
			); err != nil {
				return xerror.Wrapf("step[%d] %q: %w", i, step.Slug, err)
			}
		case nodeKindSink:
			if step.Sink == nil {
				return xerror.Unexpected().Detailf(
					"step[%d] %q: kind=sink requires a sink block",
					i,
					step.Slug,
				)
			}
			if err := structvalidate.Default().Struct(*step.Sink); err != nil {
				return xerror.Wrapf("step[%d] %q: %w", i, step.Slug, err)
			}
			if err := validateSinkOperationContract(step.Sink); err != nil {
				return xerror.Wrapf("step[%d] %q: %w", i, step.Slug, err)
			}
			if err := validateContractDeclaration(step.Sink.InputContract, step.Sink.InputContractRef); err != nil {
				return xerror.Wrapf("step[%d] %q input_contract: %w", i, step.Slug, err)
			}
			sinkRepresentation, sinkRootShape := sinkInputContractFacets(step.Sink.InputContract)
			sinkRepresentation, sinkRootShape = outputContractFacets(
				step.Sink.InputContract,
				sinkRepresentation,
				sinkRootShape,
			)
			if err := validateStepContract(
				step.Sink.InputContract,
				true,
				sinkRepresentation,
				sinkRootShape,
			); err != nil {
				return xerror.Wrapf("step[%d] %q input_contract: %w", i, step.Slug, err)
			}
			if err := validateSinkInputRepresentation(step.Sink); err != nil {
				return xerror.Wrapf("step[%d] %q input_contract: %w", i, step.Slug, err)
			}
		default:
			return xerror.Unexpected().Detailf("step[%d]: unsupported step kind %q", i, step.Kind)
		}
	}
	return nil
}

func validateQueryOperationContract(query *pipelinespec.QueryRef) error {
	if query == nil {
		return nil
	}

	if query.OperationType == "" {
		return ErrMissingField.Detail("query target requires operation_type")
	}
	if !pipelinespec.IsSupportedQueryOperationType(query.OperationType) {
		return xerror.InvalidInput(xerror.Pipeline).Detailf(
			"query target uses unsupported operation_type %q",
			query.OperationType,
		)
	}

	hasRef := query.OperationRef != ""
	hasInline := len(query.OperationInline) > 0
	if hasRef == hasInline {
		return xerror.InvalidInput(xerror.Pipeline).Detail(
			"query target must set exactly one of operation_ref or operation_inline",
		)
	}

	if query.OperationType == pipelinespec.OperationTypeGoogleWorkspace && hasInline {
		raw, err := json.Marshal(query.OperationInline)
		if err != nil {
			return xerror.InvalidInput(xerror.Pipeline).With(err)
		}
		statement, err := workspace.DecodeStatement(raw)
		if err != nil {
			return xerror.Wrapf("validate Google Workspace operation: %w", err)
		}
		if !statement.IsRead() {
			return xerror.InvalidInput(xerror.Pipeline).Detail("Workspace source operations require read or list")
		}
	}
	return nil
}

func validateIncrementalSourceContract(query *pipelinespec.QueryRef) error {
	if query == nil || query.Incremental == nil {
		return nil
	}

	contract := query.Incremental
	if contract.CheckpointKind != nil &&
		*contract.CheckpointKind == model.IncrementalCheckpointKindUnknown {
		return xerror.InvalidInput(xerror.Pipeline).Detail(
			"incremental contract uses unsupported checkpoint_kind \"unknown\"",
		)
	}

	if err := validateIncrementalCheckpointContract(query.OperationType, contract); err != nil {
		return err
	}
	return nil
}

func validateIncrementalCheckpointContract(
	operationType string,
	contract *pipelinespec.IncrementalSourceContract,
) error {
	if contract.CheckpointKind == nil {
		return xerror.InvalidInput(xerror.Pipeline).Detail(
			"incremental contract requires checkpoint_kind",
		)
	}
	checkpointKind := *contract.CheckpointKind
	if !pipelinespec.SupportsIncrementalSourceCheckpoint(
		operationType,
		checkpointKind,
	) {
		return xerror.InvalidInput(xerror.Pipeline).Detailf(
			"incremental mode is not supported for operation_type %q and checkpoint_kind %q",
			operationType,
			checkpointKind.String(),
		)
	}

	switch checkpointKind {
	case model.IncrementalCheckpointKindWatermark:
		if contract.Field == "" {
			return xerror.InvalidInput(xerror.Pipeline).Detail(
				"watermark incremental contract requires field",
			)
		}
		if contract.Cursor != "" {
			return xerror.InvalidInput(xerror.Pipeline).Detail(
				"watermark incremental contract must not declare cursor",
			)
		}
		if err := validateLookbackWindow(contract.LookbackWindow); err != nil {
			return err
		}
	case model.IncrementalCheckpointKindCursor:
		if contract.Cursor == "" {
			return xerror.InvalidInput(xerror.Pipeline).Detail(
				"cursor incremental contract requires cursor",
			)
		}
		if contract.Field != "" {
			return xerror.InvalidInput(xerror.Pipeline).Detail(
				"cursor incremental contract must not declare field",
			)
		}
		if contract.LookbackWindow != "" {
			return xerror.InvalidInput(xerror.Pipeline).Detail(
				"cursor incremental contract must not declare lookback_window",
			)
		}
	default:
		return nil
	}

	return nil
}

func isWatermarkIncrementalContract(contract *pipelinespec.IncrementalSourceContract) bool {
	return contract != nil &&
		contract.CheckpointKind != nil &&
		*contract.CheckpointKind == model.IncrementalCheckpointKindWatermark
}

func contractHasField(contract *payload.Contract, fieldName string) bool {
	if contract == nil {
		return false
	}
	for _, field := range contract.Fields {
		if field.Name == fieldName {
			return true
		}
	}
	return false
}

func validateLookbackWindow(lookbackWindow string) error {
	if lookbackWindow == "" {
		return nil
	}
	if pipelineruntime.HasRuntimeVariableReference(lookbackWindow) {
		return nil
	}

	duration, err := time.ParseDuration(lookbackWindow)
	if err != nil || duration <= 0 {
		return xerror.InvalidInput(xerror.Pipeline).Detail(
			"lookback_window must be a positive duration",
		)
	}

	return nil
}

func validateSinkOperationContract(sink *pipelinespec.SinkSpec) error {
	if sink == nil {
		return nil
	}

	if sink.OperationType == "" {
		return xerror.Unexpected().Detail(
			"sink target requires operation_type",
		)
	}
	if sink.DataSourceSlug == "" {
		return xerror.Unexpected().Detail(
			"sink target requires data_source_ref",
		)
	}
	if !pipelinespec.IsSupportedSinkOperationType(sink.OperationType) {
		return xerror.Unexpected().Detailf(
			"sink target uses unsupported operation_type %q",
			sink.OperationType,
		)
	}
	hasRef := sink.OperationRef != ""
	hasInline := len(sink.OperationInline) > 0
	if hasRef == hasInline {
		return xerror.Unexpected().Detail(
			"sink target must set exactly one of operation_ref or operation_inline",
		)
	}

	if sink.OperationType == pipelinespec.OperationTypeGoogleWorkspace && hasInline {
		raw, err := json.Marshal(sink.OperationInline)
		if err != nil {
			return xerror.InvalidInput(xerror.Pipeline).With(err)
		}
		statement, err := workspace.DecodeStatement(raw)
		if err != nil {
			return xerror.Wrapf("validate Google Workspace operation: %w", err)
		}
		if statement.IsRead() {
			return xerror.InvalidInput(xerror.Pipeline).Detail("Workspace sink operations require create or update")
		}
		if (statement.Resource == "file" && statement.Operation == "create" || statement.IsNative()) && sink.IdempotencyClass != pipelinespec.SinkIdempotencyClassUnsafe {
			return xerror.InvalidInput(xerror.Pipeline).Detail("Native writes and Drive file creation require unsafe replay policy")
		}
		if statement.IsNative() && statement.Operation == "create" {
			return nil
		}
		var rows string
		binding := statement.Rows
		if statement.Resource == "file" || statement.IsNative() {
			binding = statement.Content
		}
		if err := json.Unmarshal(binding, &rows); err != nil || rows != "{{input}}" {
			return xerror.InvalidInput(xerror.Pipeline).Detail("Workspace inline sinks require payload bound to {{input}}")
		}
	}
	return nil
}

func validateStepInputs(p *pipelinespec.Pipeline, requireConnected bool) error {
	if p == nil {
		return nil
	}

	for _, step := range p.Steps {
		if step.Kind != nodeKindTransform || step.Transform == nil {
			continue
		}

		inputs := sortedTransformInputs(step.Transform)
		if len(inputs) > 0 {
			for _, namedInput := range inputs {
				if err := transformsql.ValidateTransformInputRelationName(namedInput.name); err != nil {
					return xerror.Wrapf(
						"validate step inputs: transform step %q input %q: %w",
						step.Slug,
						namedInput.name,
						err,
					)
				}
			}
		}
	}

	normalized, err := pipelinegraph.Normalize(p)
	if err != nil {
		return err
	}
	if requireConnected {
		if err := pipelinegraph.ValidateConnectivity(normalized); err != nil {
			return err
		}
	}
	return validateTransformCombineContracts(normalized)
}

func validateTransformCombineContracts(graph *pipelinegraph.Graph) error {
	for _, step := range graph.TopologicalSteps() {
		if step.Transform == nil {
			continue
		}
		inputs, _ := graph.TransformInputStepSlugs(step.Slug)
		contracts := make(map[string]*payload.Contract, len(inputs))
		for name, upstream := range inputs {
			producer, _ := graph.Step(upstream)
			contracts[name], _ = stepOutputContract(producer)
			if attachment := step.Transform.InputContracts[name]; attachment != nil && attachment.Contract != nil {
				contracts[name] = attachment.Contract
			}
		}
		if err := transformsql.ValidateTransformInputContracts(step.Transform.SQL, contracts); err != nil {
			return xerror.Wrapf("transform step %q: %w", step.Slug, err)
		}
	}
	return nil
}

func validateStepInputCompatibility(p *pipelinespec.Pipeline) error {
	contexts, err := stepEdgeContexts(p)
	if err != nil {
		return err
	}

	for _, ctx := range contexts {
		if edgeHasUnresolvedContractRef(ctx.fromStep, ctx.toStep) {
			continue
		}
		diagnostic := evaluateEdgeDiagnosticsForSteps(ctx.fromStep, ctx.toStep)
		if !diagnostic.Compatible {
			if diagnostic.ReasonCode == ContractReasonMissingContract {
				return xerror.Wrapf(
					"validate step inputs: edge %q incompatible contracts (%s): source step %q to destination step %q requires producer output_contract or consumer input_contract: %w",
					diagnostic.EdgeSlug,
					diagnostic.ReasonCode,
					diagnostic.FromStepSlug,
					diagnostic.ToStepSlug,
					ErrContractMismatch,
				)
			}
			detail := EdgeCompatibilityDetail(diagnostic)
			if detail != "" {
				return xerror.Wrapf(
					"validate step inputs: edge %q incompatible contracts (%s): %s: %w",
					diagnostic.EdgeSlug,
					diagnostic.ReasonCode,
					detail,
					ErrContractMismatch,
				)
			}
			return xerror.Wrapf(
				"validate step inputs: edge %q incompatible contracts (%s): %w",
				diagnostic.EdgeSlug,
				diagnostic.ReasonCode,
				ErrContractMismatch,
			)
		}
	}

	resolvedContexts := withoutUnresolvedContractRefEdges(contexts)
	if err := validateFanOutInferredOutputContracts(resolvedContexts); err != nil {
		return err
	}
	if err := validateWatermarkEffectiveOutputContracts(p, resolvedContexts); err != nil {
		return err
	}

	graph, err := pipelinegraph.Normalize(p)
	if err != nil {
		return err
	}
	return validateTransformCombineContracts(graph)
}

func withoutUnresolvedContractRefEdges(contexts []stepEdgeContext) []stepEdgeContext {
	resolved := make([]stepEdgeContext, 0, len(contexts))
	for _, ctx := range contexts {
		if !edgeHasUnresolvedContractRef(ctx.fromStep, ctx.toStep) {
			resolved = append(resolved, ctx)
		}
	}
	return resolved
}

func edgeHasUnresolvedContractRef(fromStep, toStep pipelinespec.Step) bool {
	return stepOutputContractRef(fromStep) != nil || stepInputContractRef(toStep) != nil
}

func stepOutputContractRef(step pipelinespec.Step) *pipelinespec.ODCSContractRef {
	switch step.Kind {
	case nodeKindInput:
		if step.Input != nil {
			return step.Input.OutputContractRef
		}
	case nodeKindQuery:
		if step.Query != nil {
			return step.Query.OutputContractRef
		}
	case nodeKindTransform:
		if step.Transform != nil {
			return step.Transform.OutputContractRef
		}
	}
	return nil
}

func stepInputContractRef(step pipelinespec.Step) *pipelinespec.ODCSContractRef {
	switch step.Kind {
	case nodeKindTransform:
		if step.Transform != nil {
			return step.Transform.InputContractRef
		}
	case nodeKindSink:
		if step.Sink != nil {
			return step.Sink.InputContractRef
		}
	}
	return nil
}

// ValidateStepInputCompatibility enforces explicit per-edge contracts and
// validates inferred edge contracts after the pipeline structure is valid.
func ValidateStepInputCompatibility(p *pipelinespec.Pipeline) error {
	return validateStepInputCompatibility(p)
}

// AggregateStepInputCompatibilityDiagnostics returns edge-scoped diagnostics for
// compatibility failures that involve more than one edge or source-level state.
func AggregateStepInputCompatibilityDiagnostics(
	p *pipelinespec.Pipeline,
) []EdgeCompatibilityResult {
	contexts, err := stepEdgeContexts(p)
	if err != nil {
		return nil
	}

	diagnostics := make([]EdgeCompatibilityResult, 0)
	contexts = withoutUnresolvedContractRefEdges(contexts)
	diagnostics = append(diagnostics, fanOutInferredOutputContractDiagnostics(contexts)...)
	diagnostics = append(diagnostics, watermarkEffectiveOutputContractDiagnostics(p, contexts)...)

	return diagnostics
}

// EdgeCompatibilityDetail formats expected and actual contract values so
// validation diagnostics identify the failing field or facet.
func EdgeCompatibilityDetail(diagnostic *EdgeCompatibilityResult) string {
	if diagnostic == nil || len(diagnostic.Expected) == 0 {
		return ""
	}

	keys := make([]string, 0, len(diagnostic.Expected))
	for key := range diagnostic.Expected {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		expected := diagnostic.Expected[key]
		actual := diagnostic.Actual[key]
		if actual == "" {
			parts = append(parts, fmt.Sprintf("expected %s=%q", key, expected))
			continue
		}
		parts = append(parts, fmt.Sprintf("expected %s=%q actual %s=%q", key, expected, key, actual))
	}

	return strings.Join(parts, ", ")
}

// validateFanOutInferredOutputContracts rejects incompatible consumer-only
// requirements that would otherwise imply contradictory producer output shapes.
func validateFanOutInferredOutputContracts(contexts []stepEdgeContext) error {
	if diagnostics := fanOutInferredOutputContractDiagnostics(contexts); len(diagnostics) > 0 {
		diagnostic := diagnostics[0]
		return xerror.Wrapf(
			"validate step inputs: producer %q has conflicting inferred output contract between downstream steps %q and %q (%s): %w",
			diagnostic.FromStepSlug,
			diagnostic.Expected["downstream_step"],
			diagnostic.Actual["downstream_step"],
			diagnostic.ReasonCode,
			ErrContractMismatch,
		)
	}

	return nil
}

// fanOutInferredOutputContractDiagnostics finds consumer-only fan-out edges
// whose merged effective producer contract would require contradictory shapes.
func fanOutInferredOutputContractDiagnostics(
	contexts []stepEdgeContext,
) []EdgeCompatibilityResult {
	requirementsByProducer := make(map[slug.Slug][]inferredOutputRequirement)
	stepsByProducer := make(map[slug.Slug]pipelinespec.Step)
	for _, ctx := range contexts {
		if _, hasProducer := stepOutputContract(ctx.fromStep); hasProducer {
			continue
		}
		consumer, hasConsumer := stepInputContract(ctx.toStep)
		if !hasConsumer {
			continue
		}
		requirementsByProducer[ctx.fromStep.Slug] = append(
			requirementsByProducer[ctx.fromStep.Slug],
			inferredOutputRequirement{
				toStep:   ctx.toStep.Slug,
				contract: inferredProducerContractForCombinedOutput(ctx.fromStep, consumer),
			},
		)
		stepsByProducer[ctx.fromStep.Slug] = ctx.fromStep
	}

	for fromStep, requirements := range requirementsByProducer {
		if len(requirements) < 2 {
			continue
		}
		for i := range requirements {
			for j := i + 1; j < len(requirements); j++ {
				diagnostic := inferredOutputRequirementConflictDiagnostic(
					stepsByProducer[fromStep],
					requirements[i],
					requirements[j],
				)
				if diagnostic != nil {
					return []EdgeCompatibilityResult{*diagnostic}
				}
			}
		}
	}

	return nil
}

// inferredOutputRequirementConflictDiagnostic compares two downstream inferred
// requirements and returns the edge-scoped diagnostic for the first conflict.
func inferredOutputRequirementConflictDiagnostic(
	step pipelinespec.Step,
	first inferredOutputRequirement,
	second inferredOutputRequirement,
) *EdgeCompatibilityResult {
	result := EdgeCompatibilityResult{
		EdgeSlug:     fmt.Sprintf("%s->%s", step.Slug, second.toStep),
		FromStepSlug: step.Slug,
		ToStepSlug:   second.toStep,
		Compatible:   false,
		Expected:     map[string]string{"downstream_step": first.toStep.String()},
		Actual:       map[string]string{"downstream_step": second.toStep.String()},
	}

	if first.contract.Representation != second.contract.Representation {
		result.ReasonCode = ContractReasonRepresentationMismatch
		result.Expected["representation"] = first.contract.Representation.String()
		result.Actual["representation"] = second.contract.Representation.String()
		return &result
	}
	if first.contract.RootShape != second.contract.RootShape {
		result.ReasonCode = ContractReasonRootShapeMismatch
		result.Expected["root_shape"] = first.contract.RootShape.String()
		result.Actual["root_shape"] = second.contract.RootShape.String()
		return &result
	}

	firstFields := make(map[string]payload.ContractField, len(first.contract.Fields))
	for _, field := range first.contract.Fields {
		firstFields[field.Name] = field
	}
	for _, secondField := range second.contract.Fields {
		firstField, ok := firstFields[secondField.Name]
		if !ok {
			continue
		}
		if compatibleInferredFieldTypes(firstField.Type, secondField.Type) {
			continue
		}
		if !firstField.Required && !secondField.Required {
			continue
		}
		result.ReasonCode = ContractReasonTypeMismatch
		result.Expected["field"] = secondField.Name
		result.Expected["type"] = firstField.Type.String()
		result.Actual["field"] = secondField.Name
		result.Actual["type"] = secondField.Type.String()
		return &result
	}

	return nil
}

// validateWatermarkEffectiveOutputContracts checks watermark source fields
// against the explicit or inferred effective output contract for each source.
func validateWatermarkEffectiveOutputContracts(
	p *pipelinespec.Pipeline,
	contexts []stepEdgeContext,
) error {
	if p == nil {
		return nil
	}
	if diagnostics := watermarkEffectiveOutputContractDiagnostics(p, contexts); len(diagnostics) > 0 {
		diagnostic := diagnostics[0]
		return xerror.Wrapf(
			"validate step inputs: source %q watermark incremental field %q must be present in effective output contract: %w",
			diagnostic.FromStepSlug,
			diagnostic.Expected["field"],
			ErrContractMismatch,
		)
	}

	return nil
}

// watermarkEffectiveOutputContractDiagnostics turns missing watermark fields
// into edge diagnostics so runtime preflight failures point at the source edge.
func watermarkEffectiveOutputContractDiagnostics(
	p *pipelinespec.Pipeline,
	contexts []stepEdgeContext,
) []EdgeCompatibilityResult {
	if p == nil {
		return nil
	}
	for _, step := range p.Steps {
		query := step.Query
		if query == nil || !isWatermarkIncrementalContract(query.Incremental) {
			continue
		}
		if query.OutputContractRef != nil {
			continue
		}

		contract, err := effectiveOutputContractForStep(step, contexts)
		if err != nil {
			continue
		}
		if contractHasField(contract, query.Incremental.Field) {
			continue
		}
		toStep := firstDownstreamStep(step.Slug, contexts)
		return []EdgeCompatibilityResult{{
			EdgeSlug:     fmt.Sprintf("%s->%s", step.Slug, toStep),
			FromStepSlug: step.Slug,
			ToStepSlug:   toStep,
			Compatible:   false,
			ReasonCode:   ContractReasonMissingRequiredField,
			Expected:     map[string]string{"field": query.Incremental.Field},
			Actual:       map[string]string{"field": "missing from effective output contract"},
		}}
	}

	return nil
}

// firstDownstreamStep returns one destination for source-scoped diagnostics that
// still need to fit the edge diagnostic shape.
func firstDownstreamStep(
	fromStep slug.Slug,
	contexts []stepEdgeContext,
) slug.Slug {
	for _, ctx := range contexts {
		if ctx.fromStep.Slug == fromStep {
			return ctx.toStep.Slug
		}
	}
	return ""
}

// effectiveOutputContractForStep returns the explicit producer contract or the
// merged set of downstream consumer contracts inferred for that producer.
func effectiveOutputContractForStep(
	step pipelinespec.Step,
	contexts []stepEdgeContext,
) (*payload.Contract, error) {
	if producer, hasProducer := stepOutputContract(step); hasProducer {
		return producer, nil
	}

	requirements := make([]inferredOutputRequirement, 0)
	for _, ctx := range contexts {
		if ctx.fromStep.Slug != step.Slug {
			continue
		}
		consumer, hasConsumer := stepInputContract(ctx.toStep)
		if !hasConsumer {
			continue
		}
		requirements = append(requirements, inferredOutputRequirement{
			toStep:   ctx.toStep.Slug,
			contract: inferredProducerContractForCombinedOutput(ctx.fromStep, consumer),
		})
	}
	if len(requirements) == 0 {
		representation, rootShape := producerOutputFacets(step)
		return &payload.Contract{
			Name:           fmt.Sprintf("%s-output", step.Slug),
			Representation: representation,
			RootShape:      rootShape,
		}, nil
	}

	merged, _, err := mergeInferredOutputRequirementsForStep(step, requirements)
	if err != nil {
		return nil, err
	}

	return merged, nil
}

// mergeInferredOutputRequirements combines downstream consumer-only contracts
// and reports the first incompatible fan-out requirement pair.
func mergeInferredOutputRequirements(
	name string,
	requirements []inferredOutputRequirement,
) (*payload.Contract, []slug.Slug, error) {
	merged := inferredContract(name, requirements[0].contract)
	sources := []slug.Slug{requirements[0].toStep}
	fieldSources := make(map[string]slug.Slug, len(merged.Fields))
	fieldRequirements := make(map[string][]inferredFieldRequirement, len(merged.Fields))
	for _, field := range merged.Fields {
		fieldSources[field.Name] = requirements[0].toStep
		fieldRequirements[field.Name] = append(
			fieldRequirements[field.Name],
			inferredFieldRequirement{toStep: requirements[0].toStep, field: field},
		)
	}

	for _, requirement := range requirements[1:] {
		if err := mergeInferredContract(
			merged,
			fieldSources,
			fieldRequirements,
			requirement,
		); err != nil {
			return nil, nil, err
		}
		sources = append(sources, requirement.toStep)
	}

	return merged, sources, nil
}

// mergeInferredOutputRequirementsForStep combines consumer-inferred fields
// while preserving output facets already implied by the producer step.
func mergeInferredOutputRequirementsForStep(
	step pipelinespec.Step,
	requirements []inferredOutputRequirement,
) (*payload.Contract, []slug.Slug, error) {
	merged, sources, err := mergeInferredOutputRequirements(
		fmt.Sprintf("%s-output", step.Slug),
		requirements,
	)
	if err != nil {
		return nil, nil, err
	}
	applyProducerOutputFacets(step, merged)
	return merged, sources, nil
}

// mergeInferredContract mutates merged with one consumer-inferred requirement
// while preserving the strictest common field requirements.
func mergeInferredContract(
	merged *payload.Contract,
	fieldSources map[string]slug.Slug,
	fieldRequirements map[string][]inferredFieldRequirement,
	requirement inferredOutputRequirement,
) error {
	contract := requirement.contract
	if merged.Representation != contract.Representation {
		return conflictingInferredOutputError(
			merged.Name,
			fieldSources,
			requirement,
			"representation",
		)
	}
	if merged.RootShape != contract.RootShape {
		return conflictingInferredOutputError(
			merged.Name,
			fieldSources,
			requirement,
			"root_shape",
		)
	}

	fieldIndexes := make(map[string]int, len(merged.Fields))
	for i, field := range merged.Fields {
		fieldIndexes[field.Name] = i
	}
	for _, field := range contract.Fields {
		i, exists := fieldIndexes[field.Name]
		if !exists {
			merged.Fields = append(merged.Fields, field)
			fieldSources[field.Name] = requirement.toStep
			fieldRequirements[field.Name] = append(
				fieldRequirements[field.Name],
				inferredFieldRequirement{toStep: requirement.toStep, field: field},
			)
			fieldIndexes[field.Name] = len(merged.Fields) - 1
			continue
		}

		for _, previous := range fieldRequirements[field.Name] {
			if compatibleInferredFieldTypes(previous.field.Type, field.Type) {
				if previous.field.Type == model.ContractFieldTypeValueDecimal && field.Type == model.ContractFieldTypeValueDecimal && (payload.ValidateDecimalFieldDeclaration(previous.field) != nil || payload.ValidateDecimalFieldDeclaration(field) != nil) {
					return conflictingInferredOutputError(merged.Name, map[string]slug.Slug{field.Name: previous.toStep}, requirement, "invalid decimal declaration")
				}
				continue
			}
			if !previous.field.Required && !field.Required {
				continue
			}
			return conflictingInferredOutputError(
				merged.Name,
				map[string]slug.Slug{field.Name: previous.toStep},
				requirement,
				fmt.Sprintf("field %q type", field.Name),
			)
		}

		existing := merged.Fields[i]
		mergedField := mergeInferredField(existing, field)
		if err := payload.ValidateDecimalFieldDeclaration(mergedField); err != nil {
			return conflictingInferredOutputError(merged.Name, fieldSources, requirement, "decimal requirements have no representable shared declaration")
		}
		merged.Fields[i] = mergedField
		fieldRequirements[field.Name] = append(
			fieldRequirements[field.Name],
			inferredFieldRequirement{toStep: requirement.toStep, field: field},
		)
	}

	return nil
}

// compatibleInferredFieldTypes reports whether one concrete output type can
// satisfy both downstream consumer requirements.
func compatibleInferredFieldTypes(
	existing model.ContractFieldType,
	next model.ContractFieldType,
) bool {
	return isCompatibleContractFieldType(existing, next) ||
		isCompatibleContractFieldType(next, existing)
}

// mergeInferredField keeps the stricter shared requirement for a field required
// by more than one downstream edge.
func mergeInferredField(
	existing payload.ContractField,
	next payload.ContractField,
) payload.ContractField {
	if existing.Type == model.ContractFieldTypeValueNumber &&
		next.Type == model.ContractFieldTypeValueInteger {
		existing.Type = next.Type
	}
	if existing.Type == model.ContractFieldTypeValueDecimal && next.Type == model.ContractFieldTypeValueInteger {
		existing.Type = next.Type
		existing.Precision, existing.Scale = nil, nil
	}
	if existing.Type == model.ContractFieldTypeValueDecimal && next.Type == model.ContractFieldTypeValueDecimal && existing.Precision != nil && existing.Scale != nil && next.Precision != nil && next.Scale != nil {
		scale := min(*existing.Scale, *next.Scale)
		precision := min(*existing.Precision-*existing.Scale, *next.Precision-*next.Scale) + scale
		existing.Precision, existing.Scale = &precision, &scale
	}
	existing.Required = existing.Required || next.Required
	existing.Nullable = existing.Nullable && next.Nullable
	return existing
}

// conflictingInferredOutputError formats a fan-out conflict with the producer
// and the downstream edges that introduced incompatible inferred requirements.
func conflictingInferredOutputError(
	producerName string,
	fieldSources map[string]slug.Slug,
	requirement inferredOutputRequirement,
	reason string,
) error {
	firstStep := slug.Slug("")
	for _, source := range fieldSources {
		firstStep = source
		break
	}
	return xerror.Wrapf(
		"validate step inputs: producer %q has conflicting inferred output contract between downstream steps %q and %q (%s): %w",
		strings.TrimSuffix(producerName, "-output"),
		firstStep,
		requirement.toStep,
		reason,
		ErrContractMismatch,
	)
}

func StepEdgeContexts(p *pipelinespec.Pipeline) ([]EdgeContext, error) {
	if p == nil {
		return nil, nil
	}

	normalized, err := pipelinegraph.Normalize(p)
	if err != nil {
		return nil, err
	}

	edges := normalized.StepInputEdges()
	contexts := make([]EdgeContext, 0, len(edges))
	for _, edge := range edges {
		fromStep, fromOK := normalized.Step(edge.FromStepSlug)
		toStep, toOK := normalized.Step(edge.ToStepSlug)
		if !fromOK || !toOK {
			continue
		}
		if edge.InputName != "" && toStep.Transform != nil {
			transform := *toStep.Transform
			transform.InputContract = contractForNamedTransformInput(
				toStep.Transform.InputContract,
				edge.InputName,
				len(toStep.Transform.Inputs),
			)
			if toStep.Transform.InputContracts != nil {
				transform.InputContracts = map[string]*pipelinespec.InputContractAttachment{edge.InputName: toStep.Transform.InputContracts[edge.InputName]}
				if attachment := toStep.Transform.InputContracts[edge.InputName]; attachment != nil {
					transform.InputContract = attachment.Contract
					transform.InputContractRef = attachment.ContractRef
				}
			}
			toStep.Transform = &transform
		}
		contexts = append(contexts, EdgeContext{FromStep: fromStep, ToStep: toStep})
	}
	return contexts, nil
}

func stepEdgeContexts(p *pipelinespec.Pipeline) ([]stepEdgeContext, error) {
	contexts, err := StepEdgeContexts(p)
	if err != nil {
		return nil, err
	}
	privateContexts := make([]stepEdgeContext, 0, len(contexts))
	for _, ctx := range contexts {
		privateContexts = append(privateContexts, stepEdgeContext{
			fromStep: ctx.FromStep,
			toStep:   ctx.ToStep,
		})
	}
	return privateContexts, nil
}

func sortedTransformInputs(transform *pipelinespec.TransformSpec) []namedStepInput {
	if transform == nil || len(transform.Inputs) == 0 {
		return nil
	}

	names := make([]string, 0, len(transform.Inputs))
	for name := range transform.Inputs {
		names = append(names, name)
	}
	sort.Strings(names)

	inputs := make([]namedStepInput, 0, len(names))
	for _, name := range names {
		inputs = append(inputs, namedStepInput{name: name, input: transform.Inputs[name]})
	}
	return inputs
}

// evaluateEdgeDiagnosticsForSteps assesses contract-based compatibility for one step-to-step edge.
func EvaluateEdgeDiagnosticsForSteps(
	fromStep pipelinespec.Step,
	toStep pipelinespec.Step,
) (result *EdgeCompatibilityResult) {
	defer func() {
		if result != nil && toStep.Transform != nil && len(toStep.Transform.InputContracts) == 1 {
			for name := range toStep.Transform.InputContracts {
				result.EdgeSlug += ":" + name
			}
		}
	}()
	edge := EdgeLink{FromStep: fromStep.Slug, ToStep: toStep.Slug}
	if diagnostic := producerContractFacetDiagnostic(edge, fromStep); diagnostic != nil {
		return diagnostic
	}

	producer, hasProducer := stepOutputContract(fromStep)
	consumer, hasConsumer := stepInputContract(toStep)
	if !hasProducer && !hasConsumer {
		result := EdgeCompatibilityResult{
			EdgeSlug:     fmt.Sprintf("%s->%s", fromStep.Slug, toStep.Slug),
			FromStepSlug: fromStep.Slug,
			ToStepSlug:   toStep.Slug,
			Compatible:   false,
			ReasonCode:   ContractReasonMissingContract,
			Expected: map[string]string{
				"contract": "producer output_contract or consumer input_contract",
			},
			Actual: map[string]string{"contract": "missing"},
		}
		return &result
	}
	if !hasProducer {
		producer = inferredProducerContractForEdge(fromStep, toStep, consumer)
	}
	if !hasConsumer {
		consumer = inferredContract(fmt.Sprintf("%s-input", toStep.Slug), producer)
	}

	diagnostic := EvaluateEdgeCompatibility(edge, producer, consumer)

	return &diagnostic
}

// producerContractFacetDiagnostic rejects explicit producer contracts that
// contradict runtime facets known from query operation type or transform output kind.
func producerContractFacetDiagnostic(
	edge EdgeLink,
	step pipelinespec.Step,
) *EdgeCompatibilityResult {
	contract := producerDeclaredContract(step)
	if contract == nil {
		return nil
	}

	runtimeRepresentation, runtimeRootShape := producerOutputFacets(step)
	result := EdgeCompatibilityResult{
		EdgeSlug:     fmt.Sprintf("%s->%s", edge.FromStep, edge.ToStep),
		FromStepSlug: edge.FromStep,
		ToStepSlug:   edge.ToStep,
		Compatible:   false,
	}
	if runtimeRepresentation != model.ContractRepresentationUnknown &&
		contract.Representation != model.ContractRepresentationUnknown &&
		contract.Representation != runtimeRepresentation {
		result.ReasonCode = ContractReasonRepresentationMismatch
		result.Expected = map[string]string{"producer_representation": runtimeRepresentation.String()}
		result.Actual = map[string]string{"producer_representation": contract.Representation.String()}
		return &result
	}
	if runtimeRootShape != model.ContractRootShapeUnknown &&
		contract.RootShape != model.ContractRootShapeUnknown &&
		contract.RootShape != runtimeRootShape {
		result.ReasonCode = ContractReasonRootShapeMismatch
		result.Expected = map[string]string{"producer_root_shape": runtimeRootShape.String()}
		result.Actual = map[string]string{"producer_root_shape": contract.RootShape.String()}
		return &result
	}

	return nil
}

// producerDeclaredContract returns the author-declared output contract for
// producer steps without applying inferred or runtime facet normalization.
func producerDeclaredContract(step pipelinespec.Step) *payload.Contract {
	switch step.Kind {
	case nodeKindInput:
		if step.Input == nil {
			return nil
		}
		return step.Input.OutputContract
	case nodeKindQuery:
		if step.Query == nil {
			return nil
		}
		return step.Query.OutputContract
	case nodeKindTransform:
		if step.Transform == nil {
			return nil
		}
		return step.Transform.OutputContract
	default:
		return nil
	}
}

func evaluateEdgeDiagnosticsForSteps(
	fromStep pipelinespec.Step,
	toStep pipelinespec.Step,
) *EdgeCompatibilityResult {
	return EvaluateEdgeDiagnosticsForSteps(fromStep, toStep)
}

func stepOutputContract(step pipelinespec.Step) (*payload.Contract, bool) {
	if step.Kind == nodeKindInput && step.Input != nil {
		if step.Input.OutputContract == nil {
			return nil, false
		}

		return outputContractToSchemaContract(
			fmt.Sprintf("%s-output", step.Slug),
			model.ContractRepresentationJSON,
			step.Input.OutputContract.RootShape,
			step.Input.OutputContract,
		), true
	}

	if step.Kind == nodeKindQuery && step.Query != nil {
		if step.Query.OutputContract == nil {
			return nil, false
		}

		runtimeRepresentation, runtimeRootShape := producerOutputFacets(step)
		representation, rootShape := producerContractFacets(
			step.Query.OutputContract,
			runtimeRepresentation,
			runtimeRootShape,
		)

		return outputContractToSchemaContract(
			fmt.Sprintf("%s-output", step.Slug),
			representation,
			rootShape,
			step.Query.OutputContract,
		), true
	}

	if step.Kind != nodeKindTransform || step.Transform == nil {
		return nil, false
	}

	if step.Transform.OutputContract == nil {
		return nil, false
	}

	runtimeRepresentation, runtimeRootShape := producerOutputFacets(step)
	representation, rootShape := producerContractFacets(
		step.Transform.OutputContract,
		runtimeRepresentation,
		runtimeRootShape,
	)

	return outputContractToSchemaContract(
		fmt.Sprintf("%s-output", step.Slug),
		representation,
		rootShape,
		step.Transform.OutputContract,
	), true
}

// StepInputContract returns the explicit consumer contract for a transform or sink step.
func StepInputContract(step pipelinespec.Step) *payload.Contract {
	contract, _ := stepInputContract(step)
	return contract
}

func stepInputContract(step pipelinespec.Step) (*payload.Contract, bool) {
	switch step.Kind {
	case nodeKindSink:
		if step.Sink == nil {
			return nil, false
		}

		if step.Sink.InputContract == nil {
			return nil, false
		}

		representation, rootShape := sinkInputContractFacets(step.Sink.InputContract)
		representation, rootShape = outputContractFacets(
			step.Sink.InputContract,
			representation,
			rootShape,
		)

		return outputContractToSchemaContract(
			fmt.Sprintf("%s-input", step.Slug),
			representation,
			rootShape,
			step.Sink.InputContract,
		), true
	case nodeKindTransform:
		if step.Transform == nil {
			return nil, false
		}

		if step.Transform.InputContract == nil {
			return nil, false
		}

		representation, rootShape := outputContractFacets(
			step.Transform.InputContract,
			model.ContractRepresentationRows,
			model.ContractRootShapeUnknown,
		)

		return outputContractToSchemaContract(
			fmt.Sprintf("%s-input", step.Slug),
			representation,
			rootShape,
			step.Transform.InputContract,
		), true
	default:
		return nil, false
	}
}

func SinkInputKindForStep(step pipelinespec.Step) model.TransformOutputKind {
	if step.Sink != nil && step.Sink.InputContract != nil {
		derivedKind := contractRepresentationToOutputKind(step.Sink.InputContract.Representation)
		if derivedKind != model.TransformOutputKindUnknown {
			return derivedKind
		}
	}

	return model.TransformOutputKindRows
}

// sinkInputContractFacets returns sink consumer defaults without inventing a JSON
// root shape the sink did not declare.
func sinkInputContractFacets(
	contract *payload.Contract,
) (model.ContractRepresentation, model.ContractRootShape) {
	representation := model.ContractRepresentationRows
	if contract != nil && contract.Representation != model.ContractRepresentationUnknown {
		representation = contract.Representation
	}

	rootShape := model.ContractRootShapeUnknown
	if contract != nil && contract.RootShape != model.ContractRootShapeUnknown {
		rootShape = contract.RootShape
	}

	return representation, rootShape
}

func contractForNamedTransformInput(
	contract *payload.Contract,
	inputName string,
	inputCount int,
) *payload.Contract {
	if contract == nil || inputCount <= 1 {
		return contract
	}

	prefix := inputName + "."
	fields := make([]payload.ContractField, 0, len(contract.Fields))
	for _, field := range contract.Fields {
		if !strings.HasPrefix(field.Name, prefix) {
			continue
		}
		field.Name = strings.TrimPrefix(field.Name, prefix)
		fields = append(fields, field)
	}
	if len(fields) == 0 {
		return nil
	}

	namedContract := payload.CloneContract(contract)
	namedContract.Fields = fields
	return namedContract
}

// inferredContract clones an explicit contract to represent the missing side of
// one edge without mutating the author-declared producer or consumer contract.
func inferredContract(name string, contract *payload.Contract) *payload.Contract {
	inferred := payload.CloneContract(contract)
	if inferred == nil {
		return nil
	}
	inferred.Name = name
	return inferred
}

// inferredProducerContractForEdge derives a missing producer contract while
// preserving transform-specific row views that are normalized by runtime.
func inferredProducerContractForEdge(
	fromStep pipelinespec.Step,
	toStep pipelinespec.Step,
	contract *payload.Contract,
) *payload.Contract {
	inferred := inferredContract(fmt.Sprintf("%s-output", fromStep.Slug), contract)
	if inferred == nil {
		return nil
	}
	if usesTransformRowViewOfMongoDBDocuments(fromStep, toStep, inferred) {
		return inferred
	}
	applyProducerOutputFacets(fromStep, inferred)
	return inferred
}

// inferredProducerContractForCombinedOutput derives the producer's actual
// effective shape for validations that merge requirements across fan-out edges.
func inferredProducerContractForCombinedOutput(
	fromStep pipelinespec.Step,
	contract *payload.Contract,
) *payload.Contract {
	inferred := inferredContract(fmt.Sprintf("%s-output", fromStep.Slug), contract)
	applyProducerOutputFacets(fromStep, inferred)
	return inferred
}

// usesTransformRowViewOfMongoDBDocuments preserves the SQL transform runtime's
// documented behavior of turning MongoDB document arrays into row inputs.
func usesTransformRowViewOfMongoDBDocuments(
	fromStep pipelinespec.Step,
	toStep pipelinespec.Step,
	contract *payload.Contract,
) bool {
	if toStep.Transform == nil || contract == nil {
		return false
	}
	if !isMongoDBQuery(fromStep) {
		return false
	}
	return contract.Representation == model.ContractRepresentationRows ||
		contract.Representation == model.ContractRepresentationUnknown
}

func isMongoDBQuery(step pipelinespec.Step) bool {
	return step.Query != nil &&
		strings.EqualFold(strings.TrimSpace(step.Query.OperationType), "mongodb")
}

// producerOutputFacets returns output representation constraints implied by a
// producer kind even when it omits an explicit output_contract.
func producerOutputFacets(
	step pipelinespec.Step,
) (model.ContractRepresentation, model.ContractRootShape) {
	if step.Kind == nodeKindInput && step.Input != nil {
		rootShape := model.ContractRootShapeUnknown
		if step.Input.OutputContract != nil {
			rootShape = step.Input.OutputContract.RootShape
		}
		return model.ContractRepresentationJSON, rootShape
	}
	if step.Kind == nodeKindQuery && step.Query != nil {
		return queryOutputFacets(step.Query)
	}
	if step.Kind == nodeKindTransform && step.Transform != nil {
		return outputKindToContractFacets(step.Transform.OutputKind)
	}
	return model.ContractRepresentationUnknown, model.ContractRootShapeUnknown
}

// queryOutputFacets returns representation constraints known from query
// operation types without pretending dynamic HTTP response media types are static.
func queryOutputFacets(
	query *pipelinespec.QueryRef,
) (model.ContractRepresentation, model.ContractRootShape) {
	if query == nil {
		return model.ContractRepresentationUnknown, model.ContractRootShapeUnknown
	}
	switch strings.ToLower(strings.TrimSpace(query.OperationType)) {
	case "", "sql", "postgres", "mysql", "hubspot":
		return model.ContractRepresentationRows, model.ContractRootShapeUnknown
	case "google_workspace":
		if query.OperationRef != "" {
			return model.ContractRepresentationUnknown, model.ContractRootShapeUnknown
		}
		if query.OperationInline["operation"] == "export" || query.OperationInline["resource"] == "file" && queryInlineFormat(query) == "file" {
			return model.ContractRepresentationFile, model.ContractRootShapeUnknown
		}
		return model.ContractRepresentationRows, model.ContractRootShapeUnknown
	case "ftp", "git":
		if queryInlineFormat(query) == model.ContractRepresentationFile.String() {
			return model.ContractRepresentationFile, model.ContractRootShapeUnknown
		}
		if query.OperationRef == "" {
			return model.ContractRepresentationRows, model.ContractRootShapeUnknown
		}
		return model.ContractRepresentationUnknown, model.ContractRootShapeUnknown
	case "mongodb":
		return model.ContractRepresentationJSON, model.ContractRootShapeArray
	default:
		return model.ContractRepresentationUnknown, model.ContractRootShapeUnknown
	}
}

// queryInlineFormat extracts a static inline file format when the query embeds one.
func queryInlineFormat(query *pipelinespec.QueryRef) string {
	if query == nil || len(query.OperationInline) == 0 {
		return ""
	}

	raw, ok := query.OperationInline["format"]
	if !ok {
		return ""
	}

	value, ok := raw.(string)
	if !ok {
		return ""
	}

	return strings.ToLower(strings.TrimSpace(value))
}

// applyProducerOutputFacets mutates an inferred producer contract so validation
// checks fields from the consumer without forgetting default producer output kind.
func applyProducerOutputFacets(
	step pipelinespec.Step,
	contract *payload.Contract,
) {
	if contract == nil {
		return
	}

	representation, rootShape := producerInferredOutputFacets(step)
	if representation != model.ContractRepresentationUnknown {
		contract.Representation = representation
		if representation != model.ContractRepresentationJSON {
			contract.RootShape = model.ContractRootShapeUnknown
		}
	}
	if rootShape != model.ContractRootShapeUnknown {
		contract.RootShape = rootShape
	}
}

// producerInferredOutputFacets returns the default producer facets used when a
// missing output_contract is inferred from downstream consumer contracts.
func producerInferredOutputFacets(
	step pipelinespec.Step,
) (model.ContractRepresentation, model.ContractRootShape) {
	representation, rootShape := producerOutputFacets(step)
	if representation != model.ContractRepresentationUnknown {
		return representation, rootShape
	}
	if step.Query == nil {
		return representation, rootShape
	}

	switch strings.ToLower(strings.TrimSpace(step.Query.OperationType)) {
	case "ftp", "git":
		return model.ContractRepresentationRows, model.ContractRootShapeUnknown
	default:
		return representation, rootShape
	}
}

// validateStepContract checks shared payload contract structure and limits
// native file contracts to runtimes that can pass opaque bytes through safely.
func validateStepContract(
	contract *payload.Contract,
	allowNativeFile bool,
	effectiveRepresentation model.ContractRepresentation,
	effectiveRootShape model.ContractRootShape,
) error {
	if contract == nil {
		return nil
	}

	if contract.Representation == model.ContractRepresentationFile && !allowNativeFile {
		return xerror.InvalidInput(xerror.Pipeline).Detail(
			"file representation is only supported for query output_contract and sink input_contract",
		)
	}

	fieldValidation := "required,min=1,dive"
	if contract.Representation == model.ContractRepresentationFile {
		fieldValidation = "omitempty,dive"
	}

	if err := structvalidate.Default().Var(
		contract.Representation,
		"omitempty,contractrepresentation",
	); err != nil {
		return xerror.ValidationFailed().Detailf("validate step contract: %w", err)
	}
	if err := structvalidate.Default().Var(
		contract.RootShape,
		"omitempty,contractrootshape",
	); err != nil {
		return xerror.ValidationFailed().Detailf("validate step contract: %w", err)
	}
	if err := structvalidate.Default().Var(contract.Fields, fieldValidation); err != nil {
		return xerror.ValidationFailed().Detailf("validate step contract: %w", err)
	}

	seen := make(map[string]struct{}, len(contract.Fields))
	for _, field := range contract.Fields {
		if _, exists := seen[field.Name]; exists {
			return xerror.Unexpected().Detailf("duplicate field %q", field.Name)
		}
		seen[field.Name] = struct{}{}
		if err := validateFieldPathSyntax(field.Name); err != nil {
			return xerror.Wrapf("field %q: %w", field.Name, err)
		}
	}
	declarationContract := payload.CloneContract(contract)
	if effectiveRepresentation == model.ContractRepresentationJSON ||
		(declarationContract.Representation == model.ContractRepresentationUnknown &&
			effectiveRepresentation != model.ContractRepresentationUnknown) {
		declarationContract.Representation = effectiveRepresentation
	}
	if declarationContract.RootShape == model.ContractRootShapeUnknown &&
		effectiveRootShape != model.ContractRootShapeUnknown {
		declarationContract.RootShape = effectiveRootShape
	}
	if err := payload.ValidateContractFieldDeclarations(declarationContract); err != nil {
		return err
	}

	return nil
}

func validateContractDeclaration(contract *payload.Contract, ref *pipelinespec.ODCSContractRef) error {
	if contract != nil && ref != nil {
		return xerror.InvalidInput(xerror.Pipeline).Detail(
			"inline contract and contract reference are mutually exclusive",
		)
	}
	if ref == nil {
		return nil
	}
	if _, err := slug.New(ref.ContractSlug.String()); err != nil {
		return xerror.InvalidInput(xerror.Pipeline).Detail("contract reference requires a valid contract_slug")
	}
	if _, err := slug.New(ref.RevisionSlug.String()); err != nil || ref.RevisionSlug == "latest" {
		return xerror.InvalidInput(xerror.Pipeline).Detail("contract reference requires an exact revision_slug")
	}
	if strings.TrimSpace(ref.SchemaObjectID) == "" {
		return xerror.InvalidInput(xerror.Pipeline).Detail("contract reference requires schema_object_id")
	}
	if err := structvalidate.Default().Var(ref.Representation, "omitempty,contractrepresentation"); err != nil {
		return xerror.ValidationFailed().Detailf("validate contract reference: %w", err)
	}
	if err := structvalidate.Default().Var(ref.RootShape, "omitempty,contractrootshape"); err != nil {
		return xerror.ValidationFailed().Detailf("validate contract reference: %w", err)
	}
	return nil
}

// validateNamedTransformInputContractPrefixes ensures multi-input transform
// contracts can be split unambiguously into per-edge requirements.
func validateNamedTransformInputContractPrefixes(transform *pipelinespec.TransformSpec) error {
	if transform == nil || transform.InputContract == nil || len(transform.Inputs) <= 1 {
		return nil
	}

	prefixes := make([]string, 0, len(transform.Inputs))
	for inputName := range transform.Inputs {
		prefixes = append(prefixes, inputName+".")
	}
	sort.Strings(prefixes)

	for _, field := range transform.InputContract.Fields {
		matched := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(field.Name, prefix) {
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		return xerror.InvalidInput(xerror.Pipeline).Detailf(
			"multi-input transform field %q must be prefixed with one of: %s",
			field.Name,
			strings.Join(prefixes, ", "),
		)
	}

	return nil
}

// validateFieldPathSyntax checks that name is a well-formed dot-delimited field path.
// It accepts top-level names ("email"), nested paths ("profile.email"),
// and array traversal paths ("items[].sku"). Each segment must be non-empty.
func validateFieldPathSyntax(name string) error {
	if name == "" {
		return ErrInvalidFieldPath.Detail("empty path")
	}
	for _, seg := range strings.Split(name, ".") {
		if strings.TrimSuffix(seg, "[]") == "" {
			return ErrInvalidFieldPath.Detailf("empty segment in %q", name)
		}
	}
	return nil
}

// validateQueryOutputRepresentation keeps form output declarations on HTTP sources.
func validateQueryOutputRepresentation(query *pipelinespec.QueryRef) error {
	if query == nil || query.OutputContract == nil {
		return nil
	}
	if query.OutputContract.Representation != model.ContractRepresentationMultipartForm {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(query.OperationType), "http") {
		return nil
	}

	return xerror.InvalidInput(xerror.Pipeline).Detail(
		"multipart_form query output_contract is only supported for http operation_type",
	)
}

// validateSQLTransformContractRepresentation rejects payloads the SQL runtime cannot read or emit.
func validateSQLTransformContractRepresentation(contract *payload.Contract) error {
	if contract == nil || contract.Representation != model.ContractRepresentationMultipartForm {
		return nil
	}

	return xerror.InvalidInput(xerror.Pipeline).Detail(
		"multipart_form contracts are not supported by sql_v1 transform steps",
	)
}

// validateSinkInputRepresentation keeps form sink contracts on HTTP delivery operations.
func validateSinkInputRepresentation(sink *pipelinespec.SinkSpec) error {
	if sink == nil || sink.InputContract == nil {
		return nil
	}
	if sink.InputContract.Representation != model.ContractRepresentationMultipartForm {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(sink.OperationType), "http") {
		return nil
	}

	return xerror.InvalidInput(xerror.Pipeline).Detail(
		"multipart_form sink input_contract is only supported for http operation_type",
	)
}

func outputContractToSchemaContract(
	name string,
	representation model.ContractRepresentation,
	rootShape model.ContractRootShape,
	contract *payload.Contract,
) *payload.Contract {
	if contract == nil {
		return nil
	}

	fields := make([]payload.ContractField, 0, len(contract.Fields))
	for _, field := range contract.Fields {
		fields = append(fields, payload.ContractField{
			Name:      field.Name,
			Type:      field.Type,
			Precision: field.Precision,
			Scale:     field.Scale,
			Required:  field.Required,
			Nullable:  field.Nullable,
		})
	}

	return &payload.Contract{
		Name:           name,
		Representation: representation,
		RootShape:      rootShape,
		Fields:         fields,
	}
}

func outputContractFacets(
	contract *payload.Contract,
	fallbackRepresentation model.ContractRepresentation,
	fallbackRootShape model.ContractRootShape,
) (model.ContractRepresentation, model.ContractRootShape) {
	if contract == nil {
		return fallbackRepresentation, fallbackRootShape
	}

	representation := fallbackRepresentation
	if contract.Representation != model.ContractRepresentationUnknown {
		representation = contract.Representation
	}

	rootShape := fallbackRootShape
	if contract.RootShape != model.ContractRootShapeUnknown {
		rootShape = contract.RootShape
	}

	return representation, rootShape
}

// producerContractFacets applies explicit contract facets only where the
// producer runtime does not already constrain the payload representation/shape.
func producerContractFacets(
	contract *payload.Contract,
	runtimeRepresentation model.ContractRepresentation,
	runtimeRootShape model.ContractRootShape,
) (model.ContractRepresentation, model.ContractRootShape) {
	representation, rootShape := outputContractFacets(
		contract,
		runtimeRepresentation,
		runtimeRootShape,
	)
	if runtimeRepresentation != model.ContractRepresentationUnknown {
		representation = runtimeRepresentation
		if runtimeRepresentation != model.ContractRepresentationJSON {
			rootShape = model.ContractRootShapeUnknown
		}
	}
	if runtimeRootShape != model.ContractRootShapeUnknown {
		rootShape = runtimeRootShape
	}

	return representation, rootShape
}

func outputKindToContractFacets(
	kind model.TransformOutputKind,
) (model.ContractRepresentation, model.ContractRootShape) {
	switch kind {
	case model.TransformOutputKindRows:
		return model.ContractRepresentationRows, model.ContractRootShapeUnknown
	case model.TransformOutputKindCSV:
		return model.ContractRepresentationCSV, model.ContractRootShapeUnknown
	case model.TransformOutputKindJSON:
		return model.ContractRepresentationJSON, model.ContractRootShapeArray
	default:
		return model.ContractRepresentationUnknown, model.ContractRootShapeUnknown
	}
}

func contractRepresentationToOutputKind(
	representation model.ContractRepresentation,
) model.TransformOutputKind {
	switch representation {
	case model.ContractRepresentationRows:
		return model.TransformOutputKindRows
	case model.ContractRepresentationCSV:
		return model.TransformOutputKindCSV
	case model.ContractRepresentationJSON:
		return model.TransformOutputKindJSON
	default:
		return model.TransformOutputKindUnknown
	}
}

func validateTransformInputAttachments(transform *pipelinespec.TransformSpec) error {
	if transform.InputContracts == nil {
		return nil
	}
	if len(transform.InputContracts) == 0 || transform.InputContract != nil || transform.InputContractRef != nil {
		return xerror.InvalidInput(xerror.Pipeline).Detail("input_contracts must be nonempty and mutually exclusive with the combined input contract/reference")
	}
	for _, input := range sortedTransformInputs(transform) {
		attachment, exists := transform.InputContracts[input.name]
		if !exists {
			continue
		}
		if attachment == nil || (attachment.Contract == nil && attachment.ContractRef == nil) {
			return xerror.InvalidInput(xerror.Pipeline).Detailf("input_contracts.%s requires a contract or contract_ref", input.name)
		}
		if err := validateContractDeclaration(attachment.Contract, attachment.ContractRef); err != nil {
			return xerror.Wrapf("input_contracts.%s: %w", input.name, err)
		}
		if ref := attachment.ContractRef; ref != nil && (ref.Representation == model.ContractRepresentationFile || ref.Representation == model.ContractRepresentationMultipartForm) {
			return xerror.InvalidInput(xerror.Pipeline).Detailf("input_contracts.%s contract_ref requires rows, json, or csv representation", input.name)
		}
		representation, root := outputContractFacets(attachment.Contract, model.ContractRepresentationRows, model.ContractRootShapeUnknown)
		if err := validateStepContract(attachment.Contract, false, representation, root); err != nil {
			return xerror.Wrapf("input_contracts.%s: %w", input.name, err)
		}
		if err := validateSQLTransformContractRepresentation(attachment.Contract); err != nil {
			return err
		}
	}
	for name := range transform.InputContracts {
		if _, exists := transform.Inputs[name]; !exists {
			return xerror.InvalidInput(xerror.Pipeline).Detailf("input_contracts.%s names an unknown transform input", name)
		}
	}
	return nil
}

// ValidateContractAttachment validates a saved-operation attachment using the same
// declaration rules as a pipeline port, without resolving a catalog reference.
func ValidateContractAttachment(contract *payload.Contract, ref *pipelinespec.ODCSContractRef) error {
	if err := validateContractDeclaration(contract, ref); err != nil {
		return err
	}
	if contract == nil {
		return nil
	}
	return validateStepContract(contract, true, contract.Representation, contract.RootShape)
}
