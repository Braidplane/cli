package diff

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/model"
	"github.com/Braidplane/cli/pkg/must"
	"github.com/Braidplane/cli/pkg/pipeline/payload"
	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"
)

// baselineStep returns a valid pipelinespec.Step for reuse across tests.
func baselineStep(slug string) pipelinespec.Step {
	return pipelinespec.Step{
		Kind: "query",
		Slug: must.Slug(slug),
		Name: "Fetch " + slug,
		Query: &pipelinespec.QueryRef{
			DataSourceSlug: must.Slug("acme-postgres"),
			OperationType:  "sql",
			OperationRef:   must.Slug("list-" + slug),
		},
	}
}

func baselineTransformStep(slug string) pipelinespec.Step {
	return pipelinespec.Step{
		Kind: "transform",
		Slug: must.Slug(slug),
		Name: "Transform " + slug,
		Transform: &pipelinespec.TransformSpec{
			Inputs: map[string]pipelinespec.StepInput{"input": {FromStep: must.Slug("fetch-orders")}}, SQL: "SELECT id AS id FROM input",
			OutputKind: model.TransformOutputKindRows,
			OutputContract: &payload.Contract{Fields: []payload.ContractField{
				{Name: "id", Type: model.ContractFieldTypeValueInteger, Required: true},
			}},
		},
	}
}

// baselinePipeline returns a minimal valid pipelinespec.Pipeline for reuse across diff tests.
func baselinePipeline() *pipelinespec.Pipeline {
	return &pipelinespec.Pipeline{
		Metadata: pipelinespec.Metadata{Slug: must.Slug("export-orders"), Name: "Export Orders"},
		Steps:    []pipelinespec.Step{baselineStep("fetch-orders")},
	}
}

func TestSinkInputContractAcceptanceChanges(t *testing.T) {
	field := payload.ContractField{Name: "id", Type: model.ContractFieldTypeValueInteger}
	required, nullable := field, field
	required.Required = true
	nullable.Nullable = true
	for _, tc := range []struct {
		name          string
		before, after []payload.ContractField
		severity      Severity
	}{
		{"required addition", nil, []payload.ContractField{required}, SeverityBreaking},
		{"optional addition", nil, []payload.ContractField{field}, SeverityWarning},
		{"removal", []payload.ContractField{required}, nil, SeverityWarning},
		{"required tightening", []payload.ContractField{field}, []payload.ContractField{required}, SeverityBreaking},
		{"required relaxation", []payload.ContractField{required}, []payload.ContractField{field}, SeverityWarning},
		{"nullability tightening", []payload.ContractField{nullable}, []payload.ContractField{field}, SeverityBreaking},
		{"nullability relaxation", []payload.ContractField{field}, []payload.ContractField{nullable}, SeverityWarning},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pipeline := func(fields []payload.ContractField) *pipelinespec.Pipeline {
				return &pipelinespec.Pipeline{Steps: []pipelinespec.Step{{Slug: "store", Kind: "sink", Sink: &pipelinespec.SinkSpec{InputContract: &payload.Contract{Fields: fields}}}}}
			}
			result := Check(Diff(pipeline(tc.before), pipeline(tc.after)))
			require.Equal(t, tc.severity, result.Severity)
			require.Equal(t, tc.severity == SeverityBreaking, ShouldBlockDiff(pipeline(tc.after), result))
		})
	}
}

// --- Diff tests ---

