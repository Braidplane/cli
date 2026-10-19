package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

func TestRunWithArgsValidateFile(t *testing.T) {
	t.Parallel()

	validSpec := []byte(`
metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-db
      operation_type: sql
      operation_ref: list-users
`)

	invalidSpec := []byte(`
metadata:
  slug: ""
  name: ""
steps: []
`)

	tests := []struct {
		name        string
		specContent []byte
		specPath    string
		assertErr   func(t *testing.T, err error)
		assertOut   func(t *testing.T, stdout, stderr string)
	}{
		{
			name:        "ValidSpec",
			specContent: validSpec,
			assertErr: func(t *testing.T, err error) {
				t.Helper()
				require.NoError(t, err)
			},
			assertOut: func(t *testing.T, stdout, stderr string) {
				t.Helper()
				require.Contains(t, stdout, "pipeline validation succeeded")
				require.Empty(t, stderr)
			},
		},
		{
			name:        "InvalidSpec",
			specContent: invalidSpec,
			assertErr: func(t *testing.T, err error) {
				t.Helper()
				require.Error(t, err)
				require.ErrorContains(t, err, "validate pipeline")
			},
			assertOut: func(t *testing.T, stdout, stderr string) {
				t.Helper()
				require.Empty(t, stdout)
				require.Empty(t, stderr)
			},
		},
		{
			name:     "NonExistentFile",
			specPath: "/nonexistent/path/test.pipeline.yaml",
			assertErr: func(t *testing.T, err error) {
				t.Helper()
				require.Error(t, err)
				require.ErrorContains(t, err, "validate pipeline")
			},
			assertOut: func(t *testing.T, stdout, stderr string) {
				t.Helper()
				require.Empty(t, stdout)
				require.Empty(t, stderr)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			specPath := tc.specPath
			if specPath == "" {
				dir := t.TempDir()
				specPath = filepath.Join(dir, "test.pipeline.yaml")
				require.NoError(t, os.WriteFile(specPath, tc.specContent, 0o600))
			}

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			err := runWithArgs(
				[]string{"braidplane", "pipeline", "validate", "--file", specPath},
				&stdout,
				&stderr,
			)
			tc.assertErr(t, err)
			tc.assertOut(t, stdout.String(), stderr.String())
		})
	}
}

func TestRunWithArgsRejectsUnknownCommand(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithArgs([]string{"braidplane", "pipeline", "unknown"}, &stdout, &stderr)

	require.Error(t, err)
	require.Empty(t, stdout.String())
	require.Contains(t, stderr.String(), "unknown")
}

func TestRunWithArgsValidateRequiresFile(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithArgs([]string{"braidplane", "pipeline", "validate"}, &stdout, &stderr)

	require.Error(t, err)
	require.Empty(t, stdout.String())
	require.Contains(t, stderr.String(), "--file")
	require.Contains(t, stderr.String(), "--dir")
	require.Contains(t, stderr.String(), "one of --file or --dir is required")
}

func TestRunWithArgsValidateRejectsFileAndDirTogether(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithArgs([]string{"braidplane", "pipeline", "validate", "--file", "a.pipeline.yaml", "--dir", "pipelines"}, &stdout, &stderr)

	require.Error(t, err)
	require.Empty(t, stdout.String())
	require.Contains(t, stderr.String(), "only one of --file or --dir may be set")
}

func TestRunWithArgsPipelineValidateDirectorySuccess(t *testing.T) {
	t.Parallel()

	specDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(specDir, "test.pipeline.yaml"), []byte(`
metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-db
      operation_type: sql
      operation_ref: list-users
`), 0o600))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithArgs([]string{"braidplane", "pipeline", "validate", "--dir", specDir}, &stdout, &stderr)

	require.NoError(t, err)
	require.Contains(t, stdout.String(), "pipeline validation succeeded for 1 pipeline spec(s) in "+specDir)
	require.Empty(t, stderr.String())
}

func TestRunWithArgsPipelineValidateUsesServerWhenProjectConfigured(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	specPath := filepath.Join(t.TempDir(), "test.pipeline.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(`
metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-db
      operation_type: sql
      operation_ref: list-users
      operation_inline:
        sql: select * from users
`), 0o600))

	var gotPath string
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")

		var body api.PipelineValidationRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if body.Spec.Metadata.Slug != "acme-users-sync" {
			t.Errorf("metadata slug = %q, want acme-users-sync", body.Spec.Metadata.Slug)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"valid": false,
			"diagnostics": [
				{
					"severity": "error",
					"code": "PIPELINE_INVALID_INPUT",
					"field": "steps[0].query",
					"step_slug": "fetch-users",
					"message": "query target must set exactly one of operation_ref or operation_inline"
				}
			]
		}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_API_KEY", "xto_env_token")
	t.Setenv("BRAIDPLANE_PROJECT", "data-pipelines")
	t.Setenv("BRAIDPLANE_ENVIRONMENT", "prod")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "validate", "--file", specPath},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "pipeline validation failed")
	require.Equal(t, "/api/organizations/acme/projects/data-pipelines/environments/prod/pipeline-validation", gotPath)
	require.Equal(t, "Bearer xto_env_token", gotAuth)
	require.Contains(t, stdout.String(), "ERROR steps[0].query fetch-users PIPELINE_INVALID_INPUT")
	require.Contains(
		t,
		stdout.String(),
		"query target must set exactly one of operation_ref or operation_inline",
	)
	require.Empty(t, stderr.String())
}

func TestWritePipelineValidationResultPrintsDataSourceRefSyncAndBindingDetails(t *testing.T) {
	t.Parallel()

	s3Type := api.DataSourceTypeS3
	refSlug := api.NullableSlug("orders-db")
	stepSlug := api.NullableSlug("fetch-orders")
	environmentSlug := api.NullableSlug("prod")
	requiredCapability := api.DataSourceRefCapabilityPipelineSource
	var stdout bytes.Buffer

	err := writePipelineValidationResult(&stdout, &api.PipelineValidationResponse{
		Valid: false,
		DataSourceRefSync: api.PipelineDataSourceRefSyncResult{
			DataSourceRefs: []api.PipelineDataSourceRefSyncItem{
				{
					DataSourceRefSlug:    "orders-db",
					DataSourceType:       nil,
					RequiredCapabilities: []api.DataSourceRefCapability{api.DataSourceRefCapabilityPipelineSource},
					Status:               api.PipelineDataSourceRefSyncStatusCreated,
				},
				{
					DataSourceRefSlug:    "orders-export",
					DataSourceType:       &s3Type,
					RequiredCapabilities: []api.DataSourceRefCapability{api.DataSourceRefCapabilityPipelineSink},
					Status:               api.PipelineDataSourceRefSyncStatusUnchanged,
				},
			},
		},
		Diagnostics: []api.PipelineValidationDiagnostic{
			{
				Severity:           api.PipelineValidationDiagnosticSeverityError,
				Code:               "DATA_SOURCE_BINDING_NOT_FOUND",
				Field:              "steps[0].query",
				StepSlug:           &stepSlug,
				DataSourceRefSlug:  &refSlug,
				EnvironmentSlug:    &environmentSlug,
				RequiredCapability: &requiredCapability,
				Message:            "data source binding orders-db is missing",
			},
		},
	})

	require.NoError(t, err)
	require.Contains(t, stdout.String(), "SYNC data_source_ref orders-db created data_source_type=untyped capabilities=pipeline_source")
	require.Contains(t, stdout.String(), "SYNC data_source_ref orders-export unchanged data_source_type=s3 capabilities=pipeline_sink")
	require.Contains(t, stdout.String(), "ERROR steps[0].query fetch-orders DATA_SOURCE_BINDING_NOT_FOUND: data source binding orders-db is missing (data_source_ref=orders-db environment=prod required_capability=pipeline_source)")
}

func TestRunWithArgsPipelineValidateSendsEnvironmentSlug(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	specPath := filepath.Join(t.TempDir(), "test.pipeline.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(`
metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: users-source
      operation_type: sql
      operation_ref: list-users
`), 0o600))

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body api.PipelineValidationRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		gotPath = r.URL.Path

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"valid":true,"diagnostics":[]}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_API_KEY", "xto_env_token")
	t.Setenv("BRAIDPLANE_PROJECT", "data-pipelines")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "validate", "--file", specPath, "--environment", "prod"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Equal(t, "/api/organizations/acme/projects/data-pipelines/environments/prod/pipeline-validation", gotPath)
	require.Contains(t, stdout.String(), "pipeline validation succeeded")
	require.Empty(t, stderr.String())
}

func TestRunWithArgsPipelineValidateSendsODCSContractRefs(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	specPath := filepath.Join(t.TempDir(), "contracts.pipeline.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(`
metadata:
  slug: contract-refs
  name: Contract refs
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: warehouse
      operation_type: sql
      operation_ref: list-users
      output_contract_ref: {contract_slug: users, revision_slug: v1, schema_object_id: users-row}
  - kind: transform
    slug: shape-users
    transform:
      inputs: {input: fetch-users}
      sql: SELECT id FROM input
      output_kind: rows
      input_contract_ref: {contract_slug: users, revision_slug: v1, schema_object_id: users-row}
      output_contract_ref: {contract_slug: users, revision_slug: v1, schema_object_id: shaped-users}
  - kind: sink
    slug: write-users
    sink:
      input: shape-users
      data_source_ref: warehouse
      operation_type: sql
      operation_ref: upsert-users
      idempotency_class: idempotent_with_key
      input_contract_ref: {contract_slug: users, revision_slug: v1, schema_object_id: shaped-users}
`), 0o600))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body api.PipelineValidationRequest
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
			return
		}
		query, err := body.Spec.Steps[0].AsPipelineQueryStep()
		if !assert.NoError(t, err) {
			return
		}
		transform, err := body.Spec.Steps[1].AsPipelineTransformStep()
		if !assert.NoError(t, err) {
			return
		}
		sink, err := body.Spec.Steps[2].AsPipelineSinkStep()
		if !assert.NoError(t, err) {
			return
		}
		assert.Equal(t, "users", query.Query.OutputContractRef.ContractSlug)
		assert.Equal(t, "users-row", transform.Transform.InputContractRef.SchemaObjectId)
		assert.Equal(t, "shaped-users", transform.Transform.OutputContractRef.SchemaObjectId)
		assert.Equal(t, "shaped-users", sink.Sink.InputContractRef.SchemaObjectId)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"valid":true,"diagnostics":[]}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_API_KEY", "xto_env_token")
	t.Setenv("BRAIDPLANE_PROJECT", "data-pipelines")
	t.Setenv("BRAIDPLANE_ENVIRONMENT", "prod")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "validate", "--file", specPath},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Contains(t, stdout.String(), "via Braidplane API")
	require.Empty(t, stderr.String())
}

func TestRunWithArgsPipelineValidateServerSuccess(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	specPath := filepath.Join(t.TempDir(), "test.pipeline.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(`
metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-db
      operation_type: sql
      operation_ref: list-users
`), 0o600))

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"valid": true,
			"diagnostics": [
				{
					"severity": "warning",
					"code": "PIPELINE_CONTRACT_IMPACT_WARNING",
					"field": "steps.fetch-users.output_contract.fields.email",
					"step_slug": "write-users",
					"message": "pipeline contract changes may affect downstream consumers"
				}
			],
			"impact_report": {
				"slug": "contract-impact-acme-users-sync-a1b2c3",
				"report_type": "contract_impact",
				"status": "open",
				"severity": "warning",
				"summary": "pipeline contract changes may affect downstream consumers",
				"changes": [
					{
						"kind": "field_added",
						"path": "steps.fetch-users.output_contract.fields.email",
						"before": "<none>",
						"after": "string(required=false,nullable=false)",
						"severity": "warning",
						"reason_code": "field_added"
					}
				],
				"impacts": [
					{
						"kind": "edge",
						"pipeline_slug": "acme-users-sync",
						"step_slug": "write-users",
						"edge_slug": "fetch-users->write-users",
						"severity": "warning",
						"message": "step write-users consumes output from fetch-users"
					}
				],
				"created_at": "2026-06-05T00:00:00Z"
			}
		}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_API_KEY", "xto_env_token")
	t.Setenv("BRAIDPLANE_PROJECT", "data-pipelines")
	t.Setenv("BRAIDPLANE_ENVIRONMENT", "prod")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "validate", "--file", specPath},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Equal(t, "/api/organizations/acme/projects/data-pipelines/environments/prod/pipeline-validation", gotPath)
	require.Contains(t, stdout.String(), "pipeline validation succeeded for "+specPath)
	require.Contains(t, stdout.String(), "via Braidplane API")
	require.Contains(t, stdout.String(), "WARNING steps.fetch-users.output_contract.fields.email write-users PIPELINE_CONTRACT_IMPACT_WARNING")
	require.Contains(t, stdout.String(), "WARNING impact_report: pipeline contract changes may affect downstream consumers")
	require.Contains(t, stdout.String(), `CHANGE WARNING field_added steps.fetch-users.output_contract.fields.email "<none>" -> "string(required=false,nullable=false)" reason=field_added`)
	require.Contains(t, stdout.String(), "IMPACT WARNING edge target=pipeline=acme-users-sync step=write-users edge=fetch-users->write-users")
	require.Empty(t, stderr.String())
}

func TestRunWithArgsPipelineValidateLocalBypassesServer(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	specPath := filepath.Join(t.TempDir(), "test.pipeline.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(`
metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-db
      operation_type: sql
      operation_ref: list-users
`), 0o600))

	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_API_KEY", "xto_env_token")
	t.Setenv("BRAIDPLANE_PROJECT", "data-pipelines")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "validate", "--file", specPath, "--local"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Equal(t, 0, requestCount)
	require.Contains(t, stdout.String(), "pipeline validation succeeded")
	require.Empty(t, stderr.String())
}

func TestRunWithArgsPipelineFormatUsesServerAndPrintsDiagnostics(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	specPath := filepath.Join(t.TempDir(), "test.pipeline.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(`
metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: sink
    slug: write-users
    sink:
      idempotency_class: idempotent_with_key
      input: missing-fetch
      data_source_ref: acme-db
      operation_type: sql
      operation_ref: write-users
`), 0o600))

	var gotAuth string
	var gotPath string
	var gotRequest api.PipelineSpecFormatRequest
	var decodeErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		decodeErr = json.NewDecoder(r.Body).Decode(&gotRequest)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"format": "yaml",
			"data": "metadata:\n    slug: acme-users-sync\n    name: ACME Users Sync\nsteps: []\n",
			"diagnostics": [
				{
					"code": "unresolved_step_reference",
					"message": "step reference could not be resolved while formatting the pipeline spec",
					"step_slug": "write-users",
					"referenced_step_slug": "missing-fetch"
				}
			]
		}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_API_KEY", "xto_env_token")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "format", "--file", specPath},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.NoError(t, decodeErr)
	require.Equal(t, "/api/organizations/acme/pipeline-spec:format", gotPath)
	require.Equal(t, "Bearer xto_env_token", gotAuth)
	require.Equal(t, api.PipelineSpecFormatRequestFormatYaml, gotRequest.Format)
	require.Contains(t, gotRequest.Data, "slug: acme-users-sync")
	require.Equal(t, "metadata:\n    slug: acme-users-sync\n    name: ACME Users Sync\nsteps: []\n", stdout.String())
	require.Contains(t, stderr.String(), "WARNING write-users missing-fetch unresolved_step_reference")
	require.Contains(t, stderr.String(), "step reference could not be resolved")
}

func TestRunWithArgsPipelineFormatUsesExplicitFormat(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	specPath := filepath.Join(t.TempDir(), "test.pipeline")
	require.NoError(t, os.WriteFile(specPath, []byte(`{"metadata":{"slug":"acme-users-sync","name":"ACME Users Sync"},"steps":[]}`), 0o600))

	var gotRequest api.PipelineSpecFormatRequest
	var decodeErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeErr = json.NewDecoder(r.Body).Decode(&gotRequest)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"format": "json",
			"data": "{\"metadata\":{\"slug\":\"acme-users-sync\",\"name\":\"ACME Users Sync\"},\"steps\":[]}",
			"diagnostics": []
		}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_API_KEY", "xto_env_token")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "format", "--file", specPath, "--format", "json"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.NoError(t, decodeErr)
	require.Equal(t, api.PipelineSpecFormatRequestFormatJson, gotRequest.Format)
	require.JSONEq(t, `{"metadata":{"slug":"acme-users-sync","name":"ACME Users Sync"},"steps":[]}`, stdout.String())
	require.Empty(t, stderr.String())
}

func TestRunWithArgsPipelineFormatRequiresInferableFormat(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "test.pipeline")
	require.NoError(t, os.WriteFile(specPath, []byte(`metadata: {}`), 0o600))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithArgs(
		[]string{"braidplane", "pipeline", "format", "--file", specPath},
		&stdout,
		&stderr,
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "pipeline format could not be inferred")
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())
}

func TestLoadPipelineValidationSpec(t *testing.T) {
	t.Parallel()

	t.Run("ReturnsReadError", func(t *testing.T) {
		t.Parallel()

		_, err := loadPipelineValidationSpec(filepath.Join(t.TempDir(), "missing.pipeline.yaml"))

		require.Error(t, err)
		require.ErrorContains(t, err, "read pipeline")
	})

	t.Run("ReturnsLoadError", func(t *testing.T) {
		t.Parallel()

		specPath := filepath.Join(t.TempDir(), "bad.pipeline.yaml")
		require.NoError(t, os.WriteFile(specPath, []byte("not: [valid"), 0o600))

		_, err := loadPipelineValidationSpec(specPath)

		require.Error(t, err)
		require.ErrorContains(t, err, "load pipeline")
	})
}

func TestValidateCommandRunReturnsWriteErrors(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "test.pipeline.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(`
metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-db
      operation_type: sql
      operation_ref: list-users
`), 0o600))

	err := (&validateCommand{File: specPath}).Run(
		&cliOutput{stdout: failingWriter{}, stderr: io.Discard},
		&cliRuntime{},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "write failed")
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, xerror.Unexpected().Detail("write failed")
}

func TestRunWithArgsPipelineListUsesSavedLoginAndPrintsPipelines(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotAuth string
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"pipelines": [
				{
					"slug": "acme-users-sync",
					"name": "ACME Users Sync",
					"source_path": "pipelines/acme-users-sync.pipeline.yaml",
					"source_ref": "refs/heads/main",
					"last_applied_commit": "0123456789abcdef0123456789abcdef01234567",
					"sync_status": "synced",
					"has_draft": false,
					"created_at": "2026-05-14T00:00:00Z",
					"updated_at": "2026-05-14T00:00:00Z"
				},
				{
					"slug": "nightly-export",
					"name": "Nightly Export",
					"source_path": "pipelines/acme-users-sync.pipeline.yaml",
					"source_ref": "refs/heads/main",
					"last_applied_commit": "0123456789abcdef0123456789abcdef01234567",
					"sync_status": "drifted",
					"has_draft": true,
					"created_at": "2026-05-14T00:00:00Z",
					"updated_at": "2026-05-14T00:00:00Z"
				}
			],
			"has_more": true,
			"next_cursor": "cursor-next"
		}`))
	}))
	t.Cleanup(server.Close)

	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		APIKey:       "xto_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "list", "--project", "data-pipelines"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Equal(t, "Bearer xto_saved_token", gotAuth)
	require.Equal(t, "/api/organizations/acme/projects/data-pipelines/pipelines", gotPath)
	require.Contains(t, stdout.String(), "acme-users-sync")
	require.Contains(t, stdout.String(), "ACME Users Sync")
	require.Contains(t, stdout.String(), "nightly-export")
	require.Contains(t, stdout.String(), "Nightly Export")
	require.Contains(t, stdout.String(), "cursor-next")
}

