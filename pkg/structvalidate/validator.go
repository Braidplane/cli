package structvalidate

import (
	"sync"

	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"
	"github.com/Braidplane/cli/pkg/slug"
	"github.com/go-playground/validator/v10"
)

var defaultValidator = sync.OnceValue(New)

func Default() *validator.Validate { return defaultValidator() }

func New() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	if err := RegisterPipelineValidationTags(v); err != nil {
		panic(err)
	}
	return v
}

func validateSlug(fl validator.FieldLevel) bool {
	var value string
	switch v := fl.Field().Interface().(type) {
	case string:
		value = v
	case slug.Slug:
		value = v.String()
	default:
		return false
	}

	_, err := slug.New(value)
	return err == nil
}

// validateRunSourceType accepts the launch-supported run source categories.

func validateSinkIdempotencyClass(fl validator.FieldLevel) bool {
	value, ok := fl.Field().Interface().(pipelinespec.SinkIdempotencyClass)
	if !ok {
		return false
	}

	return value.IsASinkIdempotencyClass() && value != pipelinespec.SinkIdempotencyClassUnknown
}

// validateWishlistFocus rejects unknown wishlist qualification categories.