func TestDiff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		base   func() *pipelinespec.Pipeline
		mutate func(after *pipelinespec.Pipeline)
		assert func(t *testing.T, d *DiffResult)
	}{
		{
			name:   "IdenticalPipelinesProduceNoChanges",
			mutate: func(_ *pipelinespec.Pipeline) {},
			assert: func(t *testing.T, d *DiffResult) {
				require.False(t, d.HasChanges)
				require.Empty(t, d.Metadata)
				require.Empty(t, d.Steps)
			},
		},
		{
			name: "MetadataSlugChange",
			mutate: func(p *pipelinespec.Pipeline) {
				p.Metadata.Slug = must.Slug("export-orders-v2")
			},
			assert: func(t *testing.T, d *DiffResult) {
				require.True(t, d.HasChanges)
				require.Len(t, d.Metadata, 1)
				require.Equal(t, "metadata.slug", d.Metadata[0].Field)
				require.Equal(t, "export-orders", d.Metadata[0].Before)
				require.Equal(t, "export-orders-v2", d.Metadata[0].After)
			},
		},
		{
			name: "MetadataNameChange",
			mutate: func(p *pipelinespec.Pipeline) {
				p.Metadata.Name = "New Name"
			},
			assert: func(t *testing.T, d *DiffResult) {
				require.True(t, d.HasChanges)
				require.Len(t, d.Metadata, 1)
				require.Equal(t, "metadata.name", d.Metadata[0].Field)
			},
		},
		{
			name: "StepRemoved",
			mutate: func(p *pipelinespec.Pipeline) {
				p.Steps = []pipelinespec.Step{}
			},
			assert: func(t *testing.T, d *DiffResult) {
				require.True(t, d.HasChanges)
				require.Len(t, d.Steps, 1)
				require.Equal(t, "fetch-orders", d.Steps[0].Slug)
				require.Equal(t, StepRemoved, d.Steps[0].Kind)
			},
		},
		{
			name: "StepAdded",
			mutate: func(p *pipelinespec.Pipeline) {
				p.Steps = append(p.Steps, baselineStep("normalize-orders"))
			},
			assert: func(t *testing.T, d *DiffResult) {
				require.True(t, d.HasChanges)
				require.Len(t, d.Steps, 1)
				require.Equal(t, "normalize-orders", d.Steps[0].Slug)
				require.Equal(t, StepAdded, d.Steps[0].Kind)
			},
		},
		{
			name: "StepOperationRefChanged",
			mutate: func(p *pipelinespec.Pipeline) {
				p.Steps[0].Query.OperationRef = "list-orders-v2"
			},
			assert: func(t *testing.T, d *DiffResult) {
				require.True(t, d.HasChanges)
				require.Len(t, d.Steps, 1)
				require.Equal(t, StepModified, d.Steps[0].Kind)
				require.Len(t, d.Steps[0].Fields, 1)
				require.Equal(
					t,
					"step[fetch-orders].query.operation_ref",
					d.Steps[0].Fields[0].Field,
				)
				require.Equal(t, "list-fetch-orders", d.Steps[0].Fields[0].Before)
				require.Equal(t, "list-orders-v2", d.Steps[0].Fields[0].After)
			},
		},
		{
			name: "InputContractChanged",
			base: func() *pipelinespec.Pipeline {
				return &pipelinespec.Pipeline{
					Metadata: pipelinespec.Metadata{Slug: "event-pipeline", Name: "Event pipeline"},
					Steps: []pipelinespec.Step{{
						Kind: "input",
						Slug: "event",
						Input: &pipelinespec.InputSpec{OutputContract: &payload.Contract{
							Representation: model.ContractRepresentationJSON,
							Fields:         []payload.ContractField{{Name: "id", Type: model.ContractFieldTypeValueString}},
						}},
					}},
				}
			},
			mutate: func(p *pipelinespec.Pipeline) {
				p.Steps[0].Input.OutputContract.Fields[0].Required = true
			},
			assert: func(t *testing.T, d *DiffResult) {
				require.Equal(
					t,
					"step[event].input.output_contract.id.required",
					d.Steps[0].Fields[0].Field,
				)
			},
		},
		{
			name: "InputContractNameChanged",
			base: func() *pipelinespec.Pipeline {
				return &pipelinespec.Pipeline{
					Metadata: pipelinespec.Metadata{Slug: "event-pipeline", Name: "Event pipeline"},
					Steps: []pipelinespec.Step{{
						Kind: "input", Slug: "event",
						Input: &pipelinespec.InputSpec{OutputContract: &payload.Contract{Name: "event"}},
					}},
				}
			},
			mutate: func(p *pipelinespec.Pipeline) {
				p.Steps[0].Input.OutputContract.Name = "request"
			},
			assert: func(t *testing.T, d *DiffResult) {
				require.Equal(t, "step[event].input.output_contract.name", d.Steps[0].Fields[0].Field)
			},
		},
		{
			name: "InputContractValidationChanged",
			base: func() *pipelinespec.Pipeline {
				return &pipelinespec.Pipeline{
					Metadata: pipelinespec.Metadata{Slug: "event-pipeline", Name: "Event pipeline"},
					Steps: []pipelinespec.Step{{
						Kind: "input", Slug: "event",
						Input: &pipelinespec.InputSpec{OutputContract: &payload.Contract{
							Name: "event", Fields: []payload.ContractField{{Name: "id", Type: model.ContractFieldTypeValueString}},
						}},
					}},
				}
			},
			mutate: func(p *pipelinespec.Pipeline) {
				p.Steps[0].Input.OutputContract.Fields[0].Validations = &payload.ContractFieldValidations{Format: "uuid"}
			},
			assert: func(t *testing.T, d *DiffResult) {
				require.Equal(t, "step[event].input.output_contract.id.validations", d.Steps[0].Fields[0].Field)
			},
		},
		{
			name: "StepDataSourceChanged",
			mutate: func(p *pipelinespec.Pipeline) {
				p.Steps[0].Query.DataSourceSlug = "acme-warehouse"
			},
			assert: func(t *testing.T, d *DiffResult) {
				require.True(t, d.HasChanges)
				require.Equal(t, StepModified, d.Steps[0].Kind)
				f := d.Steps[0].Fields[0]
				require.Equal(t, "step[fetch-orders].query.data_source_ref", f.Field)
				require.Equal(t, "acme-postgres", f.Before)
				require.Equal(t, "acme-warehouse", f.After)
			},
		},
		{
			name: "QueryIncrementalContractChanged",
			mutate: func(p *pipelinespec.Pipeline) {
				p.Steps[0].Query.Incremental = &pipelinespec.IncrementalSourceContract{
					CheckpointKind: incrementalCheckpointKindPtr(model.IncrementalCheckpointKindWatermark),
					Field:          "updated_at",
					LookbackWindow: "15m",
					Ordering:       []string{"updated_at", "id"},
					DedupeKey:      "id",
				}
			},
			assert: func(t *testing.T, d *DiffResult) {
				require.True(t, d.HasChanges)
				require.Len(t, d.Steps, 1)
				require.Equal(t, StepModified, d.Steps[0].Kind)
				require.Len(t, d.Steps[0].Fields, 1)
				f := d.Steps[0].Fields[0]
				require.Equal(t, "step[fetch-orders].query.incremental", f.Field)
				require.Empty(t, f.Before)
				require.JSONEq(
					t,
					`{
						"checkpoint_kind": "watermark",
						"field": "updated_at",
						"lookback_window": "15m",
						"ordering": ["updated_at", "id"],
						"dedupe_key": "id"
					}`,
					f.After,
				)
			},
		},
		{
			name: "SinkConfigFieldChanged",
			base: func() *pipelinespec.Pipeline {
				return &pipelinespec.Pipeline{
					Metadata: pipelinespec.Metadata{Slug: must.Slug("acme-users-sync"), Name: "ACME Users Sync"},
					Steps: []pipelinespec.Step{
						baselineStep("fetch-users"),
						{
							Kind: "sink",
							Slug: must.Slug("acme-users-sync"),
							Sink: &pipelinespec.SinkSpec{
								IdempotencyClass: pipelinespec.SinkIdempotencyClassIdempotentWithKey,
								Input:            &pipelinespec.StepInput{FromStep: must.Slug("fetch-users")},
								DataSourceSlug:   must.Slug("acme-mysql"),
								OperationType:    "sql",
								OperationRef:     must.Slug("upsert-users"),
								Config: map[string]any{
									"mode":  "upsert",
									"table": "users",
								},
							},
						},
					},
				}
			},
			mutate: func(p *pipelinespec.Pipeline) {
				p.Steps[1].Sink.Config["table"] = "accounts"
			},
			assert: func(t *testing.T, d *DiffResult) {
				require.True(t, d.HasChanges)
				require.Len(t, d.Steps, 1)
				require.Equal(t, StepModified, d.Steps[0].Kind)
				require.Len(t, d.Steps[0].Fields, 1)
				f := d.Steps[0].Fields[0]
				require.Equal(t, "step[acme-users-sync].sink.config.table", f.Field)
				require.Equal(t, `"users"`, f.Before)
				require.Equal(t, `"accounts"`, f.After)
			},
		},
		{
			name: "SinkConfigTypeChange",
			base: func() *pipelinespec.Pipeline {
				return &pipelinespec.Pipeline{
					Metadata: pipelinespec.Metadata{Slug: must.Slug("acme-users-sync"), Name: "ACME Users Sync"},
					Steps: []pipelinespec.Step{{
						Kind: "sink",
						Slug: must.Slug("acme-users-sync"),
						Sink: &pipelinespec.SinkSpec{
							IdempotencyClass: pipelinespec.SinkIdempotencyClassIdempotentWithKey,
							Config:           map[string]any{"limit": int64(42)},
						},
					}},
				}
			},
			mutate: func(p *pipelinespec.Pipeline) {
				p.Steps[0].Sink.Config["limit"] = "42"
			},
			assert: func(t *testing.T, d *DiffResult) {
				require.True(t, d.HasChanges)
				f := d.Steps[0].Fields[0]
				require.Equal(t, "step[acme-users-sync].sink.config.limit", f.Field)
				require.Equal(t, "42", f.Before)
				require.Equal(t, `"42"`, f.After)
			},
		},
		{
			name: "SinkConfigAbsentValueDoesNotCollideWithLiteralNoneString",
			base: func() *pipelinespec.Pipeline {
				return &pipelinespec.Pipeline{
					Metadata: pipelinespec.Metadata{Slug: must.Slug("acme-users-sync"), Name: "ACME Users Sync"},
					Steps: []pipelinespec.Step{{
						Kind: "sink",
						Slug: must.Slug("acme-users-sync"),
						Sink: &pipelinespec.SinkSpec{Config: map[string]any{}},
					}},
				}
			},
			mutate: func(p *pipelinespec.Pipeline) {
				p.Steps[0].Sink.Config["marker"] = absentValue
			},
			assert: func(t *testing.T, d *DiffResult) {
				require.True(t, d.HasChanges)
				f := d.Steps[0].Fields[0]
				require.Equal(t, "step[acme-users-sync].sink.config.marker", f.Field)
				require.Equal(t, absentValue, f.Before)
				require.Equal(t, `"<none>"`, f.After)
			},
		},
		{
			name: "StepNameChangeOnlyIsCosmetic",
			mutate: func(p *pipelinespec.Pipeline) {
				p.Steps[0].Name = "Renamed Step"
			},
			assert: func(t *testing.T, d *DiffResult) {
				require.True(t, d.HasChanges)
				require.Equal(t, StepModified, d.Steps[0].Kind)
				require.Equal(t, "step[fetch-orders].name", d.Steps[0].Fields[0].Field)
			},
		},
		{
			name: "MultipleChangesDetectedTogether",
			mutate: func(p *pipelinespec.Pipeline) {
				p.Metadata.Name = "Updated Name"
				p.Steps[0].Query.OperationRef = "new-query"
			},
			assert: func(t *testing.T, d *DiffResult) {
				require.True(t, d.HasChanges)
				require.Len(t, d.Metadata, 1)
				require.Len(t, d.Steps, 1)
				require.Equal(t, StepModified, d.Steps[0].Kind)
			},
		},
		{
			name: "TransformSQLChange",
			base: func() *pipelinespec.Pipeline {
				return &pipelinespec.Pipeline{
					Metadata: pipelinespec.Metadata{Slug: must.Slug("export-orders"), Name: "Export Orders"},
					Steps:    []pipelinespec.Step{baselineTransformStep("normalize-orders")},
				}
			},
			mutate: func(p *pipelinespec.Pipeline) {
				p.Steps[0].Transform.SQL = "SELECT email AS id FROM input"
			},
			assert: func(t *testing.T, d *DiffResult) {
				require.True(t, d.HasChanges)
				require.Len(t, d.Steps, 1)
				require.Equal(t, StepModified, d.Steps[0].Kind)
				require.Equal(t, "step[normalize-orders].transform.sql", d.Steps[0].Fields[0].Field)
			},
		},
		{
			name: "TransformOutputContractTypeChange",
			base: func() *pipelinespec.Pipeline {
				return &pipelinespec.Pipeline{
					Metadata: pipelinespec.Metadata{Slug: must.Slug("export-orders"), Name: "Export Orders"},
					Steps:    []pipelinespec.Step{baselineTransformStep("normalize-orders")},
				}
			},
			mutate: func(p *pipelinespec.Pipeline) {
				p.Steps[0].Transform.OutputContract.Fields[0].Type = model.ContractFieldTypeValueString
			},
			assert: func(t *testing.T, d *DiffResult) {
				require.True(t, d.HasChanges)
				require.Len(t, d.Steps, 1)
				require.Equal(
					t,
					"step[normalize-orders].transform.output_contract.id.type",
					d.Steps[0].Fields[0].Field,
				)
				require.Equal(t, "integer", d.Steps[0].Fields[0].Before)
				require.Equal(t, "string", d.Steps[0].Fields[0].After)
			},
		},
		{
			name: "TransformOutputContractFieldAdded",
			base: func() *pipelinespec.Pipeline {
				return &pipelinespec.Pipeline{
					Metadata: pipelinespec.Metadata{Slug: must.Slug("export-orders"), Name: "Export Orders"},
					Steps:    []pipelinespec.Step{baselineTransformStep("normalize-orders")},
				}
			},
			mutate: func(p *pipelinespec.Pipeline) {
				p.Steps[0].Transform.OutputContract.Fields = append(
					p.Steps[0].Transform.OutputContract.Fields,
					payload.ContractField{
						Name:     "email",
						Type:     model.ContractFieldTypeValueString,
						Required: false,
					},
				)
			},
			assert: func(t *testing.T, d *DiffResult) {
				require.True(t, d.HasChanges)
				require.Len(t, d.Steps, 1)
				require.Equal(
					t,
					"step[normalize-orders].transform.output_contract.email",
					d.Steps[0].Fields[0].Field,
				)
				require.Equal(t, "<none>", d.Steps[0].Fields[0].Before)
				require.Equal(t, "string(required=false,nullable=false)", d.Steps[0].Fields[0].After)
			},
		},
		{
			name: "TransformInputContractTypeChange",
			base: func() *pipelinespec.Pipeline {
				step := baselineTransformStep("normalize-orders")
				step.Transform.InputContract = &payload.Contract{Fields: []payload.ContractField{
					{
						Name:     "id",
						Type:     model.ContractFieldTypeValueInteger,
						Required: true,
					},
				}}
				return &pipelinespec.Pipeline{
					Metadata: pipelinespec.Metadata{Slug: must.Slug("export-orders"), Name: "Export Orders"},
					Steps:    []pipelinespec.Step{step},
				}
			},
			mutate: func(p *pipelinespec.Pipeline) {
				p.Steps[0].Transform.InputContract.Fields[0].Type = model.ContractFieldTypeValueString
			},
			assert: func(t *testing.T, d *DiffResult) {
				require.True(t, d.HasChanges)
				require.Len(t, d.Steps, 1)
				require.Equal(
					t,
					"step[normalize-orders].transform.input_contract.id.type",
					d.Steps[0].Fields[0].Field,
				)
				require.Equal(t, "integer", d.Steps[0].Fields[0].Before)
				require.Equal(t, "string", d.Steps[0].Fields[0].After)
			},
		},
		{
			name: "QueryOutputContractTypeChange",
			base: func() *pipelinespec.Pipeline {
				return &pipelinespec.Pipeline{
					Metadata: pipelinespec.Metadata{Slug: must.Slug("export-orders"), Name: "Export Orders"},
					Steps: []pipelinespec.Step{{
						Kind: "query",
						Slug: must.Slug("fetch-orders"),
						Name: "Fetch orders",
						Query: &pipelinespec.QueryRef{
							DataSourceSlug: must.Slug("acme-postgres"),
							OperationType:  "sql",
							OperationRef:   must.Slug("list-orders"),
							OutputContract: &payload.Contract{Fields: []payload.ContractField{{
								Name:     "id",
								Type:     model.ContractFieldTypeValueInteger,
								Required: true,
							}}},
						},
					}},
				}
			},
			mutate: func(p *pipelinespec.Pipeline) {
				p.Steps[0].Query.OutputContract.Fields[0].Type = model.ContractFieldTypeValueString
			},
			assert: func(t *testing.T, d *DiffResult) {
				require.True(t, d.HasChanges)
				require.Len(t, d.Steps, 1)
				require.Equal(
					t,
					"step[fetch-orders].query.output_contract.id.type",
					d.Steps[0].Fields[0].Field,
				)
				require.Equal(t, "integer", d.Steps[0].Fields[0].Before)
				require.Equal(t, "string", d.Steps[0].Fields[0].After)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			beforeFactory := baselinePipeline
			if tc.base != nil {
				beforeFactory = tc.base
			}
			before := beforeFactory()
			after := beforeFactory()
			tc.mutate(after)

			d := Diff(before, after)
			tc.assert(t, d)
		})
	}
}