func TestRunWithArgsPipelineListForwardsPaginationFlags(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotLimit string
	var gotCursor string
	var gotDirection string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLimit = r.URL.Query().Get("limit")
		gotCursor = r.URL.Query().Get("cursor")
		gotDirection = r.URL.Query().Get("direction")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"pipelines": [],
			"has_more": true,
			"next_cursor": "cursor-next",
			"prev_cursor": "cursor-prev"
		}`))
	}))
	t.Cleanup(server.Close)

	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		PAT:          "xto_pat_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane",
			"pipeline",
			"list",
			"--project", "data-pipelines",
			"--limit", "2",
			"--cursor", "cursor-prev",
			"--direction", "prev",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Equal(t, "2", gotLimit)
	require.Equal(t, "cursor-prev", gotCursor)
	require.Equal(t, "prev", gotDirection)
	require.Contains(t, stdout.String(), "cursor-next")
	require.Contains(t, stdout.String(), "cursor-prev")
}

func TestRunWithArgsPipelineApplyPostsRequestAndPrintsReport(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotAuth string
	var gotPath string
	var gotMethod string
	var gotRequest api.PipelineReconcileRequest
	var decodeErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		decodeErr = json.NewDecoder(r.Body).Decode(&gotRequest)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"dry_run": true,
			"pipeline_dir": "git-pipelines",
			"requested_pipeline_slugs": ["acme-users-sync", "nightly-export"],
			"source": {
				"data_source_slug": "acme-git",
				"repo": "github.com/acme/pipelines",
				"ref": "refs/heads/main",
				"commit": "abc123"
			},
			"summary": {
				"scanned": 4,
				"synced": 1,
				"deleted": 1,
				"skipped": 1,
				"failed": 1,
				"drift_reset": 1
			},
			"synced": [{
				"pipeline_slug": "acme-users-sync",
				"source_path": "git-pipelines/acme-users-sync.pipeline.yaml",
				"spec_hash": "synced-hash"
			}],
			"deleted": [{
				"pipeline_slug": "legacy-sync",
				"previous_source_path": "git-pipelines/legacy-sync.pipeline.yaml"
			}],
			"skipped": [{
				"pipeline_slug": "other-sync",
				"source_path": "git-pipelines/other-sync.pipeline.yaml",
				"reason": "filtered_out"
			}],
			"drift_resets": [{
				"pipeline_slug": "nightly-export",
				"source_path": "git-pipelines/nightly-export.pipeline.yaml",
				"previous_spec_hash": "drifted-hash",
				"applied_spec_hash": "applied-hash"
			}],
			"failures": [{
				"pipeline_path": "git-pipelines/broken.pipeline.yaml",
				"pipeline_slug": "broken-sync",
				"stage": "load",
				"message": "pipeline metadata.slug is required"
			}]
		}`))
	}))
	t.Cleanup(server.Close)

	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		APIKey:       "xto_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane",
			"pipeline",
			"apply",
			"--project", "data-pipelines",
			"--environment", "prod",
			"--dry-run",
			"--dir", "git-pipelines",
			"--pipeline", "acme-users-sync",
			"--pipeline", "nightly-export",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.NoError(t, decodeErr)
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "/api/organizations/acme/projects/data-pipelines/pipelines:reconcile", gotPath)
	require.Equal(t, "Bearer xto_saved_token", gotAuth)
	require.NotNil(t, gotRequest.DryRun)
	require.True(t, *gotRequest.DryRun)
	require.NotNil(t, gotRequest.PipelineDir)
	require.Equal(t, "git-pipelines", *gotRequest.PipelineDir)
	require.NotNil(t, gotRequest.PipelineSlugs)
	require.Equal(t, []api.Slug{"acme-users-sync", "nightly-export"}, *gotRequest.PipelineSlugs)
	require.Contains(t, stdout.String(), `"synced"`)
	require.Contains(t, stdout.String(), "acme-users-sync")
	require.Contains(t, stdout.String(), "legacy-sync")
	require.Contains(t, stdout.String(), "filtered_out")
	require.Contains(t, stdout.String(), "nightly-export")
	require.Contains(t, stdout.String(), "pipeline metadata.slug is required")
}

func TestRunWithArgsPipelineApplyImportsWithoutDeploying(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotReconcile bool
	var gotReconcileMethod string
	var gotDeploy bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/organizations/acme/projects/data-pipelines/pipelines:reconcile":
			gotReconcile = true
			gotReconcileMethod = r.Method
			_, _ = w.Write([]byte(`{
				"dry_run": false,
				"pipeline_dir": "git-pipelines",
				"requested_pipeline_slugs": ["acme-users-sync"],
				"source": {
					"data_source_slug": "acme-git",
					"repo": "github.com/acme/pipelines",
					"ref": "refs/heads/main",
					"commit": "abc123"
				},
				"summary": {
					"scanned": 1,
					"synced": 1,
					"deleted": 0,
					"skipped": 0,
					"failed": 0,
					"drift_reset": 0
				},
				"synced": [{
					"pipeline_slug": "acme-users-sync",
					"source_path": "git-pipelines/acme-users-sync.pipeline.yaml",
					"spec_hash": "synced-hash"
				}],
				"deleted": [],
				"skipped": [],
				"drift_resets": [],
				"failures": []
			}`))
		case "/api/organizations/acme/projects/data-pipelines/environments/prod/pipelines/acme-users-sync:deploy":
			gotDeploy = true
			http.Error(w, "deployment must be explicit", http.StatusBadRequest)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		APIKey:       "xto_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane",
			"pipeline",
			"apply",
			"--project", "data-pipelines",
			"--environment", "prod",
			"--dir", "git-pipelines",
			"--pipeline", "acme-users-sync",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.True(t, gotReconcile)
	require.Equal(t, http.MethodPost, gotReconcileMethod)
	require.False(t, gotDeploy)
	require.Contains(t, stdout.String(), `"dry_run": false`)
	require.Contains(t, stdout.String(), "acme-users-sync")
}

func TestRunWithArgsPipelineApplyDoesNotRequireEnvironmentScope(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"dry_run": true,
			"summary": {
				"scanned": 0,
				"synced": 0,
				"deleted": 0,
				"skipped": 0,
				"failed": 0,
				"drift_reset": 0
			}
		}`))
	}))
	t.Cleanup(server.Close)
	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		APIKey:       "xto_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "apply", "--project", "data-pipelines", "--dry-run"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Contains(t, stdout.String(), `"dry_run": true`)
	require.Empty(t, stderr.String())
	require.Equal(t, 1, requestCount)
}

func TestRunWithArgsPipelineApplyRejectsSourceOverrideFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "source branch",
			args: []string{"braidplane", "pipeline", "apply", "--project", "data-pipelines", "--source-branch", "main"},
			want: "unknown flag --source-branch",
		},
		{
			name: "source data source",
			args: []string{"braidplane", "pipeline", "apply", "--project", "data-pipelines", "--source-data-source", "pipelines-git"},
			want: "unknown flag --source-data-source",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			configPath := filepath.Join(t.TempDir(), "braidplane.toml")
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			err := runWithRuntime(
				tc.args,
				&stdout,
				&stderr,
				cliRuntime{configPath: configPath},
			)

			require.Error(t, err)
			require.Contains(t, stderr.String(), tc.want)
			require.Empty(t, stdout.String())
		})
	}
}

func TestRunWithArgsPipelineReconcileIsNotPublicCommand(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "reconcile", "--project", "data-pipelines"},
		&stdout,
		&stderr,
		cliRuntime{configPath: filepath.Join(t.TempDir(), "braidplane.toml")},
	)

	require.Error(t, err)
	require.Contains(t, stderr.String(), "reconcile")
	require.Empty(t, stdout.String())
}

func TestRunWithArgsPipelineHelpShowsGitMaterializationCommandsOnly(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "--help"},
		&stdout,
		&stderr,
		cliRuntime{configPath: filepath.Join(t.TempDir(), "braidplane.toml")},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Contains(t, stdout.String(), "apply")
	require.Contains(t, stdout.String(), "deploy")
	require.NotRegexp(t, `(?m)^\s+(create|delete|restore|purge)\b`, stdout.String())
	require.NotContains(t, stdout.String(), "transfer-to-git")
	require.NotContains(t, stdout.String(), "reconcile")
}

func TestRunWithArgsPipelineGetUsesEnvFallbackAndPrintsDetails(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	t.Setenv("BRAIDPLANE_SERVER_URL", "http://example.invalid")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_PAT", "xto_pat_env_token")
	t.Setenv("BRAIDPLANE_PROJECT", "data-pipelines")

	var gotAuth string
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"slug": "acme-users-sync",
			"name": "ACME Users Sync",
			"source_path": "pipelines/acme-users-sync.pipeline.yaml",
			"source_ref": "refs/heads/main",
			"last_applied_commit": "0123456789abcdef0123456789abcdef01234567",
			"sync_status": "synced",
			"has_draft": false,
			"created_at": "2026-05-14T00:00:00Z",
			"updated_at": "2026-05-14T00:00:00Z",
			"spec": {
				"metadata": {
					"slug": "acme-users-sync",
					"name": "ACME Users Sync"
				},
				"steps": [
					{
						"kind": "query",
						"slug": "fetch-users",
						"query": {
							"data_source_ref": "acme-db",
							"operation_type": "sql",
							"operation_ref": "list-users"
						}
					}
				]
			}
		}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "get", "acme-users-sync"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Equal(t, "Bearer xto_pat_env_token", gotAuth)
	require.Equal(t, "/api/organizations/acme/projects/data-pipelines/pipelines/acme-users-sync", gotPath)
	require.Contains(t, stdout.String(), "acme-users-sync")
	require.Contains(t, stdout.String(), "ACME Users Sync")
	require.Contains(t, stdout.String(), "fetch-users")
}

func TestRunWithArgsPipelineGetMapsStructuredAPIError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{
			"error": {
				"code": "NOT_FOUND",
				"message": "pipeline not found",
				"detailed_error": "pipeline acme-users-sync does not exist"
			}
		}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_PAT", "xto_pat_env_token")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "get", "acme-users-sync", "--project", "data-pipelines"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "404")
	require.ErrorContains(t, err, "NOT_FOUND")
	require.ErrorContains(t, err, "pipeline not found")
	require.ErrorContains(t, err, "acme-users-sync")
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())
}

func TestRunWithArgsPipelineListHelpDoesNotRunCommand(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pipelines":[],"has_more":false}`))
	}))
	t.Cleanup(server.Close)

	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		PAT:          "xto_pat_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "list", "--help"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Usage: braidplane pipeline list [flags]")
	require.Contains(t, stdout.String(), "List pipelines from the Braidplane API.")
	require.Contains(t, stdout.String(), "--project=STRING")
	require.NotContains(t, stdout.String(), "\"has_more\"")
	require.Empty(t, stderr.String())
	require.Zero(t, requestCount)
}

