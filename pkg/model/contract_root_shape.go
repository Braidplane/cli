//go:generate -command enumer go tool enumer
//go:generate enumer -type=ContractRootShape -json -text -linecomment -output=contract_root_shape_enumer.go
package model

// ContractRootShape defines the root JSON container shape when representation is json.
type ContractRootShape int

const (
	ContractRootShapeUnknown ContractRootShape = iota // unknown
	ContractRootShapeObject                           // object
	ContractRootShapeArray                            // array
)
