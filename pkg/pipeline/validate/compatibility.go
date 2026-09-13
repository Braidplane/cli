package validate

import (
	"fmt"

	"github.com/Braidplane/cli/pkg/model"
	"github.com/Braidplane/cli/pkg/pipeline/payload"
	"github.com/Braidplane/cli/pkg/slug"
	"github.com/Braidplane/cli/pkg/xerror"
)

// EdgeCompatibilityResult captures the compatibility decision for one step-input edge.
type EdgeCompatibilityResult struct {
	EdgeSlug     string            `json:"edge_slug"`
	FromStepSlug slug.Slug         `json:"from_step_slug"`
	ToStepSlug   slug.Slug         `json:"to_step_slug"`
	Compatible   bool              `json:"compatible"`
	ReasonCode   ContractReason    `json:"reason_code,omitempty"`
	Expected     map[string]string `json:"expected,omitempty"`
	Actual       map[string]string `json:"actual,omitempty"`
}

// EdgeLink identifies a directional data-flow edge between two step slugs.
type EdgeLink struct {
	FromStep slug.Slug
	ToStep   slug.Slug
}

// EvaluateEdgeCompatibility compares producer and consumer contracts for one edge.
// Missing consumer facets are treated as unconstrained in this slice.
func EvaluateEdgeCompatibility(
	edge EdgeLink,
	producer *payload.Contract,
	consumer *payload.Contract,
) EdgeCompatibilityResult {
	result := EdgeCompatibilityResult{
		EdgeSlug:     fmt.Sprintf("%s->%s", edge.FromStep, edge.ToStep),
		FromStepSlug: edge.FromStep,
		ToStepSlug:   edge.ToStep,
		Compatible:   true,
	}

	if consumer == nil {
		return result
	}

	if producer == nil {
		return incompatibleEdge(
			result,
			ContractReasonInvalidDefinition,
			map[string]string{"contract": consumer.Name},
			map[string]string{"contract": "missing producer contract"},
		)
	}

	if err := validateCompatibilityContract(producer); err != nil {
		return incompatibleEdge(
			result,
			ContractReasonInvalidDefinition,
			map[string]string{"contract": producer.Name},
			map[string]string{"error": err.Error()},
		)
	}

	if err := validateCompatibilityContract(consumer); err != nil {
		return incompatibleEdge(
			result,
			ContractReasonInvalidDefinition,
			map[string]string{"contract": consumer.Name},
			map[string]string{"error": err.Error()},
		)
	}

	if consumer.Representation != model.ContractRepresentationUnknown {
		if producer.Representation != consumer.Representation {
			return incompatibleEdge(
				result,
				ContractReasonRepresentationMismatch,
				map[string]string{"representation": consumer.Representation.String()},
				map[string]string{"representation": producer.Representation.String()},
			)
		}
	}

	if consumer.Representation == model.ContractRepresentationJSON &&
		consumer.RootShape != model.ContractRootShapeUnknown &&
		producer.RootShape != consumer.RootShape {
		return incompatibleEdge(
			result,
			ContractReasonRootShapeMismatch,
			map[string]string{"root_shape": consumer.RootShape.String()},
			map[string]string{"root_shape": producer.RootShape.String()},
		)
	}

	producerFields := make(map[string]payload.ContractField, len(producer.Fields))
	for _, field := range producer.Fields {
		producerFields[field.Name] = field
	}

	for _, expectedField := range consumer.Fields {
		actualField, ok := producerFields[expectedField.Name]
		if !ok {
			if !expectedField.Required {
				continue
			}
			return incompatibleEdge(
				result,
				ContractReasonMissingRequiredField,
				map[string]string{"field": expectedField.Name},
				map[string]string{"field": "missing"},
			)
		}

		if !isCompatibleContractFieldType(actualField.Type, expectedField.Type) {
			return incompatibleEdge(
				result,
				ContractReasonTypeMismatch,
				map[string]string{
					"field": expectedField.Name,
					"type":  expectedField.Type.String(),
				},
				map[string]string{
					"field": expectedField.Name,
					"type":  actualField.Type.String(),
				},
			)
		}

		if producer.Representation != model.ContractRepresentationCSV &&
			actualField.Nullable && !expectedField.Nullable {
			return incompatibleEdge(
				result,
				ContractReasonNullabilityMismatch,
				map[string]string{
					"field":    expectedField.Name,
					"nullable": fmt.Sprintf("%t", expectedField.Nullable),
				},
				map[string]string{
					"field":    expectedField.Name,
					"nullable": fmt.Sprintf("%t", actualField.Nullable),
				},
			)
		}
	}

	return result
}

func incompatibleEdge(
	result EdgeCompatibilityResult,
	reasonCode ContractReason,
	expected map[string]string,
	actual map[string]string,
) EdgeCompatibilityResult {
	result.Compatible = false
	result.ReasonCode = reasonCode
	result.Expected = expected
	result.Actual = actual
	return result
}

func validateCompatibilityContract(contract *payload.Contract) error {
	if contract == nil {
		return nil
	}

	if contract.Representation != model.ContractRepresentationJSON &&
		contract.RootShape != model.ContractRootShapeUnknown {
		return xerror.Unexpected().Detail("root_shape requires json representation")
	}

	seen := make(map[string]struct{}, len(contract.Fields))
	for _, field := range contract.Fields {
		if _, ok := seen[field.Name]; ok {
			return xerror.Unexpected().Detailf("duplicate field %q", field.Name)
		}
		seen[field.Name] = struct{}{}
	}

	return nil
}

func isCompatibleContractFieldType(
	producer model.ContractFieldType,
	consumer model.ContractFieldType,
) bool {
	if producer == consumer {
		return true
	}

	if consumer == model.ContractFieldTypeValueNumber &&
		producer == model.ContractFieldTypeValueInteger {
		return true
	}

	return false
}