func TestRunWithArgsPipelineListRequiresProjectScope(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    "http://example.invalid",
		Organization: "acme",
		APIKey:       "xto_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "list"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "project is required")
	require.ErrorContains(t, err, "set --project or BRAIDPLANE_PROJECT")
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())
}

func TestRunWithArgsPipelineUpdatePrintsUpdatedPipeline(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "update.pipeline.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(`
metadata:
  slug: acme-users-sync
  name: ACME Users Sync v2
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-db
      operation_type: sql
      operation_ref: list-users
`), 0o600))

	t.Setenv("BRAIDPLANE_SERVER_URL", "http://example.invalid")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_API_KEY", "xto_env_token")
	var gotAuth string
	var gotPaths []string
	var gotMethod string
	var gotBody api.UpdatePipeline
	var gotDecodeErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/organizations/acme/projects/data-pipelines/pipelines/acme-users-sync":
			gotAuth = r.Header.Get("Authorization")
			gotMethod = r.Method
			gotDecodeErr = json.NewDecoder(r.Body).Decode(&gotBody)
			_, _ = w.Write([]byte(`{
				"slug": "acme-users-sync",
				"name": "ACME Users Sync v2",
				"source_path": "pipelines/acme-users-sync.pipeline.yaml",
				"source_ref": "refs/heads/main",
				"last_applied_commit": "0123456789abcdef0123456789abcdef01234567",
				"sync_status": "synced",
				"has_draft": false,
				"created_at": "2026-05-15T00:00:00Z",
				"updated_at": "2026-05-15T01:00:00Z",
				"spec": {
					"metadata": {
						"slug": "acme-users-sync",
						"name": "ACME Users Sync v2"
					},
					"steps": [
						{
							"kind": "query",
							"slug": "fetch-users",
							"query": {
								"data_source_ref": "acme-db",
								"operation_type": "sql",
								"operation_ref": "list-users"
							}
						}
					]
				}
			}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "update", "acme-users-sync", "--file", specPath, "--project", "data-pipelines"},
		&stdout,
		&stderr,
		cliRuntime{configPath: filepath.Join(t.TempDir(), "braidplane.toml")},
	)

	require.NoError(t, err)
	require.NoError(t, gotDecodeErr)
	require.Empty(t, stderr.String())
	require.Equal(t, http.MethodPut, gotMethod)
	require.Equal(t, "Bearer xto_env_token", gotAuth)
	require.Equal(t, []string{"/api/organizations/acme/projects/data-pipelines/pipelines/acme-users-sync"}, gotPaths)
	require.Equal(t, "ACME Users Sync v2", gotBody.Spec.Metadata.Name)
	require.Contains(t, stdout.String(), "acme-users-sync")
	require.Contains(t, stdout.String(), "ACME Users Sync v2")
}

func TestRunWithArgsPipelineUpdateDoesNotRequireEnvironmentScope(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "update.pipeline.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(`
metadata:
  slug: acme-users-sync
  name: ACME Users Sync v2
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-db
      operation_type: sql
      operation_ref: list-users
`), 0o600))
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"slug": "acme-users-sync",
			"name": "ACME Users Sync v2",
			"source_path": "pipelines/acme-users-sync.pipeline.yaml",
			"source_ref": "refs/heads/main",
			"last_applied_commit": "0123456789abcdef0123456789abcdef01234567",
			"sync_status": "synced",
			"has_draft": false,
			"created_at": "2026-05-15T00:00:00Z",
			"updated_at": "2026-05-15T01:00:00Z",
			"spec": {
				"metadata": {
					"slug": "acme-users-sync",
					"name": "ACME Users Sync v2"
				},
				"steps": []
			}
		}`))
	}))
	t.Cleanup(server.Close)
	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		APIKey:       "xto_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane", "pipeline", "update", "acme-users-sync",
			"--file", specPath,
			"--project", "data-pipelines",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Equal(t, 1, requestCount)
	require.Contains(t, stdout.String(), `"slug": "acme-users-sync"`)
	require.Contains(t, stdout.String(), `"name": "ACME Users Sync v2"`)
}

func TestRunWithArgsPipelineUpdateRejectsMismatchedSpecSlug(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "update.pipeline.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(`
metadata:
  slug: nightly-export
  name: ACME Users Sync v2
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-db
      operation_type: sql
      operation_ref: list-users
`), 0o600))

	t.Setenv("BRAIDPLANE_SERVER_URL", "http://example.invalid")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_API_KEY", "xto_env_token")

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "update", "acme-users-sync", "--file", specPath},
		&stdout,
		&stderr,
		cliRuntime{configPath: filepath.Join(t.TempDir(), "braidplane.toml")},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "pipeline slug mismatch")
	require.ErrorContains(t, err, "acme-users-sync")
	require.ErrorContains(t, err, "nightly-export")
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())
	require.Zero(t, requestCount)
}

func TestRunWithArgsPipelineDeployPostsRequestAndPrintsDeployment(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotAuth string
	var gotPath string
	var gotMethod string
	var gotBody api.PipelineDeploymentRequest
	var decodeErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		decodeErr = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"pipeline_slug": "acme-users-sync",
			"project_slug": "data-pipelines",
			"environment_slug": "prod",
			"current_version_spec_hash": "abc1234fffff000000000000000000000000000000000000000000000000000",
			"current_version_short_spec_hash": "abc1234fffff",
			"previous_version_spec_hash": "def5678fffff000000000000000000000000000000000000000000000000000",
			"previous_version_short_spec_hash": "def5678fffff",
			"deployed_by_actor_type": "api_key",
			"deployed_by_actor_slug": "ci-deploy-key",
			"deployed_at": "2026-05-15T08:00:00Z",
			"updated_at": "2026-05-15T08:00:00Z"
		}`))
	}))
	t.Cleanup(server.Close)

	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		APIKey:       "xto_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane", "pipeline", "deploy", "acme-users-sync",
			"--project", "data-pipelines",
			"--environment", "prod",
			"--spec-hash", "abc1234",
			"--expected-current-spec-hash", "def5678",
			"--source-environment", "stg",
			"--proof-receipt", "proof-123",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.NoError(t, decodeErr)
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "Bearer xto_saved_token", gotAuth)
	require.Equal(t, "/api/organizations/acme/projects/data-pipelines/environments/prod/pipelines/acme-users-sync:deploy", gotPath)
	require.Equal(t, "abc1234", gotBody.SpecHash)
	require.NotNil(t, gotBody.ExpectedCurrentSpecHash)
	require.Equal(t, "def5678", *gotBody.ExpectedCurrentSpecHash)
	require.NotNil(t, gotBody.SourceEnvironmentSlug)
	require.Equal(t, api.NullableSlug("stg"), *gotBody.SourceEnvironmentSlug)
	require.NotNil(t, gotBody.ProofReceiptSlug)
	require.Equal(t, api.NullableSlug("proof-123"), *gotBody.ProofReceiptSlug)
	require.Contains(t, stdout.String(), `"environment_slug": "prod"`)
	require.Contains(t, stdout.String(), `"current_version_short_spec_hash": "abc1234fffff"`)
}

func TestRunWithArgsPipelineDraftGetUsesSavedLoginAndPrintsDraft(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotAuth string
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"draft": {
				"pipeline_slug": "acme-users-sync",
				"based_on_spec_hash": "spec-hash-12",
				"based_on_version_id": 12,
				"current_version_id": 13,
				"created_at": "2026-05-15T00:00:00Z",
				"updated_at": "2026-05-15T01:00:00Z",
				"spec": {
					"metadata": {
						"slug": "acme-users-sync",
						"name": "ACME Users Sync Draft"
					},
					"steps": [
						{
							"kind": "query",
							"slug": "fetch-users",
							"query": {
								"data_source_ref": "acme-db",
								"operation_type": "sql",
								"operation_ref": "list-users"
							}
						}
					]
				}
			}
		}`))
	}))
	t.Cleanup(server.Close)

	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		PAT:          "xto_pat_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "draft", "get", "acme-users-sync", "--project", "data-pipelines"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Equal(t, "Bearer xto_pat_saved_token", gotAuth)
	require.Equal(t, "/api/organizations/acme/projects/data-pipelines/pipelines/acme-users-sync/draft", gotPath)
	require.Contains(t, stdout.String(), "acme-users-sync")
	require.Contains(t, stdout.String(), "ACME Users Sync Draft")
	require.Contains(t, stdout.String(), "spec-hash-12")
}

func TestRunWithArgsPipelineDraftGetRejectsAPIKeyOnlyAuth(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    "https://api.example.test",
		Organization: "acme",
		APIKey:       "xto_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "draft", "get", "acme-users-sync", "--project", "data-pipelines"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "personal access token")
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())
}

func TestRunWithArgsPipelineDraftGetMapsStructuredAPIError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{
			"error": {
				"code": "PIPELINE_DRAFT_NOT_FOUND",
				"message": "pipeline draft not found",
				"detailed_error": "pipeline acme-users-sync has no saved draft"
			}
		}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_PAT", "xto_pat_env_token")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "draft", "get", "acme-users-sync", "--project", "data-pipelines"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "404")
	require.ErrorContains(t, err, "PIPELINE_DRAFT_NOT_FOUND")
	require.ErrorContains(t, err, "pipeline draft not found")
	require.ErrorContains(t, err, "acme-users-sync")
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())
}

func TestRunWithArgsPipelineDraftDiscardUsesSavedLoginAndReturnsNoOutput(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotAuth string
	var gotPath string
	var gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		PAT:          "xto_pat_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "draft", "discard", "acme-users-sync", "--project", "data-pipelines"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())
	require.Equal(t, http.MethodDelete, gotMethod)
	require.Equal(t, "Bearer xto_pat_saved_token", gotAuth)
	require.Equal(t, "/api/organizations/acme/projects/data-pipelines/pipelines/acme-users-sync/draft", gotPath)
}

func TestRunWithArgsPipelineDraftDiscardMapsStructuredAPIError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{
			"error": {
				"code": "PIPELINE_DRAFT_NOT_FOUND",
				"message": "pipeline draft not found",
				"detailed_error": "pipeline acme-users-sync has no saved draft"
			}
		}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_PAT", "xto_pat_env_token")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "draft", "discard", "acme-users-sync", "--project", "data-pipelines"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "404")
	require.ErrorContains(t, err, "PIPELINE_DRAFT_NOT_FOUND")
	require.ErrorContains(t, err, "pipeline draft not found")
	require.ErrorContains(t, err, "acme-users-sync")
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())
}

func TestRunWithArgsPipelineDraftSaveUsesSavedLoginAndPrintsDraft(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "draft.pipeline.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(`
metadata:
  slug: acme-users-sync
  name: ACME Users Sync Draft
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-db
      operation_type: sql
      operation_ref: list-users
`), 0o600))

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotAuth string
	var gotPath string
	var gotMethod string
	var gotBody api.SavePipelineDraftRequest
	var gotDecodeErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotDecodeErr = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"draft": {
				"pipeline_slug": "acme-users-sync",
				"based_on_spec_hash": "spec-hash-12",
				"based_on_version_id": 12,
				"current_version_id": 12,
				"created_at": "2026-05-15T00:00:00Z",
				"updated_at": "2026-05-15T01:00:00Z",
				"spec": {
					"metadata": {
						"slug": "acme-users-sync",
						"name": "ACME Users Sync Draft"
					},
					"steps": [
						{
							"kind": "query",
							"slug": "fetch-users",
							"query": {
								"data_source_ref": "acme-db",
								"operation_type": "sql",
								"operation_ref": "list-users"
							}
						}
					]
				}
			}
		}`))
	}))
	t.Cleanup(server.Close)

	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		PAT:          "xto_pat_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane", "pipeline", "draft", "save", "acme-users-sync",
			"--file", specPath,
			"--project", "data-pipelines",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.NoError(t, gotDecodeErr)
	require.Empty(t, stderr.String())
	require.Equal(t, http.MethodPut, gotMethod)
	require.Equal(t, "Bearer xto_pat_saved_token", gotAuth)
	require.Equal(t, "/api/organizations/acme/projects/data-pipelines/pipelines/acme-users-sync/draft", gotPath)
	require.Equal(t, "ACME Users Sync Draft", gotBody.Spec.Metadata.Name)
	require.Nil(t, gotBody.ExpectedSpecHash)
	require.Contains(t, stdout.String(), "acme-users-sync")
	require.Contains(t, stdout.String(), "ACME Users Sync Draft")
}

