//go:generate -command enumer go tool enumer
//go:generate enumer -type=RuntimeVariableValueType -json -text -linecomment -output=runtime_variable_value_type_enumer.go
package model

import (
	"time"

	"github.com/Braidplane/cli/pkg/slug"
)

type RuntimeVariableValueType int

const (
	RuntimeVariableValueTypeUnknown RuntimeVariableValueType = iota // unknown
	RuntimeVariableValueTypeText                                    // text
	RuntimeVariableValueTypeInteger                                 // integer
	RuntimeVariableValueTypeBoolean                                 // boolean
	RuntimeVariableValueTypeDate                                    // date
	RuntimeVariableValueTypeUUID                                    // uuid
)

type RuntimeVariable struct {
	EnvironmentSlug slug.Slug
	Key             string
	ValueType       RuntimeVariableValueType
	Value           string
	Description     *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
