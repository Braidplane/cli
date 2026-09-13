//go:generate -command enumer go tool enumer
//go:generate enumer -type=ParameterDataType -transform=snake -output=query_parameter_data_type_string.go -text -json -linecomment
package model

// ParameterDataType is the data type of a query parameter.
type ParameterDataType int

const (
	ParameterDataTypeUnknown ParameterDataType = iota // unknown
	ParameterDataTypeText                             // text
	ParameterDataTypeInteger                          // integer
	ParameterDataTypeBoolean                          // boolean
	ParameterDataTypeDate                             // date
	ParameterDataTypeUUID                             // uuid
)
