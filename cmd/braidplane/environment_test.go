package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// CLI auth resolution reads process environment variables.
func TestRunWithArgsEnvironmentMutationCommandsUseExpectedRequests(t *testing.T) {
	environmentPayload := `{"slug":"prod","name":"Production","description":"Customers","color":"red","protected":false,"created_at":"2026-03-01T00:00:00Z","updated_at":"2026-03-01T00:00:00Z"}`
	projectEnvironmentPayload := `{"environment_slug":"prod","environment_name":"Production","environment_description":"Customers","environment_color":"red","environment_protected":false,"is_default":true,"enabled_at":"2026-03-01T00:00:00Z","updated_at":"2026-03-01T00:00:00Z"}`
	runtimeVariablePayload := `{"environment_slug":"prod","key":"s3_prefix","value_type":"text","value":"prd/orders/","description":"S3 prefix","created_at":"2026-03-01T00:00:00Z","updated_at":"2026-03-01T00:00:00Z"}`
	refPayload := `{"pipeline_slug":"orders-sync","slug":"orders-db","name":"Orders DB","data_source_type":"postgres","required_capabilities":["pipeline_source","pipeline_sink"],"created_at":"2026-03-01T00:00:00Z","updated_at":"2026-03-01T00:00:00Z"}`
	bindingPayload := `{"data_source_ref_slug":"orders-db","data_source_ref_name":"Orders DB","environment_slug":"prod","data_source_slug":"orders-postgres-prod","data_source_name":"Orders Postgres Prod","data_source_type":"postgres","data_source_active":true,"data_source_read_only":false,"bound_at":"2026-03-01T00:00:00Z","updated_at":"2026-03-01T00:00:00Z"}`

	tests := []struct {
		name       string
		args       []string
		method     string
		target     string
		statusCode int
		response   string
		wantBody   string
	}{
		{
			name:       "environment create",
			args:       []string{"braidplane", "environment", "create", "--name", "Production", "--slug", "prod", "--description", "Customers"},
			method:     http.MethodPost,
			target:     "/api/organizations/acme/environments",
			statusCode: http.StatusCreated,
			response:   environmentPayload,
			wantBody:   `{"name":"Production","slug":"prod","description":"Customers","color":"gray"}`,
		},
		{
			name:       "environment update",
			args:       []string{"braidplane", "environment", "update", "prod", "--name", "Production", "--description", "Customers"},
			method:     http.MethodPut,
			target:     "/api/organizations/acme/environments/prod",
			statusCode: http.StatusOK,
			response:   environmentPayload,
			wantBody:   `{"name":"Production","description":"Customers","color":"red","protected":false}`,
		},
		{
			name:       "environment delete",
			args:       []string{"braidplane", "environment", "delete", "prod", "--delete-reason", "retired"},
			method:     http.MethodDelete,
			target:     "/api/organizations/acme/environments/prod?delete_reason=retired",
			statusCode: http.StatusNoContent,
		},
		{
			name:       "project environment enable",
			args:       []string{"braidplane", "project-environment", "enable", "prod", "--project", "data-pipelines", "--default"},
			method:     http.MethodPut,
			target:     "/api/organizations/acme/projects/data-pipelines/environments/prod",
			statusCode: http.StatusOK,
			response:   projectEnvironmentPayload,
			wantBody:   `{"is_default":true}`,
		},
		{
			name:       "project environment set default",
			args:       []string{"braidplane", "project-environment", "set-default", "prod", "--project", "data-pipelines"},
			method:     http.MethodPut,
			target:     "/api/organizations/acme/projects/data-pipelines/environments/prod",
			statusCode: http.StatusOK,
			response:   projectEnvironmentPayload,
			wantBody:   `{"is_default":true}`,
		},
		{
			name:       "project environment disable",
			args:       []string{"braidplane", "project-environment", "disable", "prod", "--project", "data-pipelines"},
			method:     http.MethodDelete,
			target:     "/api/organizations/acme/projects/data-pipelines/environments/prod",
			statusCode: http.StatusNoContent,
		},
		{
			name: "runtime variable upsert",
			args: []string{
				"braidplane", "environment", "var", "upsert", "s3_prefix",
				"--environment", "prod",
				"--type", "text",
				"--value", "prd/orders/",
				"--description", "S3 prefix",
			},
			method:     http.MethodPut,
			target:     "/api/organizations/acme/environments/prod/runtime-variables/s3_prefix",
			statusCode: http.StatusOK,
			response:   runtimeVariablePayload,
			wantBody:   `{"value_type":"text","value":"prd/orders/","description":"S3 prefix"}`,
		},
		{
			name:       "runtime variable delete",
			args:       []string{"braidplane", "environment", "var", "delete", "s3_prefix", "--environment", "prod"},
			method:     http.MethodDelete,
			target:     "/api/organizations/acme/environments/prod/runtime-variables/s3_prefix",
			statusCode: http.StatusNoContent,
		},
		{
			name: "data source ref create",
			args: []string{
				"braidplane", "data-source-ref", "create",
				"--project", "data-pipelines",
				"--pipeline", "orders-sync",
				"--name", "Orders DB",
				"--slug", "orders-db",
				"--data-source-type", "postgres",
				"--capability", "pipeline_source",
				"--capability", "pipeline_sink",
			},
			method:     http.MethodPost,
			target:     "/api/organizations/acme/projects/data-pipelines/pipelines/orders-sync/data-source-refs",
			statusCode: http.StatusCreated,
			response:   refPayload,
			wantBody:   `{"name":"Orders DB","slug":"orders-db","data_source_type":"postgres","required_capabilities":["pipeline_source","pipeline_sink"]}`,
		},
		{
			name: "data source ref update",
			args: []string{
				"braidplane", "data-source-ref", "update", "orders-db",
				"--project", "data-pipelines",
				"--pipeline", "orders-sync",
				"--name", "Orders DB",
				"--data-source-type", "postgres",
				"--capability", "pipeline_source",
				"--capability", "pipeline_sink",
			},
			method:     http.MethodPut,
			target:     "/api/organizations/acme/projects/data-pipelines/pipelines/orders-sync/data-source-refs/orders-db",
			statusCode: http.StatusOK,
			response:   refPayload,
			wantBody:   `{"name":"Orders DB","data_source_type":"postgres","required_capabilities":["pipeline_source","pipeline_sink"]}`,
		},
		{
			name: "data source ref update with protected confirmation",
			args: []string{
				"braidplane", "data-source-ref", "update", "orders-db",
				"--project", "data-pipelines",
				"--pipeline", "orders-sync",
				"--name", "Orders DB",
				"--data-source-type", "postgres",
				"--capability", "pipeline_source",
				"--confirm-environment", "prod",
			},
			method:     http.MethodPut,
			target:     "/api/organizations/acme/projects/data-pipelines/pipelines/orders-sync/data-source-refs/orders-db",
			statusCode: http.StatusOK,
			response:   refPayload,
			wantBody:   `{"name":"Orders DB","data_source_type":"postgres","protected_environment_confirmation":"prod","required_capabilities":["pipeline_source"]}`,
		},
		{
			name:       "data source ref delete",
			args:       []string{"braidplane", "data-source-ref", "delete", "orders-db", "--project", "data-pipelines", "--pipeline", "orders-sync"},
			method:     http.MethodDelete,
			target:     "/api/organizations/acme/projects/data-pipelines/pipelines/orders-sync/data-source-refs/orders-db",
			statusCode: http.StatusNoContent,
		},
		{
			name: "data source binding upsert",
			args: []string{
				"braidplane", "data-source-binding", "upsert", "orders-db",
				"--project", "data-pipelines",
				"--pipeline", "orders-sync",
				"--environment", "prod",
				"--data-source", "orders-postgres-prod",
			},
			method:     http.MethodPut,
			target:     "/api/organizations/acme/projects/data-pipelines/pipelines/orders-sync/environments/prod/data-source-bindings/orders-db",
			statusCode: http.StatusOK,
			response:   bindingPayload,
			wantBody:   `{"data_source_slug":"orders-postgres-prod"}`,
		},
		{
			name: "data source binding bind",
			args: []string{
				"braidplane", "data-source-binding", "bind", "orders-db",
				"--project", "data-pipelines",
				"--pipeline", "orders-sync",
				"--environment", "prod",
				"--data-source", "orders-postgres-prod",
			},
			method:     http.MethodPut,
			target:     "/api/organizations/acme/projects/data-pipelines/pipelines/orders-sync/environments/prod/data-source-bindings/orders-db",
			statusCode: http.StatusOK,
			response:   bindingPayload,
			wantBody:   `{"data_source_slug":"orders-postgres-prod"}`,
		},
		{
			name:       "data source binding delete",
			args:       []string{"braidplane", "data-source-binding", "delete", "orders-db", "--project", "data-pipelines", "--pipeline", "orders-sync", "--environment", "prod"},
			method:     http.MethodDelete,
			target:     "/api/organizations/acme/projects/data-pipelines/pipelines/orders-sync/environments/prod/data-source-bindings/orders-db",
			statusCode: http.StatusNoContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotMethod string
			var gotTarget string
			var gotAuth string
			var gotBody []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotTarget = r.URL.RequestURI()
				gotAuth = r.Header.Get("Authorization")
				var err error
				gotBody, err = io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read request body: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}

				if tt.statusCode != http.StatusNoContent {
					w.Header().Set("Content-Type", "application/json")
				}
				w.WriteHeader(tt.statusCode)
				if tt.response != "" {
					_, _ = w.Write([]byte(tt.response))
				}
			}))
			t.Cleanup(server.Close)

			stdout, stderr, err := runCLIForEnvironmentTest(t, server.URL, tt.args)
			require.NoError(t, err)
			require.Empty(t, stderr)
			require.Equal(t, tt.method, gotMethod)
			require.Equal(t, tt.target, gotTarget)
			require.Equal(t, "Bearer xto_test_token", gotAuth)
			if tt.wantBody != "" {
				require.JSONEq(t, tt.wantBody, string(gotBody))
				require.NotEmpty(t, stdout)
			} else {
				require.Empty(t, string(gotBody))
				require.Empty(t, stdout)
			}
		})
	}
}