func TestRunWithArgsPipelineDraftSaveMapsStructuredConflict(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "draft.pipeline.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(`
metadata:
  slug: acme-users-sync
  name: ACME Users Sync Draft
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-db
      operation_type: sql
      operation_ref: list-users
`), 0o600))

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotBody api.SavePipelineDraftRequest
	var gotDecodeErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotDecodeErr = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{
			"error": {
				"code": "PIPELINE_DRAFT_CONFLICT",
				"message": "pipeline draft version conflict"
			},
			"pipeline_slug": "acme-users-sync",
			"expected_spec_hash": "stale-spec-hash",
			"current_spec_hash": "current-spec-hash",
			"draft_based_on_spec_hash": "draft-base-spec-hash",
			"draft_preserved": true,
			"can_overwrite": false
		}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_PAT", "xto_pat_env_token")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane", "pipeline", "draft", "save", "acme-users-sync",
			"--file", specPath,
			"--project", "data-pipelines",
			"--expected-spec-hash", "stale-spec-hash",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "409")
	require.ErrorContains(t, err, "PIPELINE_DRAFT_CONFLICT")
	require.ErrorContains(t, err, "acme-users-sync")
	require.ErrorContains(t, err, "current_spec_hash")
	require.ErrorContains(t, err, "can_overwrite")
	require.NoError(t, gotDecodeErr)
	require.NotNil(t, gotBody.ExpectedSpecHash)
	require.Equal(t, "stale-spec-hash", *gotBody.ExpectedSpecHash)
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())
}

func TestRunWithArgsPipelineDraftPublishUsesSavedLoginAndPrintsPipeline(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotAuth string
	var gotPath string
	var gotMethod string
	var gotBody api.PublishPipelineDraftRequest
	var gotDecodeErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotDecodeErr = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"slug": "acme-users-sync",
			"name": "ACME Users Sync Draft",
			"source_path": "pipelines/acme-users-sync.pipeline.yaml",
			"source_ref": "refs/heads/main",
			"last_applied_commit": "0123456789abcdef0123456789abcdef01234567",
			"sync_status": "synced",
			"has_draft": false,
			"created_at": "2026-05-15T00:00:00Z",
			"updated_at": "2026-05-15T01:00:00Z",
			"spec": {
				"metadata": {
					"slug": "acme-users-sync",
					"name": "ACME Users Sync Draft"
				},
				"steps": [
					{
						"kind": "query",
						"slug": "fetch-users",
						"query": {
							"data_source_ref": "acme-db",
							"operation_type": "sql",
							"operation_ref": "list-users"
						}
					}
				]
			}
		}`))
	}))
	t.Cleanup(server.Close)

	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		PAT:          "xto_pat_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "draft", "publish", "acme-users-sync", "--project", "data-pipelines"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.NoError(t, gotDecodeErr)
	require.Empty(t, stderr.String())
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "Bearer xto_pat_saved_token", gotAuth)
	require.Equal(t, "/api/organizations/acme/projects/data-pipelines/pipelines/acme-users-sync/draft:publish", gotPath)
	require.Nil(t, gotBody.ExpectedSpecHash)
	require.Nil(t, gotBody.Force)
	require.Contains(t, stdout.String(), "acme-users-sync")
	require.Contains(t, stdout.String(), "\"has_draft\": false")
}

func TestRunWithArgsPipelineDraftPublishMapsStructuredConflict(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotBody api.PublishPipelineDraftRequest
	var gotDecodeErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotDecodeErr = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{
			"error": {
				"code": "PIPELINE_DRAFT_CONFLICT",
				"message": "pipeline draft version conflict"
			},
			"pipeline_slug": "acme-users-sync",
			"expected_spec_hash": "stale-spec-hash",
			"current_spec_hash": "current-spec-hash",
			"draft_based_on_spec_hash": "draft-base-spec-hash",
			"draft_preserved": true,
			"can_overwrite": true
		}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_PAT", "xto_pat_env_token")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane", "pipeline", "draft", "publish", "acme-users-sync",
			"--project", "data-pipelines",
			"--expected-spec-hash", "stale-spec-hash",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "409")
	require.ErrorContains(t, err, "PIPELINE_DRAFT_CONFLICT")
	require.ErrorContains(t, err, "acme-users-sync")
	require.ErrorContains(t, err, "current_spec_hash")
	require.ErrorContains(t, err, "can_overwrite")
	require.NoError(t, gotDecodeErr)
	require.NotNil(t, gotBody.ExpectedSpecHash)
	require.Equal(t, "stale-spec-hash", *gotBody.ExpectedSpecHash)
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())
}

func TestRunWithArgsPipelineVersionListUsesSavedLoginAndPrintsVersions(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotAuth string
	var gotPath string
	var gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"versions": [
				{
					"spec_hash": "abc1234fffff000000000000000000000000000000000000000000000000000",
					"short_spec_hash": "abc1234fffff",
					"pipeline_slug": "acme-users-sync",
					"git_commit_sha": "abc123def456",
					"created_at": "2026-05-15T00:00:00Z"
				},
				{
					"spec_hash": "def5678fffff000000000000000000000000000000000000000000000000000",
					"short_spec_hash": "def5678fffff",
					"pipeline_slug": "acme-users-sync",
					"created_at": "2026-05-14T00:00:00Z"
				}
			]
		}`))
	}))
	t.Cleanup(server.Close)

	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		APIKey:       "xto_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "version", "list", "acme-users-sync", "--project", "data-pipelines"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Equal(t, http.MethodGet, gotMethod)
	require.Equal(t, "Bearer xto_saved_token", gotAuth)
	require.Equal(t, "/api/organizations/acme/projects/data-pipelines/pipelines/acme-users-sync/versions", gotPath)
	require.Contains(t, stdout.String(), "short_spec_hash")
	require.Contains(t, stdout.String(), "abc1234fffff")
	require.Contains(t, stdout.String(), "abc1234fffff000000000000000000000000000000000000000000000000000")
	require.Contains(t, stdout.String(), "def5678fffff")
	require.Contains(t, stdout.String(), "def5678fffff000000000000000000000000000000000000000000000000000")
	require.Contains(t, stdout.String(), "abc123def456")
}

func TestRunWithArgsPipelineVersionListMapsStructuredAPIError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{
			"error": {
				"code": "PIPELINE_NOT_FOUND",
				"message": "pipeline not found",
				"detailed_error": "pipeline acme-users-sync does not exist"
			}
		}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_API_KEY", "xto_env_token")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "version", "list", "acme-users-sync", "--project", "data-pipelines"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "404")
	require.ErrorContains(t, err, "PIPELINE_NOT_FOUND")
	require.ErrorContains(t, err, "pipeline not found")
	require.ErrorContains(t, err, "acme-users-sync")
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())
}

func TestRunWithArgsPipelineVersionGetUsesEnvFallbackAndPrintsVersion(t *testing.T) {
	var gotAuth string
	var gotPath string
	var gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"spec_hash": "abc1234fffff000000000000000000000000000000000000000000000000000",
			"short_spec_hash": "abc1234fffff",
			"pipeline_slug": "acme-users-sync",
			"git_commit_sha": "abc123def456",
			"created_at": "2026-05-15T00:00:00Z",
			"spec": {
				"metadata": {
					"slug": "acme-users-sync",
					"name": "ACME Users Sync"
				},
				"steps": [
					{
						"kind": "query",
						"slug": "fetch-users",
						"query": {
							"data_source_ref": "acme-db",
							"operation_type": "sql",
							"operation_ref": "list-users"
						}
					}
				]
			}
		}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_API_KEY", "xto_env_token")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane", "pipeline", "version", "get", "acme-users-sync", "abc1234",
			"--project", "data-pipelines",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: filepath.Join(t.TempDir(), "braidplane.toml")},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Equal(t, http.MethodGet, gotMethod)
	require.Equal(t, "Bearer xto_env_token", gotAuth)
	require.Equal(
		t,
		"/api/organizations/acme/projects/data-pipelines/pipelines/acme-users-sync/versions/abc1234",
		gotPath,
	)
	require.Contains(t, stdout.String(), "short_spec_hash")
	require.Contains(t, stdout.String(), "abc1234fffff")
	require.Contains(t, stdout.String(), "abc1234fffff000000000000000000000000000000000000000000000000000")
	require.Contains(t, stdout.String(), "abc123def456")
	require.Contains(t, stdout.String(), "ACME Users Sync")
}

func TestRunWithArgsPipelineVersionGetMapsStructuredAPIError(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{
			"error": {
				"code": "PIPELINE_VERSION_NOT_FOUND",
				"message": "pipeline version not found",
				"detailed_error": "version spec-hash-12 was not found for pipeline acme-users-sync"
			}
		}`))
	}))
	t.Cleanup(server.Close)

	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		APIKey:       "xto_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane", "pipeline", "version", "get", "acme-users-sync", "spec-hash-12",
			"--project", "data-pipelines",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "404")
	require.ErrorContains(t, err, "PIPELINE_VERSION_NOT_FOUND")
	require.ErrorContains(t, err, "pipeline version not found")
	require.ErrorContains(t, err, "spec-hash-12")
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())
}

func TestRunWithArgsPipelineRunListUsesSavedLoginAndPrintsRuns(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotAuth string
	var gotPath string
	var gotMethod string
	var gotPipelineSlug string
	var gotLimit string
	var gotCursor string
	var gotDirection string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotPipelineSlug = r.URL.Query().Get("pipeline_slug")
		gotLimit = r.URL.Query().Get("limit")
		gotCursor = r.URL.Query().Get("cursor")
		gotDirection = r.URL.Query().Get("direction")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"runs": [
				{
					"pipeline_slug": "acme-users-sync",
					"pipeline_version_spec_hash": "spec-hash-12",
					"run_slug": "run-001",
					"request_id": "req-001",
					"status": "failed",
					"duration_ms": 245,
					"step_count": 3,
					"step_slug": "load-users",
					"error_summary": "duplicate key value violates unique constraint",
					"failure_kind": "data_source",
					"started_at": "2026-05-15T08:00:00Z",
					"ended_at": "2026-05-15T08:00:00.245Z",
					"replay_of_run_slug": null,
					"replay_from_step": null,
					"created_at": "2026-05-15T08:00:00Z"
				}
			],
			"has_more": true,
			"next_cursor": "2026-05-15T08:00:00Z_run-001",
			"prev_cursor": null
		}`))
	}))
	t.Cleanup(server.Close)

	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		APIKey:       "xto_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane", "pipeline", "run", "list", "acme-users-sync",
			"--project", "data-pipelines",
			"--environment", "prod",
			"--limit", "2",
			"--cursor", "cursor-123",
			"--direction", "prev",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Equal(t, http.MethodGet, gotMethod)
	require.Equal(t, "Bearer xto_saved_token", gotAuth)
	require.Equal(t, "/api/organizations/acme/projects/data-pipelines/environments/prod/runs", gotPath)
	require.Equal(t, "acme-users-sync", gotPipelineSlug)
	require.Equal(t, "2", gotLimit)
	require.Equal(t, "cursor-123", gotCursor)
	require.Equal(t, "prev", gotDirection)
	require.Contains(t, stdout.String(), "run-001")
	require.Contains(t, stdout.String(), "spec-hash-12")
	require.Contains(t, stdout.String(), "duplicate key value")
}

func TestRunWithArgsPipelineRunListPassesOperationalFilters(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	gotQuery := make(map[string]string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, key := range []string{
			"pipeline_slug",
			"limit",
			"status",
			"mode",
			"backfill_series_slug",
			"sort_by",
			"order",
			"direction",
		} {
			gotQuery[key] = r.URL.Query().Get(key)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"runs": [],
			"has_more": false,
			"next_cursor": null,
			"prev_cursor": null
		}`))
	}))
	t.Cleanup(server.Close)

	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		APIKey:       "xto_saved_token",
		Environment:  "prod",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane", "pipeline", "run", "list", "acme-users-sync",
			"--project", "data-pipelines",
			"--limit", "10",
			"--status", "failed",
			"--mode", "backfill",
			"--backfill-series-slug", "series-001",
			"--sort-by", "run_created_at",
			"--order", "asc",
			"--direction", "next",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Equal(t, map[string]string{
		"pipeline_slug":        "acme-users-sync",
		"limit":                "10",
		"status":               "failed",
		"mode":                 "backfill",
		"backfill_series_slug": "series-001",
		"sort_by":              "run_created_at",
		"order":                "asc",
		"direction":            "next",
	}, gotQuery)
	require.Contains(t, stdout.String(), `"runs": []`)
}

func TestRunWithArgsPipelineRunListUsesSavedEnvironment(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"runs": [],
			"has_more": false,
			"next_cursor": null,
			"prev_cursor": null
		}`))
	}))
	t.Cleanup(server.Close)

	require.NoError(t, os.WriteFile(configPath, []byte(`server_url = "`+server.URL+`/api"
