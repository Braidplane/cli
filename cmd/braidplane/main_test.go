package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunWithArgsPipelineValidateFile(t *testing.T) {
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

	dir := t.TempDir()
	specPath := filepath.Join(dir, "test.pipeline.yaml")
	require.NoError(t, os.WriteFile(specPath, validSpec, 0o600))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithArgs([]string{"braidplane", "pipeline", "validate", "--file", specPath}, &stdout, &stderr)

	require.NoError(t, err)
	require.Contains(t, stdout.String(), "pipeline validation succeeded for "+specPath)
	require.Empty(t, stderr.String())
}

func TestRunWithArgsPipelineValidateDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "pipelines"), 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "pipelines", "users.pipeline.yaml"),
		validPipelineSpec("acme-users-sync", "ACME Users Sync", "fetch-users", "list-users"),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "pipelines", "orders.pipeline.json"),
		[]byte(`{
			"metadata": {"slug": "acme-orders-sync", "name": "ACME Orders Sync"},
			"steps": [{
				"kind": "query",
				"slug": "fetch-orders",
				"query": {
					"data_source_ref": "acme-db",
					"operation_type": "sql",
					"operation_ref": "list-orders"
				}
			}]
		}`),
		0o600,
	))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pipelines", "notes.yaml"), []byte("not a pipeline"), 0o600))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithArgs([]string{"braidplane", "pipeline", "validate", "--dir", dir}, &stdout, &stderr)

	require.NoError(t, err)
	require.Contains(
		t,
		stdout.String(),
		"pipeline validation succeeded for 2 pipeline spec(s) in "+dir,
	)
	require.Empty(t, stderr.String())
}

func TestRunWithArgsPipelineDiff(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "braidplane.toml")
	specPath := filepath.Join(dir, "candidate.pipeline.yaml")
	require.NoError(t, os.WriteFile(
		specPath,
		pipelineSpecWithSinkTable("accounts"),
		0o600,
	))
	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    "http://example.invalid",
		Organization: "acme",
		APIKey:       "xto_saved_token",
	}))

	var gotAuth string
	var gotPath string
	var gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
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
							"operation_ref": "list-active-users",
							"output_contract": {
								"representation": "rows",
								"fields": [
									{"name": "email", "type": "string", "required": true}
								]
							}
						}
					},
					{
						"kind": "sink",
						"slug": "acme-users-sync",
						"sink": {
                    "idempotency_class": "idempotent_with_key",
							"input": "fetch-users",
							"data_source_ref": "acme-mysql",
							"operation_type": "sql",
							"operation_ref": "upsert-users",
							"config": {
								"mode": "upsert",
								"table": "users"
							}
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
		APIKey:       "xto_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "diff", "acme-users-sync", "--file", specPath, "--project", "data-pipelines"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Equal(t, http.MethodGet, gotMethod)
	require.Equal(t, "/api/organizations/acme/projects/data-pipelines/pipelines/acme-users-sync", gotPath)
	require.Equal(t, "Bearer xto_saved_token", gotAuth)
	require.Contains(t, stdout.String(), "severity: warning")
	require.Contains(t, stdout.String(), "step[acme-users-sync].sink.config.table changed")
	require.Contains(t, stdout.String(), "users")
	require.Contains(t, stdout.String(), "accounts")
	require.Empty(t, stderr.String())
}

func TestRunWithArgsPipelineDiffFailsForBreakingChanges(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "braidplane.toml")
	specPath := filepath.Join(dir, "candidate.pipeline.yaml")
	require.NoError(t, os.WriteFile(
		specPath,
		validPipelineSpec("acme-users-sync", "ACME Users Sync", "fetch-users", "list-active-users"),
		0o600,
	))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
							"operation_ref": "list-active-users",
							"output_contract": {
								"representation": "rows",
								"fields": [
									{"name": "email", "type": "string", "required": true}
								]
							}
						}
					},
					{
						"kind": "sink",
						"slug": "acme-users-sync",
						"sink": {
                    "idempotency_class": "idempotent_with_key",
							"input": "fetch-users",
							"data_source_ref": "acme-mysql",
							"operation_type": "sql",
							"operation_ref": "upsert-users",
							"config": {
								"mode": "upsert",
								"table": "users"
							}
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
		APIKey:       "xto_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "diff", "acme-users-sync", "--file", specPath, "--project", "data-pipelines"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "breaking pipeline changes detected")
	require.Contains(t, stdout.String(), "severity: breaking")
	require.Contains(t, stdout.String(), `step "acme-users-sync" was removed`)
	require.Empty(t, stderr.String())
}

func TestRunWithArgsPipelineDiffRejectsMismatchedSpecSlugBeforeHTTP(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "braidplane.toml")
	specPath := filepath.Join(dir, "candidate.pipeline.yaml")
	require.NoError(t, os.WriteFile(
		specPath,
		validPipelineSpec("nightly-export", "Nightly Export", "fetch-users", "list-deleted-users"),
		0o600,
	))
	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    "http://example.invalid",
		Organization: "acme",
		APIKey:       "xto_saved_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "pipeline", "diff", "acme-users-sync", "--file", specPath},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "pipeline slug mismatch")
	require.ErrorContains(t, err, "acme-users-sync")
	require.ErrorContains(t, err, "nightly-export")
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())
}

