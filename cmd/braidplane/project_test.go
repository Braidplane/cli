package main

import (
	"bytes"
	"context"
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

func TestProjectRepositoryCreateWaitsForExactAttemptAfterConsent(t *testing.T) {
	for _, provider := range []string{"github", "gitlab"} {
		t.Run(provider, func(t *testing.T) {
			polls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost {
					assert.Equal(t, "/api/organizations/acme/projects/default/repository-setup", r.URL.Path)
					_, _ = io.WriteString(w, `{"slug":"repo-exact","project_slug":"default","data_source_slug":"repo-exact","status":"pending","authorization_url":"https://`+provider+`.com/authorize?state=synthetic-state"}`)
					return
				}
				assert.Equal(t, "/api/organizations/acme/projects/default/repository-setups/repo-exact", r.URL.Path)
				polls++
				status := "pending"
				if polls == 2 {
					status = "attached"
				}
				_, _ = io.WriteString(w, `{"slug":"repo-exact","project_slug":"default","data_source_slug":"repo-exact","status":"`+status+`"}`)
			}))
			defer server.Close()
			stdout, stderr, err := runCLIForEnvironmentTest(t, server.URL, []string{"braidplane", "project", "repository", "create", "default", "--provider", provider, "--owner", "customer", "--repository-name", "pipelines", "--create-private-repository", "--wait", "--timeout-seconds", "10"})
			require.NoError(t, err)
			require.Contains(t, stdout, `"status": "attached"`)
			require.NotContains(t, stdout, "synthetic-state")
			require.Contains(t, stderr, "authorize in your browser")
			require.Contains(t, stderr, "https://"+provider+".com/authorize")
			require.Equal(t, 2, polls)
		})
	}
}

