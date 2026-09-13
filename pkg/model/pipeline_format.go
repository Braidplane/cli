//go:generate -command enumer go tool enumer
//go:generate enumer -type=PipelineFormat -json -text -linecomment -output=pipeline_format_enumer.go
package model

// PipelineFormat defines the serialization format for pipeline spec files.
type PipelineFormat int

const (
	PipelineFormatUnknown PipelineFormat = iota // unknown
	PipelineFormatYAML                          // yaml
	PipelineFormatJSON                          // json
)