organization = "acme"
api_key = "xto_saved_token"
environment = "prod"
`), 0o600))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "run", "list", "acme-users-sync", "--project", "data-pipelines"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Equal(t, "/api/organizations/acme/projects/data-pipelines/environments/prod/runs", gotPath)
	require.Contains(t, stdout.String(), `"runs": []`)
}

func TestRunWithArgsPipelineRunListMapsStructuredAPIError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{
			"error": {
				"code": "VALIDATION_FAILED",
				"message": "validation failed",
				"detailed_error": "cursor is invalid for descending pipeline run pagination"
			}
		}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_API_KEY", "xto_env_token")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane", "pipeline", "run", "list", "acme-users-sync",
			"--project", "data-pipelines",
			"--environment", "prod",
			"--cursor", "bad-cursor",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "400")
	require.ErrorContains(t, err, "VALIDATION_FAILED")
	require.ErrorContains(t, err, "validation failed")
	require.ErrorContains(t, err, "bad-cursor")
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())
}

func TestRunWithArgsPipelineRunRetryAttemptsUsesSavedLoginAndPrintsDiagnostics(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotAuth string
	var gotPath string
	var gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"retry_attempts": [
				{
					"attempt_slug": "retry-attempt-002",
					"status": "queued",
					"decision_reason": "data_source_retryable_failure",
					"trigger_slug": "daily-users",
					"trigger_revision": 7,
					"pipeline_slug": "users-sync",
					"root_run_slug": "run-root-001",
					"retry_of_run_slug": "run-retry-001",
					"run_slug": "run-retry-002",
					"attempt_number": 3,
					"failure_kind": "data_source",
					"source_event_slug": "evt-retry-002",
					"scheduled_at": "2026-06-12T07:05:00Z",
					"enqueued_at": "2026-06-12T07:05:01Z",
					"started_at": null,
					"completed_at": null,
					"error_summary": null,
					"created_at": "2026-06-12T07:05:00Z",
					"updated_at": "2026-06-12T07:05:01Z"
				}
			]
		}`))
	}))
	t.Cleanup(server.Close)

	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		APIKey:       "xto_saved_token",
		Environment:  "prod",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane", "pipeline", "run", "retry-attempts", "run-root-001",
			"--project", "data-pipelines",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Equal(t, http.MethodGet, gotMethod)
	require.Equal(t, "Bearer xto_saved_token", gotAuth)
	require.Equal(
		t,
		"/api/organizations/acme/projects/data-pipelines/environments/prod/runs/run-root-001/trigger-retry-attempts",
		gotPath,
	)
	require.Contains(t, stdout.String(), `"attempt_slug": "retry-attempt-002"`)
	require.Contains(t, stdout.String(), `"attempt_number": 3`)
	require.Contains(t, stdout.String(), `"root_run_slug": "run-root-001"`)
	require.Contains(t, stdout.String(), `"retry_of_run_slug": "run-retry-001"`)
	require.NotContains(t, stdout.String(), "Authorization")
	require.NotContains(t, stdout.String(), "Bearer")
	require.NotContains(t, stdout.String(), "secret-token")
}

func TestRunWithArgsPipelineTriggerCommands(t *testing.T) {
	t.Parallel()

	type pipelineTriggerCommandState struct {
		auth         string
		method       string
		path         string
		limit        string
		cursor       string
		createBody   api.NewPipelineTrigger
		updateBody   api.UpdatePipelineTrigger
		decodeErr    error
		deleteCalled bool
	}

	tests := []struct {
		name    string
		args    []string
		handler func(*testing.T, *pipelineTriggerCommandState, http.ResponseWriter, *http.Request)
		assert  func(*testing.T, *pipelineTriggerCommandState, string, string)
	}{
		{
			name: "CreatePayloadFields",
			args: []string{
				"braidplane", "pipeline", "trigger", "create",
				"--project", "data-pipelines",
				"--name", "Daily Users",
				"--slug", "daily-users",
				"--pipeline", "users-sync",
				"--enabled",
				"--version-policy", "pinned",
				"--pinned-version-hash", "short-hash",
				"--runtime-input-key", "tenant_id",
				"--runtime-input-key", "dry_run",
				"--cron", "0 7 * * *",
				"--timezone", "UTC",
				"--misfire-policy", "fire_once",
				"--retry-enabled",
				"--retry-max-attempts", "3",
				"--retry-backoff", "fixed",
				"--retry-initial-delay-seconds", "60",
				"--retry-max-delay-seconds", "300",
				"--retry-jitter",
				"--retry-failure-kind", "platform",
				"--retry-failure-kind", "data_source",
				"--retry-max-elapsed-seconds", "3600",
			},
			handler: func(_ *testing.T, state *pipelineTriggerCommandState, w http.ResponseWriter, r *http.Request) {
				state.auth = r.Header.Get("Authorization")
				state.method = r.Method
				state.path = r.URL.Path
				state.decodeErr = json.NewDecoder(r.Body).Decode(&state.createBody)
				w.Header().Set("Content-Type", "application/json")
				if state.decodeErr != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"pipeline_trigger":` + pipelineTriggerResponseJSON("daily-users", true) + `}`))
			},
			assert: func(t *testing.T, state *pipelineTriggerCommandState, stdout, stderr string) {
				t.Helper()

				require.Empty(t, stderr)
				require.NoError(t, state.decodeErr)
				require.Equal(t, http.MethodPost, state.method)
				require.Equal(t, "Bearer xto_saved_token", state.auth)
				require.Equal(t, "/api/organizations/acme/projects/data-pipelines/environments/prod/pipeline-triggers", state.path)
				require.Equal(t, "Daily Users", state.createBody.Name)
				require.NotNil(t, state.createBody.Slug)
				require.Equal(t, "daily-users", *state.createBody.Slug)
				require.Equal(t, api.PipelineTriggerTypeSchedule, state.createBody.Type)
				require.True(t, state.createBody.Enabled)
				require.Equal(t, api.Slug("users-sync"), state.createBody.PipelineSlug)
				require.Equal(t, api.PipelineTriggerVersionPolicyModePinned, state.createBody.VersionPolicy.Mode)
				require.NotNil(t, state.createBody.VersionPolicy.PinnedVersionHash)
				require.Equal(t, "short-hash", *state.createBody.VersionPolicy.PinnedVersionHash)
				require.Equal(t, []string{"dry_run", "tenant_id"}, state.createBody.RuntimeInputKeys)
				require.Equal(t, "0 7 * * *", state.createBody.Schedule.Cron)
				require.Equal(t, "UTC", state.createBody.Schedule.Timezone)
				require.Equal(t, api.PipelineTriggerScheduleMisfirePolicyFireOnce, state.createBody.Schedule.MisfirePolicy)
				require.True(t, state.createBody.RetryPolicy.Enabled)
				require.Equal(t, 3, state.createBody.RetryPolicy.MaxAttempts)
				require.Equal(t, api.PipelineTriggerBackoffStrategyFixed, state.createBody.RetryPolicy.BackoffStrategy)
				require.Equal(t, 60, state.createBody.RetryPolicy.InitialDelaySeconds)
				require.Equal(t, 300, state.createBody.RetryPolicy.MaxDelaySeconds)
				require.True(t, state.createBody.RetryPolicy.Jitter)
				require.Equal(
					t,
					[]api.PipelineTriggerRetryFailureKind{
						api.PipelineTriggerRetryFailureKindDataSource,
						api.PipelineTriggerRetryFailureKindPlatform,
					},
					state.createBody.RetryPolicy.RetryableFailureKinds,
				)
				require.NotNil(t, state.createBody.RetryPolicy.MaxElapsedSeconds)
				require.Equal(t, 3600, *state.createBody.RetryPolicy.MaxElapsedSeconds)
				require.Contains(t, stdout, `"slug": "daily-users"`)
				require.NotContains(t, stdout, "short-hash")
			},
		},
		{
			name: "CreateWebhookPrintsOneTimeSecret",
			args: []string{
				"braidplane", "pipeline", "trigger", "create",
				"--project", "data-pipelines",
				"--name", "Customer Event",
				"--slug", "customer-event",
				"--pipeline", "users-sync",
				"--type", "webhook",
				"--disabled",
				"--version-policy", "latest_deployed",
				"--runtime-input-key", "tenant_id",
				"--runtime-input-key", "event_type",
				"--webhook-input-mapping", "/tenant/id=tenant_id",
				"--webhook-input-mapping", "/type=event_type",
			},
			handler: func(_ *testing.T, state *pipelineTriggerCommandState, w http.ResponseWriter, r *http.Request) {
				state.auth = r.Header.Get("Authorization")
				state.method = r.Method
				state.path = r.URL.Path
				state.decodeErr = json.NewDecoder(r.Body).Decode(&state.createBody)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"pipeline_trigger":` + pipelineWebhookTriggerResponseJSON("customer-event", false) + `,"webhook_secret":{"key_version":1,"value":"webhook-one-time-secret-value-abcdefghijklmnopqrstuvwxyz"}}`))
			},
			assert: func(t *testing.T, state *pipelineTriggerCommandState, stdout, stderr string) {
				t.Helper()

				require.Empty(t, stderr)
				require.NoError(t, state.decodeErr)
				require.Equal(t, http.MethodPost, state.method)
				require.Equal(t, api.PipelineTriggerTypeWebhook, state.createBody.Type)
				require.Nil(t, state.createBody.Schedule)
				require.NotNil(t, state.createBody.Webhook)
				require.Equal(t, []api.PipelineWebhookInputMapping{
					{JsonPointer: "/tenant/id", RuntimeInputKey: "tenant_id"},
					{JsonPointer: "/type", RuntimeInputKey: "event_type"},
				}, state.createBody.Webhook.InputMappings)
				require.Contains(t, stdout, `"value": "webhook-one-time-secret-value-abcdefghijklmnopqrstuvwxyz"`)
				require.NotContains(t, stdout, "must-not-print")
			},
		},
		{
			name: "ListPagination",
			args: []string{
				"braidplane", "pipeline", "trigger", "list",
				"--project", "data-pipelines",
				"--limit", "2",
				"--cursor", "cursor-1",
			},
			handler: func(_ *testing.T, state *pipelineTriggerCommandState, w http.ResponseWriter, r *http.Request) {
				state.auth = r.Header.Get("Authorization")
				state.method = r.Method
				state.path = r.URL.Path
				state.limit = r.URL.Query().Get("limit")
				state.cursor = r.URL.Query().Get("cursor")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{
					"pipeline_triggers": [` + pipelineTriggerResponseJSON("daily-users", false) + `],
					"has_more": false,
					"next_cursor": null,
					"prev_cursor": null
				}`))
			},
			assert: func(t *testing.T, state *pipelineTriggerCommandState, stdout, stderr string) {
				t.Helper()

				require.Empty(t, stderr)
				require.Equal(t, http.MethodGet, state.method)
				require.Equal(t, "Bearer xto_saved_token", state.auth)
				require.Equal(t, "/api/organizations/acme/projects/data-pipelines/environments/prod/pipeline-triggers", state.path)
				require.Equal(t, "2", state.limit)
				require.Equal(t, "cursor-1", state.cursor)
				require.Contains(t, stdout, `"pipeline_triggers": [`)
				require.Contains(t, stdout, `"slug": "daily-users"`)
			},
		},
		{
			name: "ListWebhookRedactsUnexpectedSecret",
			args: []string{
				"braidplane", "pipeline", "trigger", "list",
				"--project", "data-pipelines",
			},
			handler: func(_ *testing.T, state *pipelineTriggerCommandState, w http.ResponseWriter, r *http.Request) {
				state.method = r.Method
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"pipeline_triggers":[` + pipelineWebhookTriggerResponseJSON("customer-event", false) + `],"has_more":false}`))
			},
			assert: func(t *testing.T, state *pipelineTriggerCommandState, stdout, stderr string) {
				t.Helper()

				require.Empty(t, stderr)
				require.Equal(t, http.MethodGet, state.method)
				require.Contains(t, stdout, `"type": "webhook"`)
				require.NotContains(t, stdout, "must-not-print")
			},
		},
		{
			name: "GetBySlug",
			args: []string{
				"braidplane", "pipeline", "trigger", "get", "daily-users",
				"--project", "data-pipelines",
			},
			handler: func(_ *testing.T, state *pipelineTriggerCommandState, w http.ResponseWriter, r *http.Request) {
				state.auth = r.Header.Get("Authorization")
				state.method = r.Method
				state.path = r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(pipelineTriggerResponseJSON("daily-users", false)))
			},
			assert: func(t *testing.T, state *pipelineTriggerCommandState, stdout, stderr string) {
				t.Helper()

				require.Empty(t, stderr)
				require.Equal(t, http.MethodGet, state.method)
				require.Equal(t, "Bearer xto_saved_token", state.auth)
				require.Equal(
					t,
					"/api/organizations/acme/projects/data-pipelines/environments/prod/pipeline-triggers/daily-users",
					state.path,
				)
				require.Contains(t, stdout, `"enabled": false`)
			},
		},
		{
			name: "GetWebhookRedactsUnexpectedSecret",
			args: []string{
				"braidplane", "pipeline", "trigger", "get", "customer-event",
				"--project", "data-pipelines",
			},
			handler: func(_ *testing.T, state *pipelineTriggerCommandState, w http.ResponseWriter, r *http.Request) {
				state.method = r.Method
				state.path = r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(pipelineWebhookTriggerResponseJSON("customer-event", false)))
			},
			assert: func(t *testing.T, state *pipelineTriggerCommandState, stdout, stderr string) {
				t.Helper()

				require.Empty(t, stderr)
				require.Equal(t, http.MethodGet, state.method)
				require.Contains(t, stdout, `"receiver_id": "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"`)
				require.NotContains(t, stdout, "must-not-print")
			},
		},
		{
			name: "UpdatePayload",
			args: []string{
				"braidplane", "pipeline", "trigger", "update", "daily-users",
				"--project", "data-pipelines",
				"--name", "Daily Users Updated",
				"--pipeline", "users-sync",
				"--enabled",
				"--version-policy", "latest_deployed",
				"--runtime-input-key", "customer_id",
				"--cron", "0 * * * *",
				"--timezone", "Europe/Athens",
				"--misfire-policy", "skip",
			},
			handler: func(_ *testing.T, state *pipelineTriggerCommandState, w http.ResponseWriter, r *http.Request) {
				state.auth = r.Header.Get("Authorization")
				state.method = r.Method
				state.path = r.URL.Path
				state.decodeErr = json.NewDecoder(r.Body).Decode(&state.updateBody)
				w.Header().Set("Content-Type", "application/json")
				if state.decodeErr != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				_, _ = w.Write([]byte(pipelineTriggerResponseJSON("daily-users", true)))
			},
			assert: func(t *testing.T, state *pipelineTriggerCommandState, stdout, stderr string) {
				t.Helper()

				require.Empty(t, stderr)
				require.NoError(t, state.decodeErr)
				require.Equal(t, http.MethodPut, state.method)
				require.Equal(t, "Bearer xto_saved_token", state.auth)
				require.Equal(
					t,
					"/api/organizations/acme/projects/data-pipelines/environments/prod/pipeline-triggers/daily-users",
					state.path,
				)
				require.Equal(t, "Daily Users Updated", state.updateBody.Name)
				require.Equal(t, api.PipelineTriggerTypeSchedule, state.updateBody.Type)
				require.True(t, state.updateBody.Enabled)
				require.Equal(t, api.Slug("users-sync"), state.updateBody.PipelineSlug)
				require.Equal(t, api.PipelineTriggerVersionPolicyModeLatestDeployed, state.updateBody.VersionPolicy.Mode)
				require.Nil(t, state.updateBody.VersionPolicy.PinnedVersionHash)
				require.Equal(t, []string{"customer_id"}, state.updateBody.RuntimeInputKeys)
				require.False(t, state.updateBody.RetryPolicy.Enabled)
				require.Equal(t, 1, state.updateBody.RetryPolicy.MaxAttempts)
				require.Equal(t, api.PipelineTriggerBackoffStrategyNone, state.updateBody.RetryPolicy.BackoffStrategy)
				require.Equal(t, []api.PipelineTriggerRetryFailureKind{}, state.updateBody.RetryPolicy.RetryableFailureKinds)
				require.Contains(t, stdout, `"enabled": true`)
			},
		},
		{
			name: "UpdateWebhookDoesNotPrintSecret",
			args: []string{
				"braidplane", "pipeline", "trigger", "update", "customer-event",
				"--project", "data-pipelines",
				"--name", "Customer Event Updated",
				"--pipeline", "users-sync",
				"--type", "webhook",
				"--disabled",
				"--version-policy", "latest_deployed",
				"--runtime-input-key", "tenant_id",
				"--webhook-input-mapping", "/tenant/id=tenant_id",
			},
			handler: func(_ *testing.T, state *pipelineTriggerCommandState, w http.ResponseWriter, r *http.Request) {
				state.method = r.Method
				state.path = r.URL.Path
				state.decodeErr = json.NewDecoder(r.Body).Decode(&state.updateBody)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(pipelineWebhookTriggerResponseJSON("customer-event", false)))
			},
			assert: func(t *testing.T, state *pipelineTriggerCommandState, stdout, stderr string) {
				t.Helper()

				require.Empty(t, stderr)
				require.NoError(t, state.decodeErr)
				require.Equal(t, http.MethodPut, state.method)
				require.Equal(t, api.PipelineTriggerTypeWebhook, state.updateBody.Type)
				require.Nil(t, state.updateBody.Schedule)
				require.NotNil(t, state.updateBody.Webhook)
				require.Contains(t, stdout, `"type": "webhook"`)
				require.NotContains(t, stdout, "must-not-print")
			},
		},
		{
			name: "RotateWebhookSecretPrintsOneTimeSecret",
			args: []string{
				"braidplane", "pipeline", "trigger", "webhook-secret", "rotate", "customer-event",
				"--project", "data-pipelines",
			},
			handler: func(_ *testing.T, state *pipelineTriggerCommandState, w http.ResponseWriter, r *http.Request) {
				state.auth = r.Header.Get("Authorization")
				state.method = r.Method
				state.path = r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"key_version":2,"value":"rotated-one-time-secret-value-abcdefghijklmnopqrstuvwxyz"}`))
			},
			assert: func(t *testing.T, state *pipelineTriggerCommandState, stdout, stderr string) {
				t.Helper()

				require.Empty(t, stderr)
				require.Equal(t, http.MethodPost, state.method)
				require.Equal(t, "/api/organizations/acme/projects/data-pipelines/environments/prod/pipeline-triggers/customer-event/webhook-secret/rotate", state.path)
				require.Contains(t, stdout, `"key_version": 2`)
				require.Contains(t, stdout, `"value": "rotated-one-time-secret-value-abcdefghijklmnopqrstuvwxyz"`)
			},
		},
		{
			name: "DeleteNoContent",
			args: []string{
				"braidplane", "pipeline", "trigger", "delete", "daily-users",
				"--project", "data-pipelines",
			},
			handler: func(_ *testing.T, state *pipelineTriggerCommandState, w http.ResponseWriter, r *http.Request) {
				state.auth = r.Header.Get("Authorization")
				state.method = r.Method
				state.path = r.URL.Path
				state.deleteCalled = true
				w.WriteHeader(http.StatusNoContent)
			},
			assert: func(t *testing.T, state *pipelineTriggerCommandState, stdout, stderr string) {
				t.Helper()

				require.Equal(t, http.MethodDelete, state.method)
				require.Equal(t, "Bearer xto_saved_token", state.auth)
				require.Equal(
					t,
					"/api/organizations/acme/projects/data-pipelines/environments/prod/pipeline-triggers/daily-users",
					state.path,
				)
				require.True(t, state.deleteCalled)
				require.Empty(t, stdout)
				require.Empty(t, stderr)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			configPath := filepath.Join(t.TempDir(), "braidplane.toml")
			state := &pipelineTriggerCommandState{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				test.handler(t, state, w, r)
			}))
			t.Cleanup(server.Close)

			require.NoError(t, saveLogin(configPath, cliAuthConfig{
				ServerURL:    server.URL + "/api",
				Organization: "acme",
				APIKey:       "xto_saved_token",
				Environment:  "prod",
			}))

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			err := runWithRuntime(test.args, &stdout, &stderr, cliRuntime{configPath: configPath})

			require.NoError(t, err)
			test.assert(t, state, stdout.String(), stderr.String())
		})
	}
}

func TestRunWithArgsPipelineTriggerCreateRejectsInvalidInputsBeforeAPIRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		extraArgs   []string
		errContains string
	}{
		{
			name: "MissingEnabledState",
			extraArgs: []string{
				"--version-policy", "latest_deployed",
			},
			errContains: "set --enabled or --disabled",
		},
		{
			name: "ConflictingEnabledState",
			extraArgs: []string{
				"--enabled",
				"--disabled",
				"--version-policy", "latest_deployed",
			},
			errContains: "only one of --enabled or --disabled can be set",
		},
		{
			name: "PinnedVersionRequiresHash",
			extraArgs: []string{
				"--disabled",
				"--version-policy", "pinned",
			},
			errContains: "pinned version policy requires --pinned-version-hash",
		},
		{
			name: "DuplicateRuntimeInputKey",
			extraArgs: []string{
				"--disabled",
				"--version-policy", "latest_deployed",
				"--runtime-input-key", "customer_id",
				"--runtime-input-key", "customer_id",
			},
			errContains: "runtime input key \"customer_id\" was provided more than once",
		},
		{
			name: "SecretRuntimeInputKey",
			extraArgs: []string{
				"--disabled",
				"--version-policy", "latest_deployed",
				"--runtime-input-key", "api_token",
			},
			errContains: "runtime input values are non-secret",
		},
		{
			name: "InvalidRetryFailureKind",
			extraArgs: []string{
				"--disabled",
				"--version-policy", "latest_deployed",
				"--retry-failure-kind", "network",
			},
			errContains: "retry-failure-kind must be data_source or platform",
		},
		{
			name: "ScheduleRejectsWebhookMapping",
			extraArgs: []string{
				"--disabled",
				"--version-policy", "latest_deployed",
				"--runtime-input-key", "customer_id",
				"--webhook-input-mapping", "/customer/id=customer_id",
			},
			errContains: "schedule triggers must not set --webhook-input-mapping",
		},
		{
			name: "WebhookRejectsScheduleFlags",
			extraArgs: []string{
				"--type", "webhook",
				"--disabled",
				"--version-policy", "latest_deployed",
			},
			errContains: "webhook triggers must not set --cron, --timezone, or --misfire-policy",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			configPath := filepath.Join(t.TempDir(), "braidplane.toml")
			apiCalled := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				apiCalled = true
				w.WriteHeader(http.StatusInternalServerError)
			}))
			t.Cleanup(server.Close)
			require.NoError(t, saveLogin(configPath, cliAuthConfig{
				ServerURL:    server.URL + "/api",
				Organization: "acme",
				APIKey:       "xto_saved_token",
				Environment:  "prod",
			}))

			args := []string{
				"braidplane", "pipeline", "trigger", "create",
				"--project", "data-pipelines",
				"--name", "Daily Users",
				"--pipeline", "users-sync",
				"--cron", "0 7 * * *",
				"--timezone", "UTC",
				"--misfire-policy", "skip",
			}
			args = append(args, test.extraArgs...)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			err := runWithRuntime(args, &stdout, &stderr, cliRuntime{configPath: configPath})

			require.Error(t, err)
			require.ErrorContains(t, err, test.errContains)
			require.False(t, apiCalled)
			require.Empty(t, stdout.String())
			require.Empty(t, stderr.String())
		})
	}
}

func TestPipelineTriggerRuntimeInputKeysReturnsJSONEmptyArrayWhenUnset(t *testing.T) {
	t.Parallel()

	keys, err := pipelineTriggerRuntimeInputKeys(nil)

	require.NoError(t, err)
	require.NotNil(t, keys)
	require.Empty(t, keys)
	encoded, err := json.Marshal(keys)
	require.NoError(t, err)
	require.JSONEq(t, `[]`, string(encoded))
}

func TestPipelineWebhookInputMappingsRejectInvalidMappings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mappings    []string
		errContains string
	}{
		{name: "MissingSeparator", mappings: []string{"/tenant/id"}, errContains: "must use <json-pointer>=<runtime-input-key>"},
		{name: "InvalidPointer", mappings: []string{"tenant/id=tenant_id"}, errContains: "webhook json pointer is invalid"},
		{name: "UndeclaredKey", mappings: []string{"/tenant/id=customer_id"}, errContains: `runtime input "customer_id" is not declared`},
		{name: "DuplicatePointer", mappings: []string{"/tenant/id=tenant_id", "/tenant/id=event_type"}, errContains: "webhook json pointer mappings must be unique"},
		{name: "DuplicateKey", mappings: []string{"/tenant/id=tenant_id", "/customer/id=tenant_id"}, errContains: "webhook runtime input mappings must be unique"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := pipelineWebhookInputMappings(test.mappings, []string{"event_type", "tenant_id"})

			require.Error(t, err)
			require.ErrorContains(t, err, test.errContains)
		})
	}
}

func TestPipelineWebhookDeliveryDiagnosticsDoNotPrintSecrets(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/organizations/acme/projects/data-pipelines/environments/prod/pipeline-triggers/customer-event/deliveries/delivery-1" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"delivery_id":"delivery-1","status":"failed","webhook_secret":"must-not-print"}`))
	}))
	t.Cleanup(server.Close)

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		APIKey:       "xto_saved_token",
		Environment:  "prod",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime([]string{
		"braidplane", "pipeline", "trigger", "delivery", "get", "delivery-1",
		"--project", "data-pipelines",
		"--environment", "prod",
		"--trigger", "customer-event",
	}, &stdout, &stderr, cliRuntime{configPath: configPath})

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Contains(t, stdout.String(), `"delivery_id": "delivery-1"`)
	require.NotContains(t, stdout.String(), "must-not-print")
}

