package validate

//go:generate -command enumer go tool enumer
//go:generate enumer -type=ContractReason -json -text -linecomment -output=contract_reason_enumer.go

// ContractReason identifies why an edge contract comparison failed.
type ContractReason int

const (
	ContractReasonUnknown                ContractReason = iota // unknown
	ContractReasonRepresentationMismatch                       // contract_representation_mismatch
	ContractReasonRootShapeMismatch                            // contract_root_shape_mismatch
	ContractReasonMissingRequiredField                         // contract_missing_required_field
	ContractReasonTypeMismatch                                 // contract_type_mismatch
	ContractReasonNullabilityMismatch                          // contract_nullability_mismatch
	ContractReasonInvalidDefinition                            // contract_invalid_definition
	ContractReasonMissingContract                              // missing_contract
	ContractReasonFieldValidationFailed                        // contract_field_validation_failed
)
