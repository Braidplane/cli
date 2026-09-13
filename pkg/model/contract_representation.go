//go:generate -command enumer go tool enumer
//go:generate enumer -type=ContractRepresentation -json -text -linecomment -output=contract_representation_enumer.go
package model

// ContractRepresentation defines the payload representation at a pipeline edge.
type ContractRepresentation int

const (
	ContractRepresentationUnknown       ContractRepresentation = iota // unknown
	ContractRepresentationRows                                        // rows
	ContractRepresentationJSON                                        // json
	ContractRepresentationCSV                                         // csv
	ContractRepresentationFile                                        // file
	ContractRepresentationMultipartForm                               // multipart_form
)