func TestRunWithArgsPipelineRunGetUsesEnvFallbackAndPrintsRun(t *testing.T) {
	var gotAuth string
	var gotPath string
	var gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"metadata": {
				"pipeline_slug": "acme-users-sync",
				"pipeline_version_spec_hash": "spec-hash-12",
				"run_slug": "run-001",
				"request_id": "req-001",
				"status": "failed",
				"duration_ms": 245,
				"step_count": 3,
				"step_slug": "load-users",
				"error_summary": "HubSpot request failed: query invalid input",
				"failure_kind": "data_source",
				"started_at": "2026-05-15T08:00:00Z",
				"ended_at": "2026-05-15T08:00:00.245Z",
				"replay_of_run_slug": null,
				"replay_from_step": null,
				"created_at": "2026-05-15T08:00:00Z"
			},
			"timeline": {
				"run_slug": "run-001",
				"pipeline_slug": "acme-users-sync",
				"status": "failed",
				"replay_of_run_slug": null,
				"events": [
					{
						"at": "2026-05-15T08:00:00Z",
						"type": "step_failed",
						"step": "load-users",
						"status": "failed"
					}
				],
				"snapshots": [
					{
						"step": "load-users",
						"input": {
							"organization_slug": "acme",
							"data_source_slug": "acme-db",
							"operation_type": "sql",
							"operation_ref": "load-users",
							"uses_inline": false,
							"parameter_keys": ["limit"]
						},
						"output": {
							"result_type": "sql",
							"vendor_type": "postgres",
							"duration_ms": 120,
							"details": {
								"kind": "rows",
								"row_count": 10,
								"column_names": ["id", "email"]
							}
						}
					},
					{
						"step": "write-joined-orders",
						"kind": "sink",
						"input": {
							"organization_slug": "acme",
							"data_source_slug": "golden-path-s3-sink",
							"operation_type": "s3",
							"operation_ref": "golden-path-s3-write-joined-orders",
							"uses_inline": false,
							"parameter_keys": []
						},
						"output": {
							"result_type": "s3",
							"vendor_type": "s3",
							"duration_ms": 42,
							"operation_type": "write",
							"details": {
								"kind": "file_write",
								"target_path": "golden-path/runs/golden-path-run-001/joined-orders.json",
								"written_rows": 2,
								"written_bytes": 256
							}
						}
					}
				],
				"failure": {
					"step": "load-users",
					"error": "HubSpot request failed: query invalid input",
					"at": "2026-05-15T08:00:00.245Z",
					"kind": "data_source",
					"side": "sink",
					"data_source_slug": "warehouse",
					"action_hint": "Review the sink insert strategy before retrying."
				}
			}
		}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_API_KEY", "xto_env_token")
	t.Setenv("BRAIDPLANE_ENVIRONMENT", "prod")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "run", "get", "run-001", "--project", "data-pipelines"},
		&stdout,
		&stderr,
		cliRuntime{configPath: filepath.Join(t.TempDir(), "braidplane.toml")},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Equal(t, http.MethodGet, gotMethod)
	require.Equal(t, "Bearer xto_env_token", gotAuth)
	require.Equal(t, "/api/organizations/acme/projects/data-pipelines/environments/prod/runs/run-001", gotPath)
	require.Contains(t, stdout.String(), "run-001")
	require.Contains(t, stdout.String(), `"error_summary": "HubSpot request failed: query invalid input"`)
	require.Contains(t, stdout.String(), `"error": "HubSpot request failed: query invalid input"`)
	require.Contains(t, stdout.String(), "spec-hash-12")
	require.Contains(t, stdout.String(), "load-users")
	require.Contains(t, stdout.String(), `"written_rows": 2`)
	require.Contains(t, stdout.String(), `"target_path": "golden-path/runs/golden-path-run-001/joined-orders.json"`)
	require.Contains(t, stdout.String(), "Review the sink insert strategy")
}

