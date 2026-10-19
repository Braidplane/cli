package diff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Braidplane/cli/pkg/pipeline/payload"
	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"
)

const absentValue = "<none>"

// DiffResult is the structured output of a semantic pipeline comparison.
type DiffResult struct {
	HasChanges bool
	Metadata   []FieldChange
	Steps      []StepDiff
}

// FieldChange is a single field that changed between two pipeline versions.
type FieldChange struct {
	Field  string
	Before string
	After  string
}

// StepDiff describes what changed for a given step.
type StepDiff struct {
	Slug   string
	Kind   StepDiffKind
	Fields []FieldChange // populated for Modified steps only
}

// StepDiffKind describes the type of change for a step.
//
//go:generate -command enumer go tool enumer
//go:generate enumer -type=StepDiffKind -json -linecomment -output=diff_stepdiffkind_enumer.go
type StepDiffKind int

const (
	StepAdded    StepDiffKind = iota // added
	StepRemoved                      // removed
	StepModified                     // modified
)

// Diff computes the semantic difference between two pipeline specs.
// Both before and after must be non-nil. Steps are matched by slug.
func Diff(before, after *pipelinespec.Pipeline) *DiffResult {
	d := &DiffResult{}
	d.Metadata = diffMetadata(before.Metadata, after.Metadata)
	d.Steps = diffSteps(before.Steps, after.Steps)
	d.HasChanges = len(d.Metadata) > 0 || len(d.Steps) > 0
	return d
}

// diffMetadata extracts changed fields from pipeline metadata (slug, name).
func diffMetadata(before, after pipelinespec.Metadata) []FieldChange {
	var changes []FieldChange
	changes = appendIfChanged(changes, "metadata.slug", string(before.Slug), string(after.Slug))
	changes = appendIfChanged(changes, "metadata.name", before.Name, after.Name)
	return changes
}

// diffSteps compares step arrays and tracks added, removed, and modified steps by slug.
func diffSteps(before, after []pipelinespec.Step) []StepDiff {
	var diffs []StepDiff

	beforeBySlug := indexStepsBySlug(before)
	afterBySlug := indexStepsBySlug(after)

	// Removed steps — preserve original ordering from before.
	for _, b := range before {
		if _, exists := afterBySlug[string(b.Slug)]; !exists {
			diffs = append(diffs, StepDiff{Slug: string(b.Slug), Kind: StepRemoved})
		}
	}

	// Added or modified steps — preserve ordering from after.
	for _, a := range after {
		b, exists := beforeBySlug[string(a.Slug)]
		if !exists {
			diffs = append(diffs, StepDiff{Slug: string(a.Slug), Kind: StepAdded})
			continue
		}
		if fields := diffStep(b, a); len(fields) > 0 {
			diffs = append(
				diffs,
				StepDiff{Slug: string(a.Slug), Kind: StepModified, Fields: fields},
			)
		}
	}

	return diffs
}

