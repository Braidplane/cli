//go:generate -command enumer go tool enumer
//go:generate enumer -type=IncrementalCheckpointKind -json -text -linecomment -output=incremental_contract_enumer.go
package model

// IncrementalCheckpointKind defines the source checkpoint shape.
type IncrementalCheckpointKind int

const (
	IncrementalCheckpointKindUnknown   IncrementalCheckpointKind = iota // unknown
	IncrementalCheckpointKindWatermark                                  // watermark
	IncrementalCheckpointKindCursor                                     // cursor
)