func validPipelineSpec(pipelineSlug, pipelineName, stepSlug, operationRef string) []byte {
	return []byte(`
metadata:
  slug: ` + pipelineSlug + `
  name: ` + pipelineName + `
steps:
  - kind: query
    slug: ` + stepSlug + `
    query:
      data_source_ref: acme-db
      operation_type: sql
      operation_ref: ` + operationRef + `
`)
}

func pipelineSpecWithSinkTable(table string) []byte {
	return []byte(`
metadata:
  slug: acme-users-sync
  name: ACME Users Sync
steps:
  - kind: query
    slug: fetch-users
    query:
      data_source_ref: acme-db
      operation_type: sql
      operation_ref: list-active-users
      output_contract:
        representation: rows
        fields:
          - name: email
            type: string
            required: true
  - kind: sink
    slug: acme-users-sync
    sink:
      idempotency_class: idempotent_with_key
      input: fetch-users
      data_source_ref: acme-mysql
      operation_type: sql
      operation_ref: upsert-users
      config:
        mode: upsert
        table: ` + table + `
`)
}

func TestRunWithArgsRejectsUnknownTopLevelCommand(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithArgs([]string{"braidplane", "unknown"}, &stdout, &stderr)

	require.Error(t, err)
	require.Empty(t, stdout.String())
	require.Contains(t, stderr.String(), "unknown")
}

func TestRunWithArgsLoginSavesConfigAndRedactsToken(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotAuth string
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"slug":"acme",
			"name":"ACME",
			"created_at":"2026-05-14T00:00:00Z",
			"updated_at":"2026-05-14T00:00:00Z"
		}`))
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	store := newMemoryCredentialStore()
	err := runWithRuntime(
		[]string{
			"braidplane",
			"login",
			"--server-url", server.URL,
			"--org", "acme",
			"--api-key", "xto_secret_token",
			"--environment", "dev",
		},
		&stdout,
		&stderr,
		cliRuntime{
			configPath:      configPath,
			credentialStore: store,
		},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Contains(t, stdout.String(), server.URL)
	require.Contains(t, stdout.String(), "acme")
	require.NotContains(t, stdout.String(), "xto_secret_token")
	require.Equal(t, "Bearer xto_secret_token", gotAuth)
	require.Equal(t, "/organizations/acme", gotPath)

	got, err := loadLogin(configPath)
	require.NoError(t, err)
	require.Equal(t, cliAuthConfig{
		ServerURL:    server.URL,
		Organization: "acme",
		Environment:  "dev",
	}, got)

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NotContains(t, string(data), "xto_secret_token")
	require.NotContains(t, string(data), "api_key")
	storedToken, err := store.Get(credentialTypeAPIKey, "acme", server.URL)
	require.NoError(t, err)
	require.Equal(t, "xto_secret_token", storedToken)
}

func TestRunWithArgsLoginSavesPATSeparatelyFromAPIKey(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"slug":"acme",
			"name":"ACME",
			"created_at":"2026-05-14T00:00:00Z",
			"updated_at":"2026-05-14T00:00:00Z"
		}`))
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	store := newMemoryCredentialStore()
	err := runWithRuntime(
		[]string{
			"braidplane",
			"login",
			"--server-url", server.URL,
			"--org", "acme",
			"--pat", "xto_pat_secret_token",
		},
		&stdout,
		&stderr,
		cliRuntime{
			configPath:      configPath,
			credentialStore: store,
		},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.NotContains(t, stdout.String(), "xto_pat_secret_token")
	require.Equal(t, "Bearer xto_pat_secret_token", gotAuth)

	got, err := loadLogin(configPath)
	require.NoError(t, err)
	require.Equal(t, cliAuthConfig{
		ServerURL:    server.URL,
		Organization: "acme",
	}, got)

	storedPAT, err := store.Get(credentialTypePAT, "acme", server.URL)
	require.NoError(t, err)
	require.Equal(t, "xto_pat_secret_token", storedPAT)
	_, err = store.Get(credentialTypeAPIKey, "acme", server.URL)
	require.ErrorIs(t, err, errMemoryCredentialNotFound)
}