// CLI auth resolution reads process environment variables.
func TestRunWithArgsEnvironmentReadCommandsUseExpectedRequests(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		target   string
		response string
	}{
		{
			name:     "environment list",
			args:     []string{"braidplane", "environment", "list", "--limit", "50", "--cursor", "page-2", "--direction", "next", "--include-deleted"},
			target:   "/api/organizations/acme/environments?limit=50&cursor=page-2&include_deleted=true&direction=next",
			response: `{"environments":[{"slug":"prod","name":"Production","description":null,"color":"red","protected":false,"created_at":"2026-03-01T00:00:00Z","updated_at":"2026-03-01T00:00:00Z"}],"has_more":false}`,
		},
		{
			name:     "environment get",
			args:     []string{"braidplane", "environment", "get", "prod"},
			target:   "/api/organizations/acme/environments/prod",
			response: `{"slug":"prod","name":"Production","description":null,"color":"red","protected":false,"created_at":"2026-03-01T00:00:00Z","updated_at":"2026-03-01T00:00:00Z"}`,
		},
		{
			name:     "project environment list",
			args:     []string{"braidplane", "project-environment", "list", "--project", "data-pipelines", "--limit", "10", "--cursor", "page-2", "--direction", "prev"},
			target:   "/api/organizations/acme/projects/data-pipelines/environments?limit=10&cursor=page-2&direction=prev",
			response: `{"project_environments":[{"environment_slug":"prod","environment_name":"Production","environment_description":null,"environment_color":"red","environment_protected":false,"is_default":true,"enabled_at":"2026-03-01T00:00:00Z","updated_at":"2026-03-01T00:00:00Z"}],"has_more":false}`,
		},
		{
			name:     "runtime variable list",
			args:     []string{"braidplane", "environment", "var", "list", "--environment", "prod", "--limit", "10", "--cursor", "page-2", "--direction", "prev"},
			target:   "/api/organizations/acme/environments/prod/runtime-variables?limit=10&cursor=page-2&direction=prev",
			response: `{"runtime_variables":[{"environment_slug":"prod","key":"s3_prefix","value_type":"text","value":"prd/orders/","description":null,"created_at":"2026-03-01T00:00:00Z","updated_at":"2026-03-01T00:00:00Z"}],"has_more":false}`,
		},
		{
			name:     "runtime variable get",
			args:     []string{"braidplane", "environment", "var", "get", "s3_prefix", "--environment", "prod"},
			target:   "/api/organizations/acme/environments/prod/runtime-variables/s3_prefix",
			response: `{"environment_slug":"prod","key":"s3_prefix","value_type":"text","value":"prd/orders/","description":null,"created_at":"2026-03-01T00:00:00Z","updated_at":"2026-03-01T00:00:00Z"}`,
		},
		{
			name:     "data source ref list",
			args:     []string{"braidplane", "data-source-ref", "list", "--project", "data-pipelines", "--limit", "10", "--cursor", "page-2"},
			target:   "/api/organizations/acme/projects/data-pipelines/data-source-refs?limit=10&cursor=page-2",
			response: `{"data_source_refs":[{"pipeline_slug":"orders-sync","slug":"orders-db","name":"Orders DB","data_source_type":"postgres","required_capabilities":["pipeline_source"],"created_at":"2026-03-01T00:00:00Z","updated_at":"2026-03-01T00:00:00Z"}],"has_more":false}`,
		},
		{
			name:     "data source ref get",
			args:     []string{"braidplane", "data-source-ref", "get", "orders-db", "--project", "data-pipelines", "--pipeline", "orders-sync"},
			target:   "/api/organizations/acme/projects/data-pipelines/pipelines/orders-sync/data-source-refs/orders-db",
			response: `{"pipeline_slug":"orders-sync","slug":"orders-db","name":"Orders DB","data_source_type":"postgres","required_capabilities":["pipeline_source"],"created_at":"2026-03-01T00:00:00Z","updated_at":"2026-03-01T00:00:00Z"}`,
		},
		{
			name:     "data source binding list",
			args:     []string{"braidplane", "data-source-binding", "list", "--project", "data-pipelines", "--environment", "prod", "--limit", "10", "--cursor", "page-2"},
			target:   "/api/organizations/acme/projects/data-pipelines/environments/prod/data-source-bindings?limit=10&cursor=page-2",
			response: `{"data_source_bindings":[{"pipeline_slug":"orders-sync","data_source_ref_slug":"orders-db","data_source_ref_name":"Orders DB","environment_slug":"prod","data_source_slug":"orders-postgres-prod","data_source_name":"Orders Postgres Prod","data_source_type":"postgres","data_source_active":true,"data_source_read_only":false,"bound_at":"2026-03-01T00:00:00Z","updated_at":"2026-03-01T00:00:00Z"}],"has_more":false}`,
		},
		{
			name:     "data source binding get",
			args:     []string{"braidplane", "data-source-binding", "get", "orders-db", "--project", "data-pipelines", "--pipeline", "orders-sync", "--environment", "prod"},
			target:   "/api/organizations/acme/projects/data-pipelines/pipelines/orders-sync/environments/prod/data-source-bindings/orders-db",
			response: `{"pipeline_slug":"orders-sync","data_source_ref_slug":"orders-db","data_source_ref_name":"Orders DB","environment_slug":"prod","data_source_slug":"orders-postgres-prod","data_source_name":"Orders Postgres Prod","data_source_type":"postgres","data_source_active":true,"data_source_read_only":false,"bound_at":"2026-03-01T00:00:00Z","updated_at":"2026-03-01T00:00:00Z"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotTarget string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotTarget = r.URL.RequestURI()
				if r.Method != http.MethodGet {
					t.Errorf("method = %s, want GET", r.Method)
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.response))
			}))
			t.Cleanup(server.Close)

			stdout, stderr, err := runCLIForEnvironmentTest(t, server.URL, tt.args)
			require.NoError(t, err)
			require.NotEmpty(t, stdout)
			require.Empty(t, stderr)
			require.Equal(t, tt.target, gotTarget)
		})
	}
}

// CLI auth resolution reads process environment variables.
func TestRunWithArgsEnvironmentCreateRejectsInvalidColor(t *testing.T) {
	stdout, stderr, err := runCLIForEnvironmentTest(
		t,
		"http://127.0.0.1:1",
		[]string{"braidplane", "environment", "create", "--name", "Production", "--slug", "prod", "--color", "neon"},
	)

	require.Error(t, err)
	require.Empty(t, stdout)
	require.Empty(t, stderr)
	require.ErrorContains(t, err, "environment color must be one of blue, amber, red, green, purple, or gray")
}

func TestRunWithArgsDataSourceBindingUsesSavedEnvironmentDefault(t *testing.T) {
	var gotTarget string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTarget = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data_source_bindings":[],"has_more":false}`))
	}))
	t.Cleanup(server.Close)

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		APIKey:       "xto_test_token",
		Environment:  "stg",
	}))
	t.Setenv("BRAIDPLANE_SERVER_URL", "")
	t.Setenv("BRAIDPLANE_ORG", "")
	t.Setenv("BRAIDPLANE_API_KEY", "")
	t.Setenv("BRAIDPLANE_PROJECT", "data-pipelines")
	t.Setenv("BRAIDPLANE_ENVIRONMENT", "")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "data-source-binding", "list"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.NotEmpty(t, stdout.String())
	require.Equal(t, "/api/organizations/acme/projects/data-pipelines/environments/stg/data-source-bindings", gotTarget)
}