func TestRepositoryWaitPreservesProgressOnFailureAndRejectsAnotherAttempt(t *testing.T) {
	for _, scenario := range []string{"failure", "authorization_denied", "authorization_failed", "archived", "wrong-identity", "forbidden", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/organizations/acme/projects/default/repository-setups/repo-exact", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				if scenario == "forbidden" {
					w.WriteHeader(http.StatusForbidden)
					_, _ = io.WriteString(w, `{"error":{"message":"Permission changed"}}`)
					return
				}
				slug, extra := "repo-exact", ""
				switch scenario {
				case "failure":
					extra = `,"failure_code":"source_test_required"`
				case "authorization_denied", "authorization_failed":
					extra = `,"failure_code":"` + scenario + `"`
				case "archived":
					extra = `,"abandoned_at":"2026-10-20T18:00:00Z"`
				case "wrong-identity":
					slug = "another-attempt"
				}
				_, _ = io.WriteString(w, `{"slug":"`+slug+`","project_slug":"default","status":"pending"`+extra+`}`)
			}))
			defer server.Close()
			client, err := api.NewClientWithResponses(server.URL + "/api")
			require.NoError(t, err)
			var stdout, stderr bytes.Buffer
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "canceled" {
				cancel()
			}
			err = waitForProjectRepository(ctx, &cliAPIClient{openapi: client, organization: "acme"}, "default", &api.ProjectRepositorySetup{Slug: "repo-exact", ProjectSlug: "default", Status: api.ProjectRepositorySetupStatusPending}, &cliOutput{stdout: &stdout, stderr: &stderr})
			require.Error(t, err)
			require.Contains(t, stdout.String(), `"slug": "repo-exact"`)
			require.NotContains(t, stdout.String(), "another-attempt")
			if scenario == "failure" {
				require.ErrorContains(t, err, "source_test_required")
			}
			if scenario == "canceled" {
				require.ErrorContains(t, err, "progress is saved")
			}
			switch scenario {
			case "archived":
				require.ErrorContains(t, err, "was archived")
			case "wrong-identity":
				require.ErrorContains(t, err, "unexpected identity")
			case "forbidden":
				require.ErrorContains(t, err, "Permission changed")
			case "authorization_denied", "authorization_failed":
				require.ErrorContains(t, err, "start fresh authorization")
				require.Contains(t, stdout.String(), scenario)
			}
		})
	}
}

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
		wantOutput bool
	}{
		{
			name:   "ProjectRepositoryAbandon",
			args:   []string{"braidplane", "project", "repository", "abandon", "data-pipelines", "--setup", "repo-old", "--preserve-resources"},
			method: http.MethodPost, target: "/api/organizations/acme/projects/data-pipelines/repository-setup/abandon", statusCode: http.StatusOK,
			response: `{"slug":"repo-old","project_slug":"data-pipelines","data_source_slug":"repo-old","status":"pending","abandoned_at":"2026-10-20T18:00:00Z","repository_id":"42","request":{"provider":"gitlab","owner":"alice","repository_name":"old","create_private_repository":true}}`,
			wantBody: `{"setup_slug":"repo-old","preserve_resources":true}`,
		},
		{
			name:   "ProjectRepositoryCreate",
			args:   []string{"braidplane", "project", "repository", "create", "data-pipelines", "--provider", "gitlab", "--owner", "alice", "--repository-name", "pipelines", "--create-private-repository"},
			method: http.MethodPost, target: "/api/organizations/acme/projects/data-pipelines/repository-setup", statusCode: http.StatusOK,
			response: `{"slug":"repo-isolated","project_slug":"data-pipelines","data_source_slug":"repo-isolated","status":"attached","request":{"provider":"gitlab","owner":"alice","repository_name":"pipelines","create_private_repository":true}}`,
			wantBody: `{"provider":"gitlab","owner":"alice","repository_name":"pipelines","tracked_branch":"main","pipeline_dir":"pipelines","create_private_repository":true}`,
		},
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
			name:       "SettlementReviewResetGet",
			args:       []string{"braidplane", "project", "settlement-review-reset", "get", "settlement-review"},
			method:     http.MethodGet,
			target:     "/api/organizations/acme/projects/settlement-review/settlement-review-reset",
			statusCode: http.StatusOK,
			response:   `{"requested_at":null}`,
			wantOutput: true,
		},
		{
			name:       "SettlementReviewResetRequest",
			args:       []string{"braidplane", "project", "settlement-review-reset", "request", "settlement-review"},
			method:     http.MethodPost,
			target:     "/api/organizations/acme/projects/settlement-review/settlement-review-reset",
			statusCode: http.StatusOK,
			response:   `{"requested_at":"2026-10-27T12:00:00Z"}`,
			wantOutput: true,
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
			} else {
				require.Empty(t, string(gotBody))
			}
			if tt.wantBody != "" || tt.wantOutput {
				require.NotEmpty(t, stdout)
			} else {
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

func TestProjectRepositoryCreateRequiresConsentBeforeRequest(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()
	_, _, err := runCLIForEnvironmentTest(t, server.URL, []string{"braidplane", "project", "repository", "create", "default", "--provider", "gitlab", "--owner", "alice", "--repository-name", "pipelines"})
	require.ErrorContains(t, err, "--create-private-repository is required")
	require.False(t, called)
}

func TestProjectRepositoryWaitRejectsNonpositiveTimeoutBeforeMutation(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()
	_, _, err := runCLIForEnvironmentTest(t, server.URL, []string{"braidplane", "project", "repository", "create", "default", "--provider", "gitlab", "--owner", "alice", "--repository-name", "pipelines", "--create-private-repository", "--wait", "--timeout-seconds", "0"})
	require.ErrorContains(t, err, "--timeout-seconds must be greater than zero")
	require.False(t, called)
}

func TestProjectRepositoryGetWithoutSetupReturnsNull(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/organizations/acme/projects/default/repository-setup", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	stdout, stderr, err := runCLIForEnvironmentTest(t, server.URL, []string{"braidplane", "project", "repository", "get", "default"})
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.JSONEq(t, "null", stdout)
}

func TestProjectRepositoryGitHubAuthorizationUsesFileAndUserOperation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "authorization.json")
	require.NoError(t, os.WriteFile(file, []byte(`{"state":"isolated-state-with-at-least-32-characters","code":"isolated-user-code","installation_id":9}`), 0o600))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/organizations/acme/projects/default/repository-setup/github-authorization", r.URL.Path)
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.JSONEq(t, `{"state":"isolated-state-with-at-least-32-characters","code":"isolated-user-code","installation_id":9}`, string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"slug":"repo-isolated","project_slug":"default","data_source_slug":"repo-isolated","status":"attached","request":{"provider":"github","owner":"customer","repository_name":"pipelines","create_private_repository":true}}`)
	}))
	defer server.Close()
	stdout, stderr, err := runCLIForEnvironmentTest(t, server.URL, []string{"braidplane", "project", "repository", "complete-github", "default", "--file", file})
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "attached")
	require.NotContains(t, stdout, "isolated-user-code")
}

func TestProjectRepositoryGitHubAuthorizationRejectsInvalidFileBeforeRequest(t *testing.T) {
	file := filepath.Join(t.TempDir(), "authorization.json")
	require.NoError(t, os.WriteFile(file, []byte(`{"state":"short","code":"isolated-user-code","installation_id":9}`), 0o600))
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()
	_, stderr, err := runCLIForEnvironmentTest(t, server.URL, []string{"braidplane", "project", "repository", "complete-github", "default", "--file", file})
	require.Error(t, err)
	require.False(t, called)
	require.NotContains(t, stderr, "isolated-user-code")
}

func TestProjectRepositoryAbandonRequiresExplicitPreservationBeforeRequest(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()
	_, _, err := runCLIForEnvironmentTest(t, server.URL, []string{"braidplane", "project", "repository", "abandon", "default", "--setup", "repo-old"})
	require.ErrorContains(t, err, "--preserve-resources")
	require.False(t, called)
}

func TestProjectRepositoryGetInspectsExactArchivedAttempt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/organizations/acme/projects/default/repository-setups/repo-old", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"slug":"repo-old","project_slug":"default","data_source_slug":"repo-old","status":"pending","abandoned_at":"2026-10-20T18:00:00Z","repository_id":"42","request":{"provider":"github","owner":"customer","repository_name":"old","create_private_repository":true}}`)
	}))
	defer server.Close()
	stdout, stderr, err := runCLIForEnvironmentTest(t, server.URL, []string{"braidplane", "project", "repository", "get", "default", "--setup", "repo-old"})
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "abandoned_at")
	require.Contains(t, stdout, `"repository_id": "42"`)
}
