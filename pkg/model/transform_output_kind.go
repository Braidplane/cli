//go:generate -command enumer go tool enumer
//go:generate enumer -type=TransformOutputKind -json -text -linecomment -output=transform_output_kind_enumer.go
package model

// TransformOutputKind defines supported output payload kinds for transforms.
type TransformOutputKind int

const (
	TransformOutputKindUnknown TransformOutputKind = iota // unknown
	TransformOutputKindRows                               // rows
	TransformOutputKindJSON                               // json
	TransformOutputKindCSV                                // csv
)
