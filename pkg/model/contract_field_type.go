//go:generate -command enumer go tool enumer
//go:generate enumer -type=ContractFieldType -json -text -linecomment -output=contract_field_type_enumer.go
package model

// ContractFieldType defines supported schema field types for step contracts.
type ContractFieldType int

const (
	ContractFieldTypeUnknown       ContractFieldType = iota // unknown
	ContractFieldTypeValueString                            // string
	ContractFieldTypeValueInteger                           // integer
	ContractFieldTypeValueNumber                            // number
	ContractFieldTypeValueBoolean                           // boolean
	ContractFieldTypeValueObject                            // object
	ContractFieldTypeValueArray                             // array
	ContractFieldTypeValueDatetime                          // datetime
	ContractFieldTypeValueDecimal                           // decimal
)