func incrementalCheckpointKindPtr(
	value model.IncrementalCheckpointKind,
) *model.IncrementalCheckpointKind {
	return &value
}

// --- Check tests ---

func TestCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		diff         *DiffResult
		wantSeverity Severity
		wantMessages []string
	}{
		{
			name:         "NoChangesIsSafe",
			diff:         &DiffResult{HasChanges: false},
			wantSeverity: SeveritySafe,
			wantMessages: nil,
		},
		{
			name: "SlugChangeIsBreaking",
			diff: &DiffResult{
				HasChanges: true,
				Metadata:   []FieldChange{{Field: "metadata.slug", Before: "old", After: "new"}},
			},
			wantSeverity: SeverityBreaking,
			wantMessages: []string{"pipeline slug changed"},
		},
		{
			name: "NameChangeIsSafe",
			diff: &DiffResult{
				HasChanges: true,
				Metadata:   []FieldChange{{Field: "metadata.name", Before: "Old", After: "New"}},
			},
			wantSeverity: SeveritySafe,
			wantMessages: []string{"pipeline name changed"},
		},
		{
			name: "StepRemovedIsBreaking",
			diff: &DiffResult{
				HasChanges: true,
				Steps:      []StepDiff{{Slug: "fetch-orders", Kind: StepRemoved}},
			},
			wantSeverity: SeverityBreaking,
			wantMessages: []string{`step "fetch-orders" was removed`},
		},
		{
			name: "StepAddedIsSafe",
			diff: &DiffResult{
				HasChanges: true,
				Steps:      []StepDiff{{Slug: "normalize-orders", Kind: StepAdded}},
			},
			wantSeverity: SeveritySafe,
			wantMessages: []string{`step "normalize-orders" was added`},
		},
		{
			name: "OperationRefChangeIsWarning",
			diff: &DiffResult{
				HasChanges: true,
				Steps: []StepDiff{{
					Slug: "fetch-orders",
					Kind: StepModified,
					Fields: []FieldChange{{
						Field:  "step[fetch-orders].query.operation_ref",
						Before: "orders-v1",
						After:  "orders-v2",
					}},
				}},
			},
			wantSeverity: SeverityWarning,
			wantMessages: []string{"query.operation_ref changed"},
		},
		{
			name: "DataSourceChangeIsWarning",
			diff: &DiffResult{
				HasChanges: true,
				Steps: []StepDiff{{
					Slug: "fetch-orders",
					Kind: StepModified,
					Fields: []FieldChange{{
						Field:  "step[fetch-orders].query.data_source_ref",
						Before: "acme-postgres",
						After:  "acme-warehouse",
					}},
				}},
			},
			wantSeverity: SeverityWarning,
			wantMessages: []string{"data_source_ref changed"},
		},
		{
			name: "StepNameChangeIsSafe",
			diff: &DiffResult{
				HasChanges: true,
				Steps: []StepDiff{{
					Slug: "fetch-orders",
					Kind: StepModified,
					Fields: []FieldChange{{
						Field:  "step[fetch-orders].name",
						Before: "Old Name",
						After:  "New Name",
					}},
				}},
			},
			wantSeverity: SeveritySafe,
			wantMessages: []string{".name changed"},
		},
		{
			name: "HighestSeverityWins",
			diff: &DiffResult{
				HasChanges: true,
				Steps: []StepDiff{
					{Slug: "safe-step", Kind: StepAdded},
					{Slug: "gone-step", Kind: StepRemoved},
				},
			},
			wantSeverity: SeverityBreaking,
			wantMessages: []string{`step "safe-step" was added`, `step "gone-step" was removed`},
		},
		{
			name: "TransformSQLChangeIsWarning",
			diff: &DiffResult{
				HasChanges: true,
				Steps: []StepDiff{{
					Slug: "normalize-orders",
					Kind: StepModified,
					Fields: []FieldChange{{
						Field:  "step[normalize-orders].transform.sql",
						Before: "SELECT id AS id FROM input",
						After:  "SELECT email AS id FROM input",
					}},
				}},
			},
			wantSeverity: SeverityWarning,
			wantMessages: []string{"transform.sql changed"},
		},
		{
			name: "TransformOutputKindChangeIsWarning",
			diff: &DiffResult{
				HasChanges: true,
				Steps: []StepDiff{{
					Slug: "normalize-orders",
					Kind: StepModified,
					Fields: []FieldChange{{
						Field:  "step[normalize-orders].transform.output_kind",
						Before: "rows",
						After:  "document",
					}},
				}},
			},
			wantSeverity: SeverityWarning,
			wantMessages: []string{"transform.output_kind changed"},
		},
		{
			name: "SinkConfigChangeIsWarning",
			diff: &DiffResult{
				HasChanges: true,
				Steps: []StepDiff{{
					Slug: "acme-users-sync",
					Kind: StepModified,
					Fields: []FieldChange{{
						Field:  "step[acme-users-sync].sink.config.table",
						Before: "users",
						After:  "accounts",
					}},
				}},
			},
			wantSeverity: SeverityWarning,
			wantMessages: []string{"sink.config.table changed"},
		},
		{
			name: "TransformOutputContractTypeChangeIsBreaking",
			diff: &DiffResult{
				HasChanges: true,
				Steps: []StepDiff{{
					Slug: "normalize-orders",
					Kind: StepModified,
					Fields: []FieldChange{{
						Field:  "step[normalize-orders].transform.output_contract.id.type",
						Before: "integer",
						After:  "string",
					}},
				}},
			},
			wantSeverity: SeverityBreaking,
			wantMessages: []string{"output_contract.id.type changed"},
		},
		{
			name: "InputContractNameChangeIsWarning",
			diff: &DiffResult{
				HasChanges: true,
				Steps: []StepDiff{{
					Slug: "event", Kind: StepModified,
					Fields: []FieldChange{{Field: "step[event].input.output_contract.name", Before: "event", After: "request"}},
				}},
			},
			wantSeverity: SeverityWarning,
			wantMessages: []string{"output_contract.name changed"},
		},
		{
			name: "InputContractValidationChangeIsWarning",
			diff: &DiffResult{
				HasChanges: true,
				Steps: []StepDiff{{
					Slug: "event", Kind: StepModified,
					Fields: []FieldChange{{Field: "step[event].input.output_contract.id.validations", Before: "null", After: `{"format":"uuid"}`}},
				}},
			},
			wantSeverity: SeverityWarning,
			wantMessages: []string{"output_contract.id.validations changed"},
		},
		{
			name: "TransformOutputContractFieldAddedIsWarning",
			diff: &DiffResult{
				HasChanges: true,
				Steps: []StepDiff{{
					Slug: "normalize-orders",
					Kind: StepModified,
					Fields: []FieldChange{{
						Field:  "step[normalize-orders].transform.output_contract.email",
						Before: "<none>",
						After:  "string(required=false,nullable=false)",
					}},
				}},
			},
			wantSeverity: SeverityWarning,
			wantMessages: []string{"output_contract.email changed"},
		},
		{
			name: "TransformOutputContractFieldRemovedIsBreaking",
			diff: &DiffResult{
				HasChanges: true,
				Steps: []StepDiff{{
					Slug: "normalize-orders",
					Kind: StepModified,
					Fields: []FieldChange{{
						Field:  "step[normalize-orders].transform.output_contract.email",
						Before: "string(required=false,nullable=false)",
						After:  "<none>",
					}},
				}},
			},
			wantSeverity: SeverityBreaking,
			wantMessages: []string{"output_contract.email changed"},
		},
		{
			name: "TransformOutputContractNullableLoosenedIsBreaking",
			diff: &DiffResult{
				HasChanges: true,
				Steps: []StepDiff{{
					Slug: "normalize-orders",
					Kind: StepModified,
					Fields: []FieldChange{{
						Field:  "step[normalize-orders].transform.output_contract.email.nullable",
						Before: "false",
						After:  "true",
					}},
				}},
			},
			wantSeverity: SeverityBreaking,
			wantMessages: []string{"output_contract.email.nullable changed"},
		},
		{
			name: "TransformOutputContractNullableTightenedIsWarning",
			diff: &DiffResult{
				HasChanges: true,
				Steps: []StepDiff{{
					Slug: "normalize-orders",
					Kind: StepModified,
					Fields: []FieldChange{{
						Field:  "step[normalize-orders].transform.output_contract.email.nullable",
						Before: "true",
						After:  "false",
					}},
				}},
			},
			wantSeverity: SeverityWarning,
			wantMessages: []string{"output_contract.email.nullable changed"},
		},
		{
			name: "QueryOutputContractRepresentationChangeIsBreaking",
			diff: &DiffResult{
				HasChanges: true,
				Steps: []StepDiff{{
					Slug: "fetch-orders",
					Kind: StepModified,
					Fields: []FieldChange{{
						Field:  "step[fetch-orders].query.output_contract.representation",
						Before: "rows",
						After:  "json",
					}},
				}},
			},
			wantSeverity: SeverityBreaking,
			wantMessages: []string{"output_contract.representation changed"},
		},
		{
			name: "QueryOutputContractRootShapeAddedIsBreaking",
			diff: &DiffResult{
				HasChanges: true,
				Steps: []StepDiff{{
					Slug: "fetch-orders",
					Kind: StepModified,
					Fields: []FieldChange{{
						Field:  "step[fetch-orders].query.output_contract.root_shape",
						Before: "<none>",
						After:  "array",
					}},
				}},
			},
			wantSeverity: SeverityBreaking,
			wantMessages: []string{"output_contract.root_shape changed"},
		},
		{
			name: "TransformInputContractTypeChangeIsBreaking",
			diff: &DiffResult{
				HasChanges: true,
				Steps: []StepDiff{{
					Slug: "normalize-orders",
					Kind: StepModified,
					Fields: []FieldChange{{
						Field:  "step[normalize-orders].transform.input_contract.id.type",
						Before: "integer",
						After:  "string",
					}},
				}},
			},
			wantSeverity: SeverityBreaking,
			wantMessages: []string{"input_contract.id.type changed"},
		},
		{
			name: "QueryOutputContractTypeChangeIsBreaking",
			diff: &DiffResult{
				HasChanges: true,
				Steps: []StepDiff{{
					Slug: "fetch-orders",
					Kind: StepModified,
					Fields: []FieldChange{{
						Field:  "step[fetch-orders].query.output_contract.id.type",
						Before: "integer",
						After:  "string",
					}},
				}},
			},
			wantSeverity: SeverityBreaking,
			wantMessages: []string{"output_contract.id.type changed"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := Check(tc.diff)

			require.Equal(t, tc.wantSeverity, result.Severity)
			require.Len(t, result.Findings, len(tc.wantMessages))
			for i, msg := range tc.wantMessages {
				require.Contains(t, result.Findings[i].Message, msg)
			}
		})
	}
}

