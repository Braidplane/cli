package validate

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/model"
	"github.com/Braidplane/cli/pkg/pipeline/payload"
	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"
)

func TestLoadQueryIncrementalSourceContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		data     string
		assertFn func(t *testing.T, query *pipelinespec.QueryRef)
	}{
		{
			name: "ValidSQLWatermarkContract",
			path: "pipeline.incremental-watermark.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-users-incremental
      output_contract:
        name: users
        representation: rows
        fields:
          - name: updated_at
            type: string
            required: true
      incremental:
        checkpoint_kind: watermark
        field: updated_at
        lookback_window: 15m
        ordering:
          - updated_at
          - id
        dedupe_key: id
`,
			assertFn: func(t *testing.T, query *pipelinespec.QueryRef) {
				require.NotNil(t, query.Incremental)
				require.NotNil(t, query.Incremental.CheckpointKind)
				require.Equal(t, model.IncrementalCheckpointKindWatermark, *query.Incremental.CheckpointKind)
				require.Equal(t, "updated_at", query.Incremental.Field)
				require.Empty(t, query.Incremental.Cursor)
				require.Equal(t, "15m", query.Incremental.LookbackWindow)
				require.Equal(t, []string{"updated_at", "id"}, query.Incremental.Ordering)
				require.Equal(t, "id", query.Incremental.DedupeKey)
			},
		},
		{
			name: "WatermarkContractWithEmbeddedRuntimeVariableLookbackWindow",
			path: "pipeline.incremental-runtime-lookback.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-users-incremental
      output_contract:
        name: users
        representation: rows
        fields:
          - name: updated_at
            type: string
            required: true
      incremental:
        checkpoint_kind: watermark
        field: updated_at
        lookback_window: "{{runtime.env.lookback_hours}}h"
        ordering:
          - updated_at
        dedupe_key: id
`,
			assertFn: func(t *testing.T, query *pipelinespec.QueryRef) {
				require.NotNil(t, query.Incremental)
				require.Equal(t, "{{runtime.env.lookback_hours}}h", query.Incremental.LookbackWindow)
			},
		},
		{
			name: "ValidHTTPCursorContract",
			path: "pipeline.incremental-cursor.yaml",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: crm-api
      operation_type: http
      operation_ref: list-users-by-cursor
      incremental:
        checkpoint_kind: cursor
        cursor: next_page_token
`,
			assertFn: func(t *testing.T, query *pipelinespec.QueryRef) {
				require.NotNil(t, query.Incremental)
				require.NotNil(t, query.Incremental.CheckpointKind)
				require.Equal(t, model.IncrementalCheckpointKindCursor, *query.Incremental.CheckpointKind)
				require.Empty(t, query.Incremental.Field)
				require.Equal(t, "next_page_token", query.Incremental.Cursor)
				require.Empty(t, query.Incremental.LookbackWindow)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pipeline, err := Load(tt.path, []byte(tt.data))

			require.NoError(t, err)
			require.Len(t, pipeline.Steps, 1)
			require.NotNil(t, pipeline.Steps[0].Query)
			tt.assertFn(t, pipeline.Steps[0].Query)
		})
	}
}

func TestLoadRejectsInvalidIncrementalSourceContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		data            string
		wantErrContains string
	}{
		{
			name: "ExplicitUnknownCheckpointKind",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-users
      incremental:
        checkpoint_kind: unknown
`,
			wantErrContains: `incremental contract uses unsupported checkpoint_kind "unknown"`,
		},
		{
			name: "UnsupportedFTPWatermark",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-ftp
      operation_type: ftp
      operation_ref: read-users-csv
      incremental:
        checkpoint_kind: watermark
        field: updated_at
`,
			wantErrContains: `incremental mode is not supported for operation_type "ftp"`,
		},
		{
			name: "UnsupportedFTPCursor",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-ftp
      operation_type: ftp
      operation_ref: read-users-csv
      incremental:
        checkpoint_kind: cursor
        cursor: next_page_token
`,
			wantErrContains: `incremental mode is not supported for operation_type "ftp"`,
		},
		{
			name: "UnsupportedGitWatermark",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-git
      operation_type: git
      operation_ref: read-users-json
      incremental:
        checkpoint_kind: watermark
        field: updated_at
`,
			wantErrContains: `incremental mode is not supported for operation_type "git"`,
		},
		{
			name: "UnsupportedGitCursor",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-git
      operation_type: git
      operation_ref: read-users-json
      incremental:
        checkpoint_kind: cursor
        cursor: next_page_token
`,
			wantErrContains: `incremental mode is not supported for operation_type "git"`,
		},
		{
			name: "IncrementalBlockWithoutCheckpointKind",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-users
      incremental:
        field: updated_at
`,
			wantErrContains: "incremental contract requires checkpoint_kind",
		},
		{
			name: "WatermarkWithoutField",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-users
      incremental:
        checkpoint_kind: watermark
`,
			wantErrContains: "watermark incremental contract requires field",
		},
		{
			name: "CursorWithoutCursorName",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: crm-api
      operation_type: http
      operation_ref: list-users-by-cursor
      incremental:
        checkpoint_kind: cursor
`,
			wantErrContains: "cursor incremental contract requires cursor",
		},
		{
			name: "WatermarkWithInvalidLookbackWindow",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-users
      incremental:
        checkpoint_kind: watermark
        field: updated_at
        lookback_window: eventually
`,
			wantErrContains: "lookback_window must be a positive duration",
		},
		{
			name: "CursorWithLookbackWindow",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: crm-api
      operation_type: http
      operation_ref: list-users-by-cursor
      incremental:
        checkpoint_kind: cursor
        cursor: next_page_token
        lookback_window: 15m
`,
			wantErrContains: "cursor incremental contract must not declare lookback_window",
		},
		{
			name: "WatermarkFieldCannotBeProvenWithoutEffectiveOutputContract",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-users
      incremental:
        checkpoint_kind: watermark
        field: updated_at
`,
			wantErrContains: `watermark incremental field "updated_at" must be present in effective output contract`,
		},
		{
			name: "WatermarkFieldMissingFromOutputContract",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-users
      output_contract:
        name: users
        representation: rows
        fields:
          - name: id
            type: integer
            required: true
      incremental:
        checkpoint_kind: watermark
        field: updated_at
  - kind: sink
    slug: load-users
    sink:
      idempotency_class: idempotent_with_key
      input: fetch-users
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: upsert-users
      input_contract:
        name: users
        representation: rows
        fields:
          - name: id
            type: integer
            required: true
`,
			wantErrContains: `watermark incremental field "updated_at" must be present in effective output contract`,
		},
		{
			name: "SinkCannotOwnIncrementalContract",
			data: `metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: list-users
  - kind: sink
    slug: load-users
    sink:
      idempotency_class: idempotent_with_key
      input: fetch-users
      data_source_ref: acme-postgres
      operation_type: sql
      operation_ref: upsert-users
      incremental:
        checkpoint_kind: watermark
        field: updated_at
`,
			wantErrContains: "field incremental not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pipeline, err := Load("pipeline.invalid-incremental.yaml", []byte(tt.data))

			require.Nil(t, pipeline)
			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantErrContains)
		})
	}
}

func TestLoadWatermarkIncrementalUsesEffectiveOutputContract(t *testing.T) {
	t.Parallel()

	t.Run("AcceptsConsumerOnlyContractContainingWatermarkField", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-users-incremental",
						Incremental: &pipelinespec.IncrementalSourceContract{
							CheckpointKind: incrementalCheckpointKind(model.IncrementalCheckpointKindWatermark),
							Field:          "updated_at",
						},
					},
				},
				{
					Kind: "sink",
					Slug: "load-users",
					Sink: &pipelinespec.SinkSpec{
						IdempotencyClass: pipelinespec.SinkIdempotencyClassIdempotentWithKey,
						Input:            &pipelinespec.StepInput{FromStep: "fetch-users"},
						DataSourceSlug:   "acme-postgres",
						OperationType:    "sql",
						OperationRef:     "upsert-users",
						InputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "updated_at",
								Type:     model.ContractFieldTypeValueString,
								Required: true,
							}},
						},
					},
				},
			},
		}

		err := validateStepInputCompatibility(pipeline)
		require.NoError(t, err)
		require.NoError(t, validateIncrementalSourceContract(pipeline.Steps[0].Query))
	})

	t.Run("RejectsEffectiveContractMissingWatermarkField", func(t *testing.T) {
		t.Parallel()

		pipeline := &pipelinespec.Pipeline{
			Metadata: pipelinespec.Metadata{Slug: "acme-users-sync", Name: "ACME Users Sync"},
			Steps: []pipelinespec.Step{
				{
					Kind: "query",
					Slug: "fetch-users",
					Query: &pipelinespec.QueryRef{
						DataSourceSlug: "acme-postgres",
						OperationType:  "sql",
						OperationRef:   "list-users-incremental",
						Incremental: &pipelinespec.IncrementalSourceContract{
							CheckpointKind: incrementalCheckpointKind(model.IncrementalCheckpointKindWatermark),
							Field:          "updated_at",
						},
					},
				},
				{
					Kind: "sink",
					Slug: "load-users",
					Sink: &pipelinespec.SinkSpec{
						IdempotencyClass: pipelinespec.SinkIdempotencyClassIdempotentWithKey,
						Input:            &pipelinespec.StepInput{FromStep: "fetch-users"},
						DataSourceSlug:   "acme-postgres",
						OperationType:    "sql",
						OperationRef:     "upsert-users",
						InputContract: &payload.Contract{
							Representation: model.ContractRepresentationRows,
							Fields: []payload.ContractField{{
								Name:     "id",
								Type:     model.ContractFieldTypeValueInteger,
								Required: true,
							}},
						},
					},
				},
			},
		}

		err := validateStepInputCompatibility(pipeline)
		require.Error(t, err)
		require.ErrorContains(t, err, `watermark incremental field "updated_at"`)
		require.ErrorContains(t, err, "effective output contract")
	})
}

// incrementalCheckpointKind returns a pointer for inline incremental contract fixtures.
func incrementalCheckpointKind(kind model.IncrementalCheckpointKind) *model.IncrementalCheckpointKind {
	return &kind
}