func TestRunWithArgsLoginPATIgnoresAPIKeyEnv(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	t.Setenv("BRAIDPLANE_API_KEY", "xto_env_api_key")

	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"slug":"acme",
			"name":"ACME",
			"created_at":"2026-05-14T00:00:00Z",
			"updated_at":"2026-05-14T00:00:00Z"
		}`))
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	store := newMemoryCredentialStore()
	err := runWithRuntime(
		[]string{
			"braidplane",
			"login",
			"--server-url", server.URL,
			"--org", "acme",
			"--pat", "xto_pat_secret_token",
		},
		&stdout,
		&stderr,
		cliRuntime{
			configPath:      configPath,
			credentialStore: store,
		},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Equal(t, "Bearer xto_pat_secret_token", gotAuth)
	storedPAT, err := store.Get(credentialTypePAT, "acme", server.URL)
	require.NoError(t, err)
	require.Equal(t, "xto_pat_secret_token", storedPAT)
	_, err = store.Get(credentialTypeAPIKey, "acme", server.URL)
	require.ErrorIs(t, err, errMemoryCredentialNotFound)
}

func TestRunWithArgsLoginCanExplicitlyStoreAPIKeyInConfig(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotAuth string
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"slug":"acme",
			"name":"ACME",
			"created_at":"2026-05-14T00:00:00Z",
			"updated_at":"2026-05-14T00:00:00Z"
		}`))
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	store := newMemoryCredentialStore()
	err := runWithRuntime(
		[]string{
			"braidplane",
			"login",
			"--server-url", server.URL,
			"--org", "acme",
			"--api-key", "xto_secret_token",
			"--insecure-store-api-key-in-config",
		},
		&stdout,
		&stderr,
		cliRuntime{
			configPath:      configPath,
			credentialStore: store,
		},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.NotContains(t, stdout.String(), "xto_secret_token")
	require.Equal(t, "Bearer xto_secret_token", gotAuth)
	require.Equal(t, "/organizations/acme", gotPath)

	got, err := loadLogin(configPath)
	require.NoError(t, err)
	require.Equal(t, cliAuthConfig{
		ServerURL:    server.URL,
		Organization: "acme",
		APIKey:       "xto_secret_token",
	}, got)
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "api_key")
	require.Contains(t, string(data), "xto_secret_token")
	_, credentialErr := store.Get(credentialTypeAPIKey, "acme", server.URL)
	require.ErrorIs(t, credentialErr, errMemoryCredentialNotFound)
}

func TestRunWithArgsLoginRequiresOrgAndToken(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane",
			"login",
			"--server-url", "https://api.example.test",
		},
		&stdout,
		&stderr,
		cliRuntime{
			configPath:      configPath,
			credentialStore: newMemoryCredentialStore(),
		},
	)

	require.Error(t, err)
	require.Empty(t, stdout.String())
	require.Contains(t, stderr.String(), "--org")
	require.Contains(t, stderr.String(), "--api-key or --pat")
	require.NotContains(t, stderr.String(), "xto_secret")
}

func TestRunWithArgsLoginDoesNotSaveWhenVerificationFails(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"UNAUTHORIZED","message":"API key rejected"}}`))
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane",
			"login",
			"--server-url", server.URL,
			"--org", "acme",
			"--api-key", "xto_secret_token",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "UNAUTHORIZED")
	require.ErrorContains(t, err, "API key rejected")
	require.Empty(t, stdout.String())
	require.NotContains(t, stderr.String(), "xto_secret_token")
	_, statErr := os.Stat(configPath)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestRunWithArgsLogoutDeletesSavedLoginAndCredential(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	store := newMemoryCredentialStore()
	require.NoError(t, saveLoginWithCredentialStore(configPath, store, cliAuthConfig{
		ServerURL:    "https://api.example.test",
		Organization: "acme",
		APIKey:       "xto_secret_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "logout"},
		&stdout,
		&stderr,
		cliRuntime{
			configPath:      configPath,
			credentialStore: store,
		},
	)

	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Logged out")
	require.Empty(t, stderr.String())
	_, statErr := os.Stat(configPath)
	require.ErrorIs(t, statErr, os.ErrNotExist)
	_, credentialErr := store.Get(credentialTypeAPIKey, "acme", "https://api.example.test")
	require.ErrorIs(t, credentialErr, errMemoryCredentialNotFound)
}

func TestRunWithArgsLogoutDeletesInsecureSavedLoginWithoutCredentialStore(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    "https://api.example.test",
		Organization: "acme",
		APIKey:       "xto_secret_token",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "logout"},
		&stdout,
		&stderr,
		cliRuntime{
			configPath:      configPath,
			credentialStore: failingDeleteCredentialStore{err: errMemoryCredentialDeleteFailed},
		},
	)

	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Logged out")
	require.Empty(t, stderr.String())
	_, statErr := os.Stat(configPath)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}