// --- Format tests ---

func TestCheckResult_Format(t *testing.T) {
	t.Parallel()

	t.Run("NoChangesOutputsSafeMessage", func(t *testing.T) {
		t.Parallel()

		r := Check(&DiffResult{HasChanges: false})
		out := r.Format()
		require.Contains(t, out, "safe")
		require.Contains(t, out, "no changes detected")
	})

	t.Run("FindingsListedWithSeverityPrefix", func(t *testing.T) {
		t.Parallel()

		r := Check(&DiffResult{
			HasChanges: true,
			Steps:      []StepDiff{{Slug: "gone", Kind: StepRemoved}},
		})
		out := r.Format()
		require.Contains(t, out, "severity: breaking")
		require.Contains(t, out, "[breaking")
		require.Contains(t, out, `step "gone" was removed`)
	})
}

func TestShouldBlockDiff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		after     *pipelinespec.Pipeline
		result    *CheckResult
		wantBlock bool
	}{
		{
			name:      "NilResultDoesNotBlock",
			after:     baselinePipeline(),
			result:    nil,
			wantBlock: false,
		},
		{
			name:      "DefaultModeBlocksBreaking",
			after:     baselinePipeline(),
			result:    &CheckResult{Severity: SeverityBreaking},
			wantBlock: true,
		},
		{
			name:      "UnknownModeDefaultsToStrictAndBlocksBreaking",
			after:     &pipelinespec.Pipeline{},
			result:    &CheckResult{Severity: SeverityBreaking},
			wantBlock: true,
		},
		{
			name:      "StrictModeAllowsWarning",
			after:     &pipelinespec.Pipeline{},
			result:    &CheckResult{Severity: SeverityWarning},
			wantBlock: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			blocked := ShouldBlockDiff(tc.after, tc.result)
			require.Equal(t, tc.wantBlock, blocked)
		})
	}
}

