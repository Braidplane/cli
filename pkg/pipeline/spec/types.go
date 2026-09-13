// Package spec owns the canonical pipeline specification shape and YAML/JSON
// file decoding.
package spec

import (
	"encoding/json"

	"gopkg.in/yaml.v3"

	"github.com/Braidplane/cli/pkg/model"
	"github.com/Braidplane/cli/pkg/pipeline/payload"
	"github.com/Braidplane/cli/pkg/slug"
	"github.com/Braidplane/cli/pkg/xerror"
)

// SinkIdempotencyClass declares the durable replay strategy for one sink
// operation. Pipeline authors must choose a strategy deliberately; the runtime
// uses it to decide whether a delivery can be repeated after an uncertain
// crash-after-effect outcome.
//
//go:generate -command enumer go tool enumer
//go:generate enumer -type=SinkIdempotencyClass -json -text -linecomment -output=sink_idempotency_class_enumer.go
type SinkIdempotencyClass int

const (
	SinkIdempotencyClassUnknown              SinkIdempotencyClass = iota // unknown
	SinkIdempotencyClassNaturallyIdempotent                              // naturally_idempotent
	SinkIdempotencyClassIdempotentWithKey                                // idempotent_with_key
	SinkIdempotencyClassDeduplicatedByLedger                             // deduplicated_by_ledger
	SinkIdempotencyClassUnsafe                                           // unsafe
)

// IsReplaySafe reports whether repeating the provider call after a durable
// attempt record is known to preserve the external effect.
func (class SinkIdempotencyClass) IsReplaySafe() bool {
	switch class {
	case SinkIdempotencyClassNaturallyIdempotent,
		SinkIdempotencyClassIdempotentWithKey,
		SinkIdempotencyClassDeduplicatedByLedger:
		return true
	default:
		return false
	}
}

// Pipeline is the top-level spec loaded from a pipeline .yaml or .json file.
type Pipeline struct {
	Metadata Metadata `json:"metadata" yaml:"metadata" validate:"required"`
	Steps    []Step   `json:"steps"    yaml:"steps"    validate:"min=1,dive"`
}

// StepInput declares the upstream step consumed by a transform or sink step.
type StepInput struct {
	FromStep slug.Slug `validate:"required,slug"`
}

// MarshalJSON emits the canonical scalar step input form.
func (input StepInput) MarshalJSON() ([]byte, error) {
	return json.Marshal(input.FromStep)
}

// UnmarshalJSON accepts the canonical scalar step input form.
func (input *StepInput) UnmarshalJSON(data []byte) error {
	var fromStep slug.Slug
	if err := json.Unmarshal(data, &fromStep); err != nil {
		return err
	}
	input.FromStep = fromStep
	return nil
}

// MarshalYAML emits the canonical scalar step input form.
func (input StepInput) MarshalYAML() (any, error) {
	return input.FromStep, nil
}

// UnmarshalYAML accepts the canonical scalar step input form.
func (input *StepInput) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return xerror.InvalidInput(xerror.Pipeline).Detail("step input must be a step slug")
	}
	var fromStep slug.Slug
	if err := value.Decode(&fromStep); err != nil {
		return err
	}
	input.FromStep = fromStep
	return nil
}

// SinkSpec configures pipeline output delivery for kind=sink steps.
type SinkSpec struct {
	Input            *StepInput           `json:"input,omitempty"            yaml:"input,omitempty"            validate:"omitempty"`
	InputContract    *payload.Contract    `json:"input_contract,omitempty"   yaml:"input_contract,omitempty"   validate:"-"`
	DataSourceSlug   slug.Slug            `json:"data_source_ref,omitempty"  yaml:"data_source_ref,omitempty"  validate:"omitempty,slug"`
	OperationType    string               `json:"operation_type,omitempty"   yaml:"operation_type,omitempty"`
	OperationRef     slug.Slug            `json:"operation_ref,omitempty"    yaml:"operation_ref,omitempty"    validate:"omitempty,slug"`
	OperationInline  map[string]any       `json:"operation_inline,omitempty" yaml:"operation_inline,omitempty" validate:"-"`
	Config           map[string]any       `json:"config,omitempty"           yaml:"config,omitempty"`
	IdempotencyClass SinkIdempotencyClass `json:"idempotency_class,omitempty" yaml:"idempotency_class,omitempty" validate:"sinkidempotencyclass"`
}