// diffStep extracts changed fields within a single step.
func diffStep(before, after pipelinespec.Step) []FieldChange {
	prefix := fmt.Sprintf("step[%s]", string(before.Slug))
	var changes []FieldChange
	changes = appendIfChanged(changes, prefix+".kind", before.Kind, after.Kind)
	changes = appendIfChanged(changes, prefix+".name", before.Name, after.Name)
	if (before.Input == nil) != (after.Input == nil) {
		changes = appendIfChanged(
			changes,
			prefix+".input",
			formatPresence(before.Input),
			formatPresence(after.Input),
		)
	}
	if before.Input != nil && after.Input != nil {
		changes = appendIfChanged(changes, prefix+".input.output_contract_ref", formatDynamicValue(before.Input.OutputContractRef), formatDynamicValue(after.Input.OutputContractRef))
		changes = append(
			changes,
			diffOutputContractFields(
				prefix+".input.output_contract",
				before.Input.OutputContract,
				after.Input.OutputContract,
			)...)
	}
	if (before.Query == nil) != (after.Query == nil) {
		changes = appendIfChanged(
			changes,
			prefix+".query",
			formatPresence(before.Query),
			formatPresence(after.Query),
		)
	}
	if before.Query != nil && after.Query != nil {
		changes = appendIfChanged(changes, prefix+".query.data_source_ref",
			string(before.Query.DataSourceSlug), string(after.Query.DataSourceSlug))
		changes = appendIfChanged(changes, prefix+".query.operation_type",
			before.Query.OperationType, after.Query.OperationType)
		changes = appendIfChanged(changes, prefix+".query.operation_ref",
			string(before.Query.OperationRef), string(after.Query.OperationRef))
		changes = appendIfChanged(changes, prefix+".query.operation_inline",
			formatOperationInline(before.Query.OperationInline),
			formatOperationInline(after.Query.OperationInline))
		changes = appendIfChanged(changes, prefix+".query.incremental",
			formatIncrementalContract(before.Query.Incremental),
			formatIncrementalContract(after.Query.Incremental))
		changes = append(
			changes,
			diffOutputContractFields(
				prefix+".query.output_contract",
				before.Query.OutputContract,
				after.Query.OutputContract,
			)...)
	}
	if (before.Transform == nil) != (after.Transform == nil) {
		changes = appendIfChanged(
			changes,
			prefix+".transform",
			formatPresence(before.Transform),
			formatPresence(after.Transform),
		)
	}
	if before.Transform != nil && after.Transform != nil {
		names := make(map[string]bool)
		for name := range before.Transform.InputContracts {
			names[name] = true
		}
		for name := range after.Transform.InputContracts {
			names[name] = true
		}
		ordered := make([]string, 0, len(names))
		for name := range names {
			ordered = append(ordered, name)
		}
		sort.Strings(ordered)
		for _, name := range ordered {
			var beforeContract, afterContract *payload.Contract
			var beforeRef, afterRef *pipelinespec.ODCSContractRef
			if attachment := before.Transform.InputContracts[name]; attachment != nil {
				beforeContract, beforeRef = attachment.Contract, attachment.ContractRef
			}
			if attachment := after.Transform.InputContracts[name]; attachment != nil {
				afterContract, afterRef = attachment.Contract, attachment.ContractRef
			}
			field := prefix + ".transform.input_contracts." + name
			changes = append(changes, diffOutputContractFields(field+".contract", beforeContract, afterContract)...)
			changes = appendIfChanged(changes, field+".contract_ref", formatDynamicValue(beforeRef), formatDynamicValue(afterRef))
		}
		changes = appendIfChanged(changes, prefix+".transform.sql",
			before.Transform.SQL, after.Transform.SQL)
		changes = appendIfChanged(changes, prefix+".transform.output_kind",
			before.Transform.OutputKind.String(), after.Transform.OutputKind.String())
		changes = append(
			changes,
			diffOutputContractFields(
				prefix+".transform.input_contract",
				before.Transform.InputContract,
				after.Transform.InputContract,
			)...)
		changes = append(
			changes,
			diffOutputContractFields(
				prefix+".transform.output_contract",
				before.Transform.OutputContract,
				after.Transform.OutputContract,
			)...)
	}
	if (before.Sink == nil) != (after.Sink == nil) {
		changes = appendIfChanged(
			changes,
			prefix+".sink",
			formatPresence(before.Sink),
			formatPresence(after.Sink),
		)
	}
	if before.Sink != nil && after.Sink != nil {
		changes = append(changes, diffOutputContractFields(prefix+".sink.input_contract", before.Sink.InputContract, after.Sink.InputContract)...)
		changes = appendIfChanged(
			changes,
			prefix+".sink.data_source_ref",
			before.Sink.DataSourceSlug.String(),
			after.Sink.DataSourceSlug.String(),
		)
		changes = appendIfChanged(
			changes,
			prefix+".sink.operation_type",
			before.Sink.OperationType,
			after.Sink.OperationType,
		)
		changes = appendIfChanged(
			changes,
			prefix+".sink.operation_ref",
			before.Sink.OperationRef.String(),
			after.Sink.OperationRef.String(),
		)
		changes = appendIfChanged(
			changes,
			prefix+".sink.operation_inline",
			formatOperationInline(before.Sink.OperationInline),
			formatOperationInline(after.Sink.OperationInline),
		)
		changes = append(
			changes,
			diffMapFields(prefix+".sink.config", before.Sink.Config, after.Sink.Config)...,
		)
	}
	return changes
}

func formatOperationInline(value map[string]any) string {
	if len(value) == 0 {
		return ""
	}

	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}

	return string(data)
}

func formatIncrementalContract(value *pipelinespec.IncrementalSourceContract) string {
	if value == nil {
		return ""
	}

	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}

	return string(data)
}

func diffMapFields(prefix string, before, after map[string]any) []FieldChange {
	if len(before) == 0 && len(after) == 0 {
		return nil
	}

	keys := make(map[string]struct{}, len(before)+len(after))
	for key := range before {
		keys[key] = struct{}{}
	}
	for key := range after {
		keys[key] = struct{}{}
	}

	orderedKeys := make([]string, 0, len(keys))
	for key := range keys {
		orderedKeys = append(orderedKeys, key)
	}
	sort.Strings(orderedKeys)

	changes := make([]FieldChange, 0)
	for _, key := range orderedKeys {
		beforeValue, hasBefore := before[key]
		afterValue, hasAfter := after[key]

		beforeText := absentValue
		if hasBefore {
			beforeText = formatDynamicValue(beforeValue)
		}
		afterText := absentValue
		if hasAfter {
			afterText = formatDynamicValue(afterValue)
		}

		changes = appendIfChanged(changes, prefix+"."+key, beforeText, afterText)
	}

	return changes
}