func TestRunWithArgsPipelineRunGetIncludesIncrementalDiagnosticsInJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want %s", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/api/organizations/acme/projects/data-pipelines/environments/prod/runs/run-incremental-001" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"metadata": {
				"pipeline_slug": "users-sync",
				"pipeline_version_spec_hash": "spec-hash-12",
				"run_slug": "run-incremental-001",
				"request_id": "req-001",
				"status": "succeeded",
				"duration_ms": 245,
				"step_count": 2,
				"step_slug": "persist-users",
				"error_summary": null,
				"failure_kind": null,
				"started_at": "2026-05-24T11:00:00Z",
				"ended_at": "2026-05-24T11:00:00.245Z",
				"replay_of_run_slug": null,
				"replay_from_step": null,
				"created_at": "2026-05-24T11:00:00Z",
				"incremental": {
					"mode": "incremental",
					"checkpoint_status": "committed",
					"source_step_slug": "fetch-users",
					"replay_safety_status": "safe"
				}
			},
			"timeline": {
				"run_slug": "run-incremental-001",
				"pipeline_slug": "users-sync",
				"status": "succeeded",
				"replay_of_run_slug": null,
				"events": [
					{"at":"2026-05-24T11:00:00Z","type":"checkpoint_observed","step":"fetch-users","status":"observed"},
					{"at":"2026-05-24T11:00:00Z","type":"checkpoint_committed","step":"fetch-users","status":"committed"}
				],
				"snapshots": [],
				"failure": null
			},
			"incremental_diagnostics": {
				"sources": [
					{
						"source_step_slug": "fetch-users",
						"checkpoint_kind": "watermark",
						"previous_checkpoint": {"summary":"up to 10:00","raw_value_exposed":false},
						"observed_checkpoint": {"summary":"up to 11:00","raw_value_exposed":false},
						"commit_result": {"status":"committed"}
					}
				],
				"replay_safety": {"status":"safe"}
			}
		}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_API_KEY", "xto_env_token")
	t.Setenv("BRAIDPLANE_ENVIRONMENT", "prod")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "run", "get", "run-incremental-001", "--project", "data-pipelines"},
		&stdout,
		&stderr,
		cliRuntime{configPath: filepath.Join(t.TempDir(), "braidplane.toml")},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Contains(t, stdout.String(), `"incremental"`)
	require.Contains(t, stdout.String(), `"checkpoint_status": "committed"`)
	require.Contains(t, stdout.String(), `"incremental_diagnostics"`)
	require.Contains(t, stdout.String(), `"summary": "up to 11:00"`)
	require.NotContains(t, stdout.String(), "xto_secret")
}

func TestRunWithArgsPipelineRunGetTextSummarizesBlockedIncrementalRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"metadata": {
				"pipeline_slug": "users-sync",
				"pipeline_version_spec_hash": "spec-hash-12",
				"run_slug": "run-blocked-001",
				"source": {
					"type": "api",
					"summary": "API key run",
					"entrypoint": "api",
					"actor_type": "api_key",
					"actor_slug": "etl-worker",
					"request_id": "req-001",
					"trace_id": null,
					"parameter_keys": ["run_slug"],
					"source_event_slug": "evt-pipeline-run-execute-001"
				},
				"request_id": "req-001",
				"status": "failed",
				"duration_ms": 245,
				"step_count": 2,
				"step_slug": "persist-users",
				"error_summary": "sink write failed",
				"failure_kind": "data_source",
				"started_at": "2026-05-24T11:00:00Z",
				"ended_at": "2026-05-24T11:00:00.245Z",
				"replay_of_run_slug": null,
				"replay_from_step": null,
				"created_at": "2026-05-24T11:00:00Z",
				"incremental": {
					"mode": "incremental",
					"checkpoint_status": "blocked",
					"source_step_slug": "fetch-users",
					"blocked_reason": "sink write failed",
					"replay_safety_status": "blocked",
					"replay_blocked_reason": "append-only sink append-audit is not replay safe"
				}
			},
			"timeline": {
				"run_slug": "run-blocked-001",
				"pipeline_slug": "users-sync",
				"status": "failed",
				"replay_of_run_slug": null,
				"events": [],
				"snapshots": [],
				"failure": {
					"step":"persist-users",
					"error":"contract \"users_rows\" field \"email\" failed format validation (2 violation(s))",
					"at":"2026-05-24T11:00:00Z",
					"kind":"platform",
					"side":"sink",
					"reason_code":"sink_contract_mismatch",
					"data_source_slug":"warehouse",
					"action_hint":"Review the reported contract field and validation rule.",
					"contract": {
						"contract_name":"users_rows",
						"field_name":"email",
						"rule":"format",
						"violation_count":2,
						"reason_code":"contract_field_validation_failed"
					}
				}
			},
			"incremental_diagnostics": {
				"sources": [
					{
						"source_step_slug": "fetch-users",
						"checkpoint_kind": "watermark",
						"window": {"summary":"up to 11:00"},
						"observed_checkpoint": {"summary":"up to 11:00","raw_value_exposed":false},
						"commit_result": {"status":"blocked","blocked_by_step":"persist-users","blocked_reason":"sink write failed"}
					}
				],
				"replay_safety": {"status":"blocked","blocked_step_slug":"append-audit","blocked_reason":"append-only sink append-audit is not replay safe"}
			}
		}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_API_KEY", "xto_env_token")
	t.Setenv("BRAIDPLANE_ENVIRONMENT", "prod")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "run", "get", "run-blocked-001", "--project", "data-pipelines", "--output", "text"},
		&stdout,
		&stderr,
		cliRuntime{configPath: filepath.Join(t.TempDir(), "braidplane.toml")},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Contains(t, stdout.String(), "Run: run-blocked-001")
	require.Contains(t, stdout.String(), "Source: API key run via api")
	require.Contains(t, stdout.String(), "Source actor: api_key etl-worker")
	require.Contains(t, stdout.String(), "Source request: request=req-001")
	require.Contains(t, stdout.String(), "Source parameters: run_slug")
	require.Contains(t, stdout.String(), "Mode: incremental")
	require.Contains(t, stdout.String(), "Checkpoint: blocked")
	require.Contains(t, stdout.String(), "Failure step: persist-users")
	require.Contains(t, stdout.String(), "Failure kind: platform")
	require.Contains(t, stdout.String(), "Failure side: sink")
	require.Contains(t, stdout.String(), "Failure reason: sink_contract_mismatch")
	require.Contains(t, stdout.String(), "Failure data source: warehouse")
	require.Contains(t, stdout.String(), "Contract failure: contract=users_rows field=email rule=format violations=2 reason=contract_field_validation_failed")
	require.Contains(t, stdout.String(), "fetch-users: up to 11:00")
	require.Contains(t, stdout.String(), "Blocked by persist-users: sink write failed")
	require.Contains(t, stdout.String(), "Replay safety: blocked")
	require.Contains(t, stdout.String(), "append-only sink append-audit is not replay safe")
	require.NotContains(t, stdout.String(), "{")
}

func TestWritePipelineRunOutputTextSummarizesSourceAndRetryContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   string
		expected []string
	}{
		{
			name: "ManualFirstAttempt",
			source: `{
				"type": "manual",
				"summary": "Manual run",
				"entrypoint": "ui",
				"actor_type": "user",
				"actor_slug": "admin-user",
				"request_id": "req-manual-001",
				"trace_id": "trace-manual-001",
				"parameter_keys": ["run_slug"],
				"source_event_slug": "evt-manual-001",
				"attempt_number": 1,
				"root_run_slug": "run-manual-001",
				"retry_of_run_slug": null
			}`,
			expected: []string{
				"Source: Manual run via ui",
				"Source actor: user admin-user",
				"Source request: request=req-manual-001 trace=trace-manual-001",
				"Source parameters: run_slug",
				"Attempt: 1",
				"Root run: run-manual-001",
				"Retry of: none",
			},
		},
		{
			name: "APIRetryAttempt",
			source: `{
				"type": "api",
				"summary": "API key run",
				"entrypoint": "api",
				"actor_type": "api_key",
				"actor_slug": "etl-worker",
				"request_id": "req-api-002",
				"trace_id": "trace-api-002",
				"parameter_keys": ["customer_id", "dry_run"],
				"source_event_slug": "evt-api-002",
				"attempt_number": 3,
				"root_run_slug": "run-api-root",
				"retry_of_run_slug": "run-api-retry-1"
			}`,
			expected: []string{
				"Source: API key run via api",
				"Source actor: api_key etl-worker",
				"Source request: request=req-api-002 trace=trace-api-002",
				"Source parameters: customer_id, dry_run",
				"Attempt: 3",
				"Root run: run-api-root",
				"Retry of: run-api-retry-1",
			},
		},
		{
			name: "ScheduleRetryAttempt",
			source: `{
				"type": "schedule",
				"summary": "Scheduled run from trigger daily-users",
				"entrypoint": "scheduler",
				"actor_type": "system",
				"actor_slug": "scheduler",
				"request_id": "req-schedule-002",
				"trace_id": "trace-schedule-002",
				"parameter_keys": ["customer_id", "dry_run"],
				"source_event_slug": "evt-schedule-002",
				"attempt_number": 2,
				"root_run_slug": "run-schedule-root",
				"retry_of_run_slug": "run-schedule-root",
				"schedule": {
					"trigger_slug": "daily-users",
					"trigger_revision": 7,
					"cron": "0 7 * * *",
					"timezone": "UTC",
					"scheduled_fire_at": "2026-06-12T07:00:00Z",
					"actual_fire_at": "2026-06-12T07:00:05Z",
					"misfire_result": "on_time"
				}
			}`,
			expected: []string{
				"Source: Scheduled run from trigger daily-users via scheduler",
				"Source actor: system scheduler",
				"Source request: request=req-schedule-002 trace=trace-schedule-002",
				"Source parameters: customer_id, dry_run",
				"Attempt: 2",
				"Root run: run-schedule-root",
				"Retry of: run-schedule-root",
				"Schedule: trigger=daily-users revision=7 cron=\"0 7 * * *\" timezone=UTC",
				"Schedule fire: scheduled=2026-06-12T07:00:00Z actual=2026-06-12T07:00:05Z misfire=on_time",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var run api.PipelineRunRecord
			require.NoError(t, json.Unmarshal([]byte(pipelineRunRecordJSON("users-sync", "run-text-001", test.source)), &run))

			var stdout bytes.Buffer
			err := writePipelineRunOutput(&stdout, &run, "text")

			require.NoError(t, err)
			for _, expected := range test.expected {
				require.Contains(t, stdout.String(), expected)
			}
			require.NotContains(t, stdout.String(), "cus_123")
			require.NotContains(t, stdout.String(), "request_body")
			require.NotContains(t, stdout.String(), "Authorization")
			require.NotContains(t, stdout.String(), "secret-token")
		})
	}
}

func pipelineRunRecordJSON(pipelineSlug, runSlug, source string) string {
	return `{
		"metadata": {
			"pipeline_slug": "` + pipelineSlug + `",
			"environment_slug": "prod",
			"pipeline_version_spec_hash": "spec-hash-12",
			"run_slug": "` + runSlug + `",
			"source": ` + source + `,
			"request_id": "req-001",
			"status": "succeeded",
			"duration_ms": 245,
			"step_count": 2,
			"step_slug": "persist-users",
			"error_summary": null,
			"failure_kind": null,
			"started_at": "2026-06-12T07:00:00Z",
			"ended_at": "2026-06-12T07:00:00.245Z",
			"replay_of_run_slug": null,
			"replay_from_step": null,
			"created_at": "2026-06-12T07:00:00Z",
			"incremental": {
				"mode": "full",
				"checkpoint_status": "not_applicable",
				"source_step_slug": "",
				"blocked_reason": "",
				"replay_safety_status": "safe",
				"replay_blocked_reason": "",
				"backfill_series_slug": "",
				"backfill_chunk_index": 0,
				"backfill_chunk_count": 0
			}
		},
		"timeline": {
			"run_slug": "` + runSlug + `",
			"pipeline_slug": "` + pipelineSlug + `",
			"environment_slug": "prod",
			"status": "succeeded",
			"replay_of_run_slug": null,
			"events": [],
			"snapshots": [],
			"failure": null
		},
		"incremental_diagnostics": {
			"sources": [],
			"replay_safety": {
				"status": "safe",
				"blocked_sink": "",
				"blocked_reason": ""
			}
		}
	}`
}