// Metadata holds the pipeline identity fields.
type Metadata struct {
	Slug slug.Slug `json:"slug" yaml:"slug" validate:"required,slug"`
	Name string    `json:"name" yaml:"name" validate:"required"`
}

// Step is a pipeline step. The Kind field determines which block is populated:
// kind=query populates Query; kind=transform populates Transform; kind=sink populates Sink.
type Step struct {
	Kind      string         `json:"kind"                yaml:"kind"                validate:"required"`
	Slug      slug.Slug      `json:"slug"                yaml:"slug"                validate:"required,slug"`
	Name      string         `json:"name,omitempty"      yaml:"name,omitempty"`
	Input     *InputSpec     `json:"input,omitempty"     yaml:"input,omitempty"`
	Query     *QueryRef      `json:"query,omitempty"     yaml:"query,omitempty"`
	Transform *TransformSpec `json:"transform,omitempty" yaml:"transform,omitempty"`
	Sink      *SinkSpec      `json:"sink,omitempty"      yaml:"sink,omitempty"`
}

// InputSpec declares the one run-scoped JSON payload accepted by a pipeline.
type InputSpec struct {
	OutputContract *payload.Contract `json:"output_contract" yaml:"output_contract" validate:"required"`
}

// TransformSpec configures the sql_v1 transform behavior.
type TransformSpec struct {
	SQL            string                    `json:"sql"                       yaml:"sql"                       validate:"required"`
	OutputKind     model.TransformOutputKind `json:"output_kind"               yaml:"output_kind"               validate:"required,transformoutputkind"`
	Inputs         map[string]StepInput      `json:"inputs"                    yaml:"inputs"                    validate:"-"`
	InputContract  *payload.Contract         `json:"input_contract,omitempty"  yaml:"input_contract,omitempty"  validate:"-"`
	OutputContract *payload.Contract         `json:"output_contract,omitempty" yaml:"output_contract,omitempty" validate:"-"`
}

// IncrementalSourceContract declares source-step checkpoint behavior.
type IncrementalSourceContract struct {
	CheckpointKind *model.IncrementalCheckpointKind `json:"checkpoint_kind,omitempty" yaml:"checkpoint_kind,omitempty"`
	Field          string                           `json:"field,omitempty"           yaml:"field,omitempty"`
	Cursor         string                           `json:"cursor,omitempty"          yaml:"cursor,omitempty"`
	LookbackWindow string                           `json:"lookback_window,omitempty" yaml:"lookback_window,omitempty"`
	Ordering       []string                         `json:"ordering,omitempty"        yaml:"ordering,omitempty"`
	DedupeKey      string                           `json:"dedupe_key,omitempty"      yaml:"dedupe_key,omitempty"`
}

// QueryRef identifies the operation to execute.
// Specs must define operation_type plus exactly one of operation_ref or operation_inline.
//
// Parameter values may be plain values or step output references of the form
// $steps.<step-slug>.<field> (e.g. $steps.fetch-users.rows). References are
// resolved at runtime by the Runner before the operation is executed.
type QueryRef struct {
	DataSourceSlug  slug.Slug                  `json:"data_source_ref"            yaml:"data_source_ref"            validate:"required,slug"`
	OperationType   string                     `json:"operation_type"             yaml:"operation_type"             validate:"required"`
	OperationRef    slug.Slug                  `json:"operation_ref,omitempty"    yaml:"operation_ref,omitempty"    validate:"omitempty,slug"`
	OperationInline map[string]any             `json:"operation_inline,omitempty" yaml:"operation_inline,omitempty" validate:"-"`
	OutputContract  *payload.Contract          `json:"output_contract,omitempty"  yaml:"output_contract,omitempty"  validate:"-"`
	Parameters      map[string]any             `json:"parameters,omitempty"       yaml:"parameters,omitempty"`
	Incremental     *IncrementalSourceContract `json:"incremental,omitempty"      yaml:"incremental,omitempty"      validate:"-"`
}