func formatDynamicValue(value any) string {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return fmt.Sprintf("%v", value)
	}

	return string(bytes.TrimSpace(buf.Bytes()))
}

// diffOutputContractFields tracks contract facet and field-level changes.
func diffOutputContractFields(prefix string, before, after *payload.Contract) []FieldChange {
	if before == nil && after == nil {
		return nil
	}

	var changes []FieldChange
	changes = appendIfChanged(
		changes,
		prefix+".name",
		formatContractName(before),
		formatContractName(after),
	)
	changes = appendIfChanged(
		changes,
		prefix+".representation",
		formatContractRepresentation(before),
		formatContractRepresentation(after),
	)
	changes = appendIfChanged(
		changes,
		prefix+".root_shape",
		formatContractRootShape(before),
		formatContractRootShape(after),
	)

	beforeFields := indexOutputContractFields(before)
	afterFields := indexOutputContractFields(after)

	for _, beforeField := range orderedOutputContractFields(before) {
		afterField, exists := afterFields[beforeField.Name]
		if !exists {
			changes = append(changes, FieldChange{
				Field:  fmt.Sprintf("%s.%s", prefix, beforeField.Name),
				Before: formatOutputContractField(beforeField),
				After:  absentValue,
			})
			continue
		}
		changes = appendIfChanged(changes,
			fmt.Sprintf("%s.%s.type", prefix, beforeField.Name),
			beforeField.Type.String(),
			afterField.Type.String(),
		)
		changes = appendIfChanged(changes,
			fmt.Sprintf("%s.%s.required", prefix, beforeField.Name),
			fmt.Sprintf("%v", beforeField.Required),
			fmt.Sprintf("%v", afterField.Required),
		)
		changes = appendIfChanged(changes,
			fmt.Sprintf("%s.%s.nullable", prefix, beforeField.Name),
			fmt.Sprintf("%v", beforeField.Nullable),
			fmt.Sprintf("%v", afterField.Nullable),
		)
		changes = appendIfChanged(changes,
			fmt.Sprintf("%s.%s.validations", prefix, beforeField.Name),
			formatDynamicValue(beforeField.Validations),
			formatDynamicValue(afterField.Validations),
		)
	}

	for _, afterField := range orderedOutputContractFields(after) {
		if _, exists := beforeFields[afterField.Name]; exists {
			continue
		}
		changes = append(changes, FieldChange{
			Field:  fmt.Sprintf("%s.%s", prefix, afterField.Name),
			Before: absentValue,
			After:  formatOutputContractField(afterField),
		})
	}

	return changes
}

func formatContractName(contract *payload.Contract) string {
	if contract == nil {
		return absentValue
	}
	return contract.Name
}

func formatContractRepresentation(contract *payload.Contract) string {
	if contract == nil || contract.Representation == 0 {
		return absentValue
	}
	return contract.Representation.String()
}

func formatContractRootShape(contract *payload.Contract) string {
	if contract == nil || contract.RootShape == 0 {
		return absentValue
	}
	return contract.RootShape.String()
}

// indexOutputContractFields builds a name-indexed lookup map of fields within an output contract.

func indexOutputContractFields(contract *payload.Contract) map[string]payload.ContractField {
	if contract == nil {
		return map[string]payload.ContractField{}
	}

	fields := make(map[string]payload.ContractField, len(contract.Fields))
	for _, field := range contract.Fields {
		fields[field.Name] = field
	}
	return fields
}

// orderedOutputContractFields returns fields from a contract in their declaration order, or nil if
// contract is nil.
func orderedOutputContractFields(contract *payload.Contract) []payload.ContractField {
	if contract == nil {
		return nil
	}
	return contract.Fields
}

// formatOutputContractField returns a human-readable string representation of a contract field.
func formatOutputContractField(field payload.ContractField) string {
	return fmt.Sprintf("%s(required=%v,nullable=%v)", field.Type, field.Required, field.Nullable)
}

// formatPresence returns "<none>" if value is nil, or "present" otherwise, for use in before/after
// comparisons.
func formatPresence(value any) string {
	if value == nil {
		return absentValue
	}
	return "present"
}

// indexStepsBySlug builds a slug-indexed lookup map of steps for fast by-slug comparisons.
func indexStepsBySlug(steps []pipelinespec.Step) map[string]pipelinespec.Step {
	m := make(map[string]pipelinespec.Step, len(steps))
	for _, s := range steps {
		m[string(s.Slug)] = s
	}
	return m
}

// appendIfChanged appends a FieldChange to the slice only if before != after, otherwise returns the
// unchanged slice.
func appendIfChanged(changes []FieldChange, field, before, after string) []FieldChange {
	if before != after {
		return append(changes, FieldChange{Field: field, Before: before, After: after})
	}
	return changes
}
