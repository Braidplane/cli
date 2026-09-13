package structvalidate

import (
	"github.com/go-playground/validator/v10"

	"github.com/Braidplane/cli/pkg/model"
	"github.com/Braidplane/cli/pkg/xerror"
)

// RegisterPipelineValidationTags registers pipeline-specific validator tags
// on the provided validator instance.
func RegisterPipelineValidationTags(v *validator.Validate) error {
	if v == nil {
		return nil
	}

	err := v.RegisterValidation("slug", validateSlug)
	if err != nil {
		return xerror.Unexpected().With(err)
	}
	err = v.RegisterValidation("sinkidempotencyclass", validateSinkIdempotencyClass)
	if err != nil {
		return xerror.Unexpected().With(err)
	}
	err = v.RegisterValidation("contractfieldtype", validateContractFieldType)
	if err != nil {
		return xerror.Unexpected().With(err)
	}

	err = v.RegisterValidation("contractrepresentation", validateContractRepresentation)
	if err != nil {
		return xerror.Unexpected().With(err)
	}

	err = v.RegisterValidation("contractrootshape", validateContractRootShape)
	if err != nil {
		return xerror.Unexpected().With(err)
	}

	err = v.RegisterValidation("transformoutputkind", validateTransformOutputKind)
	if err != nil {
		return xerror.Unexpected().With(err)
	}

	err = v.RegisterValidation("sinkbackend", validateSinkBackend)
	if err != nil {
		return xerror.Unexpected().With(err)
	}

	err = v.RegisterValidation("pipelineformat", validatePipelineFormat)
	if err != nil {
		return xerror.Unexpected().With(err)
	}

	return nil
}

func validateContractFieldType(fl validator.FieldLevel) bool {
	value, ok := fl.Field().Interface().(model.ContractFieldType)
	if !ok {
		return false
	}

	return value.IsAContractFieldType() && value != model.ContractFieldTypeUnknown
}

func validateContractRepresentation(fl validator.FieldLevel) bool {
	value, ok := fl.Field().Interface().(model.ContractRepresentation)
	if !ok {
		return false
	}

	return value.IsAContractRepresentation() && value != model.ContractRepresentationUnknown
}

func validateContractRootShape(fl validator.FieldLevel) bool {
	value, ok := fl.Field().Interface().(model.ContractRootShape)
	if !ok {
		return false
	}

	return value.IsAContractRootShape() && value != model.ContractRootShapeUnknown
}

func validateTransformOutputKind(fl validator.FieldLevel) bool {
	value, ok := fl.Field().Interface().(model.TransformOutputKind)
	if !ok {
		return false
	}

	return ValidTransformOutputKind(value)
}

// ValidTransformOutputKind reports whether value is a configured transform output kind.
func ValidTransformOutputKind(value model.TransformOutputKind) bool {
	return value.IsATransformOutputKind() && value != model.TransformOutputKindUnknown
}

func validateSinkBackend(fl validator.FieldLevel) bool {
	value, ok := fl.Field().Interface().(model.SinkBackend)
	if !ok {
		return false
	}

	return ValidSinkBackend(value)
}

// ValidSinkBackend reports whether value is a configured sink backend.
func ValidSinkBackend(value model.SinkBackend) bool {
	return value.IsASinkBackend() && value != model.SinkBackendUnknown
}

func validatePipelineFormat(fl validator.FieldLevel) bool {
	value, ok := fl.Field().Interface().(model.PipelineFormat)
	if !ok {
		return false
	}

	return value.IsAPipelineFormat() && value != model.PipelineFormatUnknown
}