func TestRunWithArgsRuntimeVariableUsesSavedEnvironmentDefault(t *testing.T) {
	var gotTarget string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTarget = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"runtime_variables":[],"has_more":false}`))
	}))
	t.Cleanup(server.Close)

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		APIKey:       "xto_test_token",
		Environment:  "stg",
	}))
	t.Setenv("BRAIDPLANE_SERVER_URL", "")
	t.Setenv("BRAIDPLANE_ORG", "")
	t.Setenv("BRAIDPLANE_API_KEY", "")
	t.Setenv("BRAIDPLANE_PROJECT", "")
	t.Setenv("BRAIDPLANE_ENVIRONMENT", "")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "environment", "var", "list"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.NotEmpty(t, stdout.String())
	require.Equal(t, "/api/organizations/acme/environments/stg/runtime-variables", gotTarget)
}

func TestRunWithArgsProjectEnvironmentHelpDescribesEnablement(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithArgs([]string{"braidplane", "project-environment", "--help"}, &stdout, &stderr)

	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Manage project-environment enablement")
	require.Empty(t, stderr.String())
}

func TestRunWithArgsDataSourceBindingHelpExposesBindAndUpsert(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithArgs([]string{"braidplane", "data-source-binding", "--help"}, &stdout, &stderr)

	require.NoError(t, err)
	require.Contains(t, stdout.String(), "bind")
	require.Contains(t, stdout.String(), "upsert")
	require.Empty(t, stderr.String())
}

func TestDataSourceRefContractRejectsInvalidCapability(t *testing.T) {
	t.Parallel()

	_, _, err := dataSourceRefContract("postgres", []string{"read"})

	require.Error(t, err)
	require.ErrorContains(t, err, "capability")
}

func runCLIForEnvironmentTest(t *testing.T, serverURL string, args []string) (string, string, error) {
	t.Helper()
	t.Setenv("BRAIDPLANE_SERVER_URL", serverURL+"/api")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_API_KEY", "xto_test_token")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		args,
		&stdout,
		&stderr,
		cliRuntime{configPath: filepath.Join(t.TempDir(), "braidplane.toml")},
	)
	return stdout.String(), stderr.String(), err
}