func TestRunWithArgsPipelineRunGetMapsStructuredAPIError(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{
			"error": {
				"code": "PIPELINE_RUN_NOT_FOUND",
				"message": "pipeline run not found",
				"detailed_error": "run run-001 was not found"
			}
		}`))
	}))
	t.Cleanup(server.Close)

	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		APIKey:       "xto_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane", "pipeline", "run", "get", "run-001",
			"--project", "data-pipelines",
			"--environment", "prod",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "404")
	require.ErrorContains(t, err, "PIPELINE_RUN_NOT_FOUND")
	require.ErrorContains(t, err, "pipeline run not found")
	require.ErrorContains(t, err, "run-001")
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())
}

func TestRunWithArgsPipelineRunStartUsesSavedLoginAndPrintsRun(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotAuth string
	var gotPath string
	var gotMethod string
	var gotBody api.ExecutePipelineRequest
	var gotDecodeErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotDecodeErr = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{
			"metadata": {
				"pipeline_slug": "acme-users-sync",
				"pipeline_version_spec_hash": "spec-hash-12",
				"run_slug": "acme-users-sync-run-001",
				"request_id": "req-001",
				"status": "queued",
				"duration_ms": 0,
				"step_count": 0,
				"step_slug": "",
				"error_summary": null,
				"failure_kind": null,
				"started_at": "2026-05-15T08:00:00Z",
				"ended_at": null,
				"replay_of_run_slug": null,
				"replay_from_step": null,
				"created_at": "2026-05-15T08:00:00Z"
			},
			"timeline": {
				"run_slug": "acme-users-sync-run-001",
				"pipeline_slug": "acme-users-sync",
				"status": "queued",
				"replay_of_run_slug": null,
				"events": [],
				"snapshots": [],
				"failure": null
			}
		}`))
	}))
	t.Cleanup(server.Close)

	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		APIKey:       "xto_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane", "pipeline", "run", "start", "acme-users-sync",
			"--project", "data-pipelines",
			"--environment", "prod",
			"--run-slug", "acme-users-sync-run-001",
			"--backfill-json", `{"source_windows":{"source":{"lower_bound":{"value":9007199254740993},"upper_bound":{"value":9007199254740995}}}}`,
			"--input-json", `{"customer_id":"cus_123","dry_run":true}`,
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.NoError(t, gotDecodeErr)
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "Bearer xto_saved_token", gotAuth)
	require.Equal(t, "/api/organizations/acme/projects/data-pipelines/environments/prod/pipelines/acme-users-sync:execute", gotPath)
	require.NotNil(t, gotBody.RunSlug)
	require.Equal(t, "acme-users-sync-run-001", *gotBody.RunSlug)
	require.NotNil(t, gotBody.Input)
	require.Equal(t, map[string]any{
		"customer_id": "cus_123",
		"dry_run":     true,
	}, *gotBody.Input)
	require.Contains(t, stdout.String(), "acme-users-sync-run-001")
	require.Contains(t, stdout.String(), "\"status\": \"queued\"")
	require.NotNil(t, gotBody.Backfill)
	require.Equal(t, `{"value":9007199254740993}`, string(gotBody.Backfill.SourceWindows["source"].LowerBound))

	for _, suffix := range []string{"{}", "garbage"} {
		err := runWithRuntime([]string{"braidplane", "pipeline", "run", "start", "acme-users-sync", "--project", "data-pipelines", "--environment", "prod", "--backfill-json", `{"source_windows":{"source":{"lower_bound":{"value":1},"upper_bound":{"value":5}}}}` + suffix}, &stdout, &stderr, cliRuntime{configPath: configPath})
		require.ErrorContains(t, err, "backfill JSON must contain one object")
	}
}

func TestParseRuntimeInputJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		expected    map[string]any
		errContains string
	}{
		{
			name:  "ValidJSONObject",
			input: `{"customer_id":"cus_123","dry_run":true}`,
			expected: map[string]any{
				"customer_id": "cus_123",
				"dry_run":     true,
			},
		},
		{
			name:        "NonObjectArray",
			input:       `[1,2,3]`,
			errContains: "input-json must be a JSON object",
		},
		{
			name:        "SecretValueError",
			input:       `{"api_token":"secret"}`,
			errContains: "runtime input values are non-secret",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			input, err := parseRuntimeInputJSON(test.input)

			if test.errContains != "" {
				require.ErrorContains(t, err, test.errContains)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.expected, input)
		})
	}
}

func TestRunWithArgsPipelineRunStartTextSummarizesQueuedIncrementalDiagnostics(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want %s", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/api/organizations/acme/projects/data-pipelines/environments/prod/pipelines/users-sync:execute" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{
			"metadata": {
				"pipeline_slug": "users-sync",
				"pipeline_version_spec_hash": "spec-hash-12",
				"run_slug": "run-backfill-001",
				"source": {
					"type": "api",
					"summary": "API key run",
					"entrypoint": "api",
					"actor_type": "api_key",
					"actor_slug": "etl-worker",
					"request_id": "req-001",
					"trace_id": null,
					"parameter_keys": [],
					"source_event_slug": "evt-pipeline-run-execute-002"
				},
				"request_id": "req-001",
				"status": "queued",
				"duration_ms": 0,
				"step_count": 0,
				"step_slug": "",
				"error_summary": null,
				"failure_kind": null,
				"started_at": "2026-05-24T11:00:00Z",
				"ended_at": null,
				"replay_of_run_slug": null,
				"replay_from_step": null,
				"created_at": "2026-05-24T11:00:00Z",
				"incremental": {
					"mode": "full",
					"checkpoint_status": "not_applicable",
					"source_step_slug": "",
					"replay_safety_status": "safe",
					"backfill_series_slug": "",
					"backfill_chunk_index": 0,
					"backfill_chunk_count": 0
				}
			},
			"timeline": {
				"run_slug": "run-backfill-001",
				"pipeline_slug": "users-sync",
				"status": "queued",
				"replay_of_run_slug": null,
				"events": [],
				"snapshots": [],
				"failure": null
			},
			"incremental_diagnostics": {
				"sources": [
					{
						"source_step_slug": "fetch-users",
						"checkpoint_kind": "watermark",
						"window": {"summary":"2026-05-01 to 2026-05-02"},
						"observed_checkpoint": {"summary":"2026-05-02","raw_value_exposed":false},
						"commit_result": {"status":"skipped"}
					}
				],
				"replay_safety": {"status":"safe"}
			}
		}`))
	}))
	t.Cleanup(server.Close)

	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		APIKey:       "xto_saved_token",
		Environment:  "prod",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "run", "start", "users-sync", "--project", "data-pipelines", "--output", "text"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Contains(t, stdout.String(), "Run: run-backfill-001")
	require.Contains(t, stdout.String(), "Status: queued")
	require.Contains(t, stdout.String(), "Source: API key run via api")
	require.Contains(t, stdout.String(), "Source actor: api_key etl-worker")
	require.Contains(t, stdout.String(), "Source request: request=req-001")
	require.Contains(t, stdout.String(), "Source parameters: none")
	require.Contains(t, stdout.String(), "Mode: full")
	require.Contains(t, stdout.String(), "Checkpoint: not_applicable")
}

func TestRunWithArgsPipelineRunWatchPrintsSucceededRun(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotAuth string
	var gotPath string
	var gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pipelineRunRecordJSON("acme-users-sync", "run-001", `{
			"type": "api",
			"summary": "API key run",
			"entrypoint": "api",
			"actor_type": "api_key",
			"actor_slug": "etl-worker",
			"request_id": "req-001",
			"trace_id": null,
			"parameter_keys": [],
			"source_event_slug": "evt-pipeline-run-execute-001"
		}`)))
	}))
	t.Cleanup(server.Close)

	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		APIKey:       "xto_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane", "pipeline", "run", "watch", "run-001",
			"--project", "data-pipelines",
			"--environment", "prod",
			"--poll-interval-seconds", "1",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Equal(t, http.MethodGet, gotMethod)
	require.Equal(t, "Bearer xto_saved_token", gotAuth)
	require.Equal(t, "/api/organizations/acme/projects/data-pipelines/environments/prod/runs/run-001", gotPath)
	require.Contains(t, stdout.String(), "run-001")
	require.Contains(t, stdout.String(), "\"status\": \"succeeded\"")
}

func TestRunWithArgsPipelineRunWatchReturnsPersistedFailedRunForExecutionFailure(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotAuth string
	var gotPath string
	var gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"metadata": {
				"pipeline_slug": "acme-users-sync",
				"pipeline_version_spec_hash": "spec-hash-12",
				"run_slug": "run-001",
				"request_id": "req-001",
				"status": "failed",
				"duration_ms": 245,
				"step_count": 3,
				"step_slug": "load-users",
				"error_summary": "duplicate key value violates unique constraint",
				"failure_kind": "data_source",
				"started_at": "2026-05-15T08:00:00Z",
				"ended_at": "2026-05-15T08:00:00.245Z",
				"replay_of_run_slug": null,
				"replay_from_step": null,
				"created_at": "2026-05-15T08:00:00Z"
			},
			"timeline": {
				"run_slug": "run-001",
				"pipeline_slug": "acme-users-sync",
				"status": "failed",
				"replay_of_run_slug": null,
				"events": [],
				"snapshots": [],
				"failure": {
					"step": "load-users",
					"error": "duplicate key value violates unique constraint",
					"at": "2026-05-15T08:00:00.245Z",
					"kind": "data_source",
					"side": "sink",
					"data_source_slug": "warehouse",
					"action_hint": "Review the sink insert strategy before retrying."
				}
			}
		}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_API_KEY", "xto_env_token")
	t.Setenv("BRAIDPLANE_ENVIRONMENT", "prod")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane", "pipeline", "run", "watch", "run-001",
			"--project", "data-pipelines",
			"--poll-interval-seconds", "1",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "run-001")
	require.ErrorContains(t, err, "failed")
	require.Empty(t, stderr.String())
	require.Equal(t, http.MethodGet, gotMethod)
	require.Equal(t, "Bearer xto_env_token", gotAuth)
	require.Equal(t, "/api/organizations/acme/projects/data-pipelines/environments/prod/runs/run-001", gotPath)
	require.Contains(t, stdout.String(), "\"status\": \"failed\"")
	require.Contains(t, stdout.String(), "Review the sink insert strategy before retrying.")
}

func TestRunWithArgsPipelineRunStartMapsStructuredAPIError(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{
			"error": {
				"code": "PIPELINE_NOT_FOUND",
				"message": "pipeline not found",
				"detailed_error": "pipeline acme-users-sync does not exist"
			}
		}`))
	}))
	t.Cleanup(server.Close)

	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		APIKey:       "xto_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane", "pipeline", "run", "start", "acme-users-sync",
			"--project", "data-pipelines",
			"--environment", "prod",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "404")
	require.ErrorContains(t, err, "PIPELINE_NOT_FOUND")
	require.ErrorContains(t, err, "pipeline not found")
	require.ErrorContains(t, err, "acme-users-sync")
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())
}

func pipelineTriggerResponseJSON(slug string, enabled bool) string {
	enabledJSON := "false"
	if enabled {
		enabledJSON = "true"
	}
	return `{
		"slug": "` + slug + `",
		"name": "Daily Users",
		"type": "schedule",
		"enabled": ` + enabledJSON + `,
		"revision": 1,
		"environment_slug": "prod",
		"pipeline_slug": "users-sync",
		"version_policy": {
			"mode": "latest_deployed"
		},
		"runtime_input_keys": ["dry_run", "tenant_id"],
		"retry_policy": {
			"enabled": false,
			"max_attempts": 1,
			"backoff_strategy": "none",
			"initial_delay_seconds": 0,
			"max_delay_seconds": 0,
			"jitter": false,
			"retryable_failure_kinds": []
		},
		"schedule": {
			"cron": "0 7 * * *",
			"timezone": "UTC",
			"misfire_policy": "skip"
		},
		"created_by_actor_type": "api_key",
		"updated_by_actor_type": "api_key",
		"created_at": "2026-06-12T07:00:00Z",
		"updated_at": "2026-06-12T07:00:00Z"
	}`
}

func pipelineWebhookTriggerResponseJSON(slug string, enabled bool) string {
	enabledJSON := "false"
	if enabled {
		enabledJSON = "true"
	}
	return `{
		"slug": "` + slug + `",
		"name": "Customer Event",
		"type": "webhook",
		"enabled": ` + enabledJSON + `,
		"revision": 1,
		"environment_slug": "prod",
		"pipeline_slug": "users-sync",
		"version_policy": {"mode": "latest_deployed"},
		"runtime_input_keys": ["event_type", "tenant_id"],
		"retry_policy": {
			"enabled": false,
			"max_attempts": 1,
			"backoff_strategy": "none",
			"initial_delay_seconds": 0,
			"max_delay_seconds": 0,
			"jitter": false,
			"retryable_failure_kinds": []
		},
		"webhook": {
			"receiver_id": "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG",
			"verification_strategy": "hmac_sha256",
			"input_mappings": [{"json_pointer":"/tenant/id","runtime_input_key":"tenant_id"}],
			"enabled_key_versions": [1],
			"secret_configured": true
		},
		"webhook_secret": "must-not-print",
		"created_by_actor_type": "api_key",
		"updated_by_actor_type": "api_key",
		"created_at": "2026-06-12T07:00:00Z",
		"updated_at": "2026-06-12T07:00:00Z"
	}`
}

func TestWritePipelineValidationResultPrintsODCSConversionDiagnostics(t *testing.T) {
	t.Parallel()
	step := api.NullableSlug("fetch")
	var output bytes.Buffer
	err := writePipelineValidationResult(&output, &api.PipelineValidationResponse{Valid: false, Diagnostics: []api.PipelineValidationDiagnostic{{
		Severity: api.PipelineValidationDiagnosticSeverityError, Code: "ODCS_TYPE_UNSUPPORTED", Field: "steps[0].query.output_contract_ref", StepSlug: &step,
		Message: "$.schema[0].properties[0].logicalType: Declare a supported type.",
	}}})
	require.NoError(t, err)
	require.Contains(t, output.String(), "ERROR steps[0].query.output_contract_ref fetch ODCS_TYPE_UNSUPPORTED: $.schema[0].properties[0].logicalType: Declare a supported type.")
}

func TestRunWithArgsPipelineBootstrap(t *testing.T) {
	t.Parallel()
	var got api.BootstrapPipelineCheckpointsRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/organizations/acme/projects/default/environments/prod/pipelines/users-sync:bootstrap-checkpoints", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	config := filepath.Join(t.TempDir(), "braidplane.toml")
	require.NoError(t, saveLogin(config, cliAuthConfig{ServerURL: server.URL + "/api", Organization: "acme", APIKey: "xto_saved_token"}))
	var stdout, stderr bytes.Buffer
	err := runWithRuntime([]string{"braidplane", "pipeline", "bootstrap", "users-sync", "--project", "default", "--environment", "prod", "--confirm-environment", "prod", "--request-slug", "bootstrap-001", "--seeds-json", `{"source":{"value":9007199254740993}}`}, &stdout, &stderr, cliRuntime{configPath: config})
	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Equal(t, "bootstrap-001", got.RequestSlug)
	require.Equal(t, `{"value":9007199254740993}`, string(got.SourceSeeds["source"]))
	require.Contains(t, stdout.String(), "bootstrapped")
}
