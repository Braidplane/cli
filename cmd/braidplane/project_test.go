package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

// CLI auth resolution reads process environment variables.
func TestRunWithArgsProjectMutationCommandsUseExpectedRequests(t *testing.T) {
	projectPayload := `{"slug":"data-pipelines","name":"Data pipelines","pipeline_backend":null,"created_at":"2026-06-01T00:00:00Z","updated_at":"2026-06-01T00:00:00Z"}`
	projectWithBackendPayload := `{"slug":"data-pipelines","name":"Data pipelines","pipeline_backend":{"data_source_slug":"pipelines-git","repo":"https://github.com/acme/pipelines.git","tracked_branch":"main"},"created_at":"2026-06-01T00:00:00Z","updated_at":"2026-06-01T00:00:00Z"}`

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
			name:       "ProjectCreate",
			args:       []string{"braidplane", "project", "create", "--name", "Data pipelines", "--slug", "data-pipelines"},
			method:     http.MethodPost,
			target:     "/api/organizations/acme/projects",
			statusCode: http.StatusCreated,
			response:   projectPayload,
			wantBody:   `{"name":"Data pipelines","slug":"data-pipelines"}`,
		},
		{
			name:       "ProjectUpdate",
			args:       []string{"braidplane", "project", "update", "data-pipelines", "--name", "Operations workspace"},
			method:     http.MethodPut,
			target:     "/api/organizations/acme/projects/data-pipelines",
			statusCode: http.StatusOK,
			response:   projectPayload,
			wantBody:   `{"name":"Operations workspace"}`,
		},
		{
			name:       "ProjectDeleteWithRemoteCleanup",
			args:       []string{"braidplane", "project", "delete", "data-pipelines", "--cleanup-remote"},
			method:     http.MethodDelete,
			target:     "/api/organizations/acme/projects/data-pipelines?cleanup_remote=true",
			statusCode: http.StatusNoContent,
		},
		{
			name: "ProjectPipelineBackendSet",
			args: []string{
				"braidplane", "project", "pipeline-backend", "set", "data-pipelines",
				"--data-source", "pipelines-git",
				"--tracked-branch", "main",
				"--mode", "adopt_existing",
			},
			method:     http.MethodPut,
			target:     "/api/organizations/acme/projects/data-pipelines/pipeline-backend",
			statusCode: http.StatusOK,
			response:   projectWithBackendPayload,
			wantBody:   `{"data_source_slug":"pipelines-git","mode":"adopt_existing","pipeline_dir":"pipelines","tracked_branch":"main"}`,
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
func TestRunWithArgsProjectReadCommandsUseExpectedRequests(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		target   string
		response string
	}{
		{
			name:   "ProjectList",
			args:   []string{"braidplane", "project", "list", "--limit", "10", "--cursor", "page-2", "--sort-by", "project_name", "--order", "asc", "--direction", "prev"},
			target: "/api/organizations/acme/projects?limit=10&cursor=page-2&sort_by=project_name&order=asc&direction=prev",
			response: `{
				"projects": [{
					"slug": "data-pipelines",
					"name": "Data pipelines",
					"pipeline_backend": null,
					"created_at": "2026-06-01T00:00:00Z",
					"updated_at": "2026-06-01T00:00:00Z"
				}],
				"has_more": false
			}`,
		},
		{
			name:   "ProjectGet",
			args:   []string{"braidplane", "project", "get", "data-pipelines"},
			target: "/api/organizations/acme/projects/data-pipelines",
			response: `{
				"slug": "data-pipelines",
				"name": "Data pipelines",
				"pipeline_backend": null,
				"created_at": "2026-06-01T00:00:00Z",
				"updated_at": "2026-06-01T00:00:00Z"
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotTarget string
			var gotAuth string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotTarget = r.URL.RequestURI()
				gotAuth = r.Header.Get("Authorization")
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
			require.Equal(t, "Bearer xto_test_token", gotAuth)
		})
	}
}

func TestRunWithArgsProjectPipelineBackendSetMapsStructuredAPIErrors(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotPath string
	var gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{
			"error": {
				"code": "PROJECT_PIPELINE_BACKEND_CONFLICT",
				"message": "project pipeline backend conflict",
				"detailed_error": "data source pipelines-git cannot be attached to project data-pipelines"
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
			"braidplane", "project", "pipeline-backend", "set", "data-pipelines",
			"--data-source", "pipelines-git",
			"--tracked-branch", "main",
			"--mode", "adopt_existing",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.ErrorContains(t, err, "409")
	require.ErrorContains(t, err, "PROJECT_PIPELINE_BACKEND_CONFLICT")
	require.ErrorContains(t, err, "project pipeline backend conflict")
	require.ErrorContains(t, err, "pipelines-git")
	require.Equal(t, http.MethodPut, gotMethod)
	require.Equal(t, "/api/organizations/acme/projects/data-pipelines/pipeline-backend", gotPath)
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())
}

func TestRunWithArgsProjectPipelineBackendSetRequiresAttachModeAndDataSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		errContains string
	}{
		{
			name: "DataSource",
			args: []string{
				"braidplane", "project", "pipeline-backend", "set", "data-pipelines",
				"--tracked-branch", "main",
				"--mode", "adopt_existing",
			},
			errContains: "--data-source",
		},
		{
			name: "Mode",
			args: []string{
				"braidplane", "project", "pipeline-backend", "set", "data-pipelines",
				"--data-source", "pipelines-git",
				"--tracked-branch", "main",
			},
			errContains: "--mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			err := runWithArgs(tt.args, &stdout, &stderr)

			require.Error(t, err)
			require.ErrorContains(t, err, tt.errContains)
			require.Empty(t, stdout.String())
		})
	}
}

func TestRunWithArgsProjectRejectsInvalidEnumFlagsAsValidationFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		errContains string
	}{
		{
			name: "PipelineBackendMode",
			args: []string{
				"braidplane", "project", "pipeline-backend", "set", "data-pipelines",
				"--server-url", "http://example.invalid",
				"--org", "acme",
				"--api-key", "xto_saved_token",
				"--data-source", "pipelines-git",
				"--tracked-branch", "main",
				"--mode", "replace_existing",
			},
			errContains: "mode must be adopt_existing",
		},
		{
			name: "ListSortBy",
			args: []string{
				"braidplane", "project", "list",
				"--server-url", "http://example.invalid",
				"--org", "acme",
				"--api-key", "xto_saved_token",
				"--sort-by", "slug",
			},
			errContains: "sort-by must be project_created_at, project_name, or project_updated_at",
		},
		{
			name: "ListOrder",
			args: []string{
				"braidplane", "project", "list",
				"--server-url", "http://example.invalid",
				"--org", "acme",
				"--api-key", "xto_saved_token",
				"--order", "ascending",
			},
			errContains: "order must be asc or desc",
		},
		{
			name: "ListDirection",
			args: []string{
				"braidplane", "project", "list",
				"--server-url", "http://example.invalid",
				"--org", "acme",
				"--api-key", "xto_saved_token",
				"--direction", "forward",
			},
			errContains: "direction must be next or prev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			err := runWithArgs(tt.args, &stdout, &stderr)

			require.True(t, xerror.Is(err, xerror.ValidationFailed()))
			require.ErrorContains(t, err, tt.errContains)
			require.Empty(t, stdout.String())
			require.Empty(t, stderr.String())
		})
	}
}

func TestListProjectsParamsTrimsEnumFlags(t *testing.T) {
	t.Parallel()

	params, err := listProjectsParams(0, "", " project_name ", " asc ", " prev ")

	require.NoError(t, err)
	require.NotNil(t, params.SortBy)
	require.Equal(t, api.ListProjectsParamsSortBy("project_name"), *params.SortBy)
	require.NotNil(t, params.Order)
	require.Equal(t, api.ListProjectsParamsOrder("asc"), *params.Order)
	require.NotNil(t, params.Direction)
	require.Equal(t, api.ListProjectsParamsDirection("prev"), *params.Direction)
}
