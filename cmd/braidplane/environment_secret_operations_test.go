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

	"github.com/stretchr/testify/require"
)

func TestEnvironmentSecretValueRequiresExactlyOneInput(t *testing.T) {
	t.Parallel()

	_, err := environmentSecretValue("inline", "secret.txt")
	require.ErrorContains(t, err, "only one")
	_, err = environmentSecretValue("", "")
	require.ErrorContains(t, err, "required")
}

func TestEnvironmentSecretLifecycleKeepsReadbackSafe(t *testing.T) {
	t.Parallel()

	const secretValue = "never-return-this-secret"
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		if r.Method == http.MethodPut {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode environment secret body: %v", err)
			}
			if body["value"] != secretValue {
				t.Errorf("environment secret value mismatch")
			}
			if body["protected_environment_confirmation"] != "prod" {
				t.Errorf("protected confirmation mismatch")
			}
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"environment_slug":"prod","key":"WAREHOUSE_PASSWORD","description":"Warehouse credential","created_at":"2026-07-19T10:00:00Z","updated_at":"2026-07-19T10:00:00Z"}`)
	}))
	t.Cleanup(server.Close)

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	require.NoError(t, saveLogin(configPath, cliAuthConfig{ServerURL: server.URL + "/api", Organization: "acme", APIKey: "test-token"}))
	secretFile := filepath.Join(t.TempDir(), "secret-value")
	require.NoError(t, os.WriteFile(secretFile, []byte(secretValue), 0o600))

	for _, args := range [][]string{
		{"braidplane", "environment", "secret", "upsert", "WAREHOUSE_PASSWORD", "--environment", "prod", "--value-file", secretFile, "--description", "Warehouse credential", "--confirm-environment", "prod"},
		{"braidplane", "environment", "secret", "get", "WAREHOUSE_PASSWORD", "--environment", "prod"},
		{"braidplane", "environment", "secret", "delete", "WAREHOUSE_PASSWORD", "--environment", "prod", "--confirm-environment", "prod"},
	} {
		var stdout, stderr bytes.Buffer
		err := runWithRuntime(args, &stdout, &stderr, cliRuntime{configPath: configPath})
		require.NoError(t, err)
		require.Empty(t, stderr.String())
		require.NotContains(t, stdout.String(), secretValue)
	}

	require.Equal(t, []string{
		"PUT /api/organizations/acme/environments/prod/secrets/WAREHOUSE_PASSWORD",
		"GET /api/organizations/acme/environments/prod/secrets/WAREHOUSE_PASSWORD",
		"DELETE /api/organizations/acme/environments/prod/secrets/WAREHOUSE_PASSWORD?protected_environment_confirmation=prod",
	}, requests)
	for _, request := range requests {
		require.NotContains(t, request, secretValue)
	}
}

func TestBP804ReadAndRecoveryCommandRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		args   []string
		method string
		path   string
		status int
	}{
		{name: "environment secret metadata", args: []string{"environment", "secret", "list", "--environment", "prod"}, method: http.MethodGet, path: "/api/organizations/acme/environments/prod/secrets", status: http.StatusOK},
		{name: "stored data source test", args: []string{"data-source", "test-stored", "warehouse"}, method: http.MethodPost, path: "/api/organizations/acme/data-sources/warehouse/test-connection", status: http.StatusNoContent},
		{name: "data source branches", args: []string{"data-source", "branch", "list", "repo"}, method: http.MethodGet, path: "/api/organizations/acme/data-sources/repo/branches", status: http.StatusOK},
		{name: "deployment history", args: []string{"pipeline", "deployment", "list", "--project", "etl", "--environment", "prod"}, method: http.MethodGet, path: "/api/organizations/acme/projects/etl/environments/prod/pipeline-deployments", status: http.StatusOK},
		{name: "validation history", args: []string{"pipeline", "validation", "list", "--project", "etl", "--environment", "prod"}, method: http.MethodGet, path: "/api/organizations/acme/projects/etl/environments/prod/pipeline-validation-results", status: http.StatusOK},
		{name: "drift reports", args: []string{"pipeline", "drift", "list", "--project", "etl"}, method: http.MethodGet, path: "/api/organizations/acme/projects/etl/drift-impact-reports", status: http.StatusOK},
		{name: "schedule fires", args: []string{"pipeline", "trigger", "fire", "list", "nightly", "--project", "etl", "--environment", "prod"}, method: http.MethodGet, path: "/api/organizations/acme/projects/etl/environments/prod/pipeline-triggers/nightly/fires", status: http.StatusOK},
		{name: "webhook deliveries", args: []string{"pipeline", "trigger", "delivery", "list", "--trigger", "webhook", "--project", "etl", "--environment", "prod"}, method: http.MethodGet, path: "/api/organizations/acme/projects/etl/environments/prod/pipeline-triggers/webhook/deliveries", status: http.StatusOK},
		{name: "webhook delivery", args: []string{"pipeline", "trigger", "delivery", "get", "delivery-1", "--trigger", "webhook", "--project", "etl", "--environment", "prod"}, method: http.MethodGet, path: "/api/organizations/acme/projects/etl/environments/prod/pipeline-triggers/webhook/deliveries/delivery-1", status: http.StatusOK},
		{name: "webhook delivery replay", args: []string{"pipeline", "trigger", "delivery", "replay", "delivery-1", "--trigger", "webhook", "--project", "etl", "--environment", "prod"}, method: http.MethodPost, path: "/api/organizations/acme/projects/etl/environments/prod/pipeline-triggers/webhook/deliveries/delivery-1/replay", status: http.StatusOK},
		{name: "notification delivery", args: []string{"notification", "delivery", "list", "notif-1"}, method: http.MethodGet, path: "/api/organizations/acme/notifications/notif-1/delivery-attempts", status: http.StatusOK},
		{name: "alert delivery", args: []string{"alert", "delivery", "list", "--notification", "notif-1"}, method: http.MethodGet, path: "/api/organizations/acme/alert-delivery-attempts", status: http.StatusOK},
		{name: "alert source metadata", args: []string{"alert", "policy", "source-metadata"}, method: http.MethodGet, path: "/api/organizations/acme/alert-policy-source-metadata", status: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var gotMethod, gotPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.Path
				if tt.status == http.StatusOK {
					w.Header().Set("Content-Type", "application/json")
				}
				w.WriteHeader(tt.status)
				if tt.status == http.StatusOK {
					_, _ = io.WriteString(w, `{}`)
				}
			}))
			t.Cleanup(server.Close)

			configPath := filepath.Join(t.TempDir(), "braidplane.toml")
			require.NoError(t, saveLogin(configPath, cliAuthConfig{ServerURL: server.URL + "/api", Organization: "acme", APIKey: "test-token"}))
			var stdout, stderr bytes.Buffer
			err := runWithRuntime(append([]string{"braidplane"}, tt.args...), &stdout, &stderr, cliRuntime{configPath: configPath})

			require.NoError(t, err)
			require.Empty(t, stderr.String())
			require.Equal(t, tt.method, gotMethod)
			require.Equal(t, tt.path, gotPath)
		})
	}
}

func TestPipelineRevalidateRequiresPATAndUsesTypedRoute(t *testing.T) {
	t.Parallel()

	var gotAuthorization, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"validation_slug":"validation-daily","project_slug":"etl","environment_slug":"prod","pipeline_slug":"daily","source":"revalidate","valid":true,"diagnostics":[],"created_at":"2026-07-19T10:00:00Z"}`)
	}))
	t.Cleanup(server.Close)

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	require.NoError(t, saveLogin(configPath, cliAuthConfig{ServerURL: server.URL + "/api", Organization: "acme", PAT: "user-pat"}))

	var stdout, stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "validation", "revalidate", "daily", "--project", "etl", "--environment", "prod"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Equal(t, "Bearer user-pat", gotAuthorization)
	require.Equal(t, "/api/organizations/acme/projects/etl/environments/prod/pipelines/daily:revalidate", gotPath)
	require.Contains(t, stdout.String(), `"validation_slug": "validation-daily"`)
}

func TestPipelineRevalidateRejectsAPIKeyOnlyConfiguration(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    "https://braidplane.example",
		Organization: "acme",
		APIKey:       "organization-key",
	}))

	var stdout, stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "validation", "revalidate", "daily", "--project", "etl", "--environment", "prod"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "personal access token")
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())
}