func TestCompositionReferenceChangesAreNotSafe(t *testing.T) {
	ref := func(revision string) *pipelinespec.ODCSContractRef {
		if revision == "" {
			return nil
		}
		return &pipelinespec.ODCSContractRef{ContractSlug: "orders", RevisionSlug: must.Slug(revision), SchemaObjectID: "row"}
	}
	for _, port := range []string{"event", "named"} {
		for _, revisions := range [][2]string{{"", "v1"}, {"v1", ""}, {"v1", "v2"}} {
			t.Run(port+"/"+revisions[0]+"/"+revisions[1], func(t *testing.T) {
				p := func(revision string) *pipelinespec.Pipeline {
					step := pipelinespec.Step{Slug: "event", Kind: "input", Input: &pipelinespec.InputSpec{OutputContractRef: ref(revision)}}
					if port == "named" {
						step = pipelinespec.Step{Slug: "audit", Kind: "transform", Transform: &pipelinespec.TransformSpec{InputContracts: map[string]*pipelinespec.InputContractAttachment{"orders": {ContractRef: ref(revision)}}}}
					}
					return &pipelinespec.Pipeline{Steps: []pipelinespec.Step{step}}
				}
				check := Check(Diff(p(revisions[0]), p(revisions[1])))
				require.Equal(t, SeverityBreaking, check.Severity)
			})
		}
	}
	before := &pipelinespec.Pipeline{Steps: []pipelinespec.Step{{Slug: "audit", Kind: "transform", Transform: &pipelinespec.TransformSpec{InputContracts: map[string]*pipelinespec.InputContractAttachment{"orders": {Contract: &payload.Contract{Name: "before", Fields: []payload.ContractField{{Name: "id", Type: model.ContractFieldTypeValueInteger}}}}}}}}}
	after := &pipelinespec.Pipeline{Steps: []pipelinespec.Step{{Slug: "audit", Kind: "transform", Transform: &pipelinespec.TransformSpec{InputContracts: map[string]*pipelinespec.InputContractAttachment{"orders": {Contract: &payload.Contract{Name: "after", Fields: []payload.ContractField{{Name: "id", Type: model.ContractFieldTypeValueInteger}}}}}}}}}
	require.Equal(t, SeverityWarning, Check(Diff(before, after)).Severity)
}

func TestDecimalPrecisionAndScaleParticipateInTypeImpact(t *testing.T) {
	precision, scale := 18, 2
	before := &payload.Contract{Fields: []payload.ContractField{{Name: "price", Type: model.ContractFieldTypeValueDecimal, Precision: &precision, Scale: &scale}}}
	for _, tc := range []struct{ precision, scale int }{{20, 2}, {18, 4}} {
		after := payload.CloneContract(before)
		after.Fields[0].Precision, after.Fields[0].Scale = &tc.precision, &tc.scale
		changes := diffOutputContractFields("step[fetch].query.output_contract", before, after)
		require.Len(t, changes, 1)
		require.Equal(t, "step[fetch].query.output_contract.price.type", changes[0].Field)
		require.Equal(t, "decimal(18,2)", changes[0].Before)
		require.Equal(t, SeverityBreaking, severityForStepChange(changes[0]))
	}
}
