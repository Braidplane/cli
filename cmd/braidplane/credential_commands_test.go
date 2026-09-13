package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPersonalAccessTokenCreateUsesPATAndDoesNotDelegateCapability(t *testing.T) {
	t.Setenv("BRAIDPLANE_API_KEY", "api-key-must-not-be-used")

	var gotAuth string
	var gotBody map[string]any
	var handlerErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodPost || r.URL.Path != "/api/users/profile/personal-access-tokens" {
			handlerErr = fmt.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			handlerErr = err
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{
			"slug":"child-pat","name":"Child PAT","token_prefix":"xto_pat_child",
			"user_slug":"alice","can_manage_personal_access_tokens":false,
			"expires_at":null,"revoked_at":null,"last_used_at":null,
			"created_at":"2026-07-19T00:00:00Z","updated_at":"2026-07-19T00:00:00Z",
			"token":"xto_pat_one-time-secret"
		}`))
	}))
	t.Cleanup(server.Close)

	var stdout, stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane", "user", "personal-access-token", "create",
			"--server-url", server.URL + "/api", "--pat", "manager-pat",
			"--name", "Child PAT", "--slug", "child-pat",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: filepath.Join(t.TempDir(), "missing.toml")},
	)

	require.NoError(t, err)
	require.NoError(t, handlerErr)
	require.Equal(t, "Bearer manager-pat", gotAuth)
	require.NotContains(t, gotBody, "can_manage_personal_access_tokens")
	require.Contains(t, stdout.String(), "xto_pat_one-time-secret")
	require.Empty(t, stderr.String())
}

func TestAPIKeyCreateUsesAPIKeyAndDoesNotDelegateCapability(t *testing.T) {
	t.Parallel()

	var gotAuth string
	var gotBody map[string]any
	var handlerErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodPost || r.URL.Path != "/api/organizations/acme/api-keys" {
			handlerErr = fmt.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			handlerErr = err
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{
			"slug":"child-key","name":"Child key","token_prefix":"xto_child",
			"role":"viewer","can_manage_api_keys":false,
			"expires_at":null,
			"revoked_at":null,"last_used_at":null,
			"created_at":"2026-07-19T00:00:00Z","updated_at":"2026-07-19T00:00:00Z",
			"token":"xto_one-time-secret"
		}`))
	}))
	t.Cleanup(server.Close)

	var stdout, stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane", "api-key", "create", "--server-url", server.URL + "/api",
			"--org", "acme", "--api-key", "manager-key", "--name", "Child key",
			"--slug", "child-key", "--role", "viewer",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: filepath.Join(t.TempDir(), "missing.toml")},
	)

	require.NoError(t, err)
	require.NoError(t, handlerErr)
	require.Equal(t, "Bearer manager-key", gotAuth)
	require.NotContains(t, gotBody, "can_manage_api_keys")
	require.Contains(t, stdout.String(), "xto_one-time-secret")
	require.Empty(t, stderr.String())
}

func TestAPIKeyUpdatePreservesExpirationAndCannotToggleCapability(t *testing.T) {
	t.Parallel()

	requestCount := 0
	var handlerErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.URL.Path != "/api/organizations/acme/api-keys/child-key" {
			handlerErr = fmt.Errorf("unexpected request path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(apiKeyMetadataJSON("Child key", "Old description")))
			return
		}
		if r.Method != http.MethodPut {
			handlerErr = fmt.Errorf("unexpected request method %s", r.Method)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			handlerErr = err
		}
		if body["name"] != "Renamed key" || body["description"] != nil || body["expires_at"] != "2026-08-01T00:00:00Z" {
			handlerErr = fmt.Errorf("unexpected update body: %v", body)
		}
		if _, ok := body["can_manage_api_keys"]; ok {
			handlerErr = fmt.Errorf("update body delegated API-key management capability")
		}
		_, _ = w.Write([]byte(apiKeyMetadataJSON("Renamed key", "")))
	}))
	t.Cleanup(server.Close)

	var stdout, stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane", "api-key", "update", "child-key", "--server-url", server.URL + "/api",
			"--org", "acme", "--api-key", "manager-key", "--name", "Renamed key",
			"--clear-description",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: filepath.Join(t.TempDir(), "missing.toml")},
	)

	require.NoError(t, err)
	require.NoError(t, handlerErr)
	require.Equal(t, 2, requestCount)
	require.Contains(t, stdout.String(), "Renamed key")
	require.Empty(t, stderr.String())
}

func TestAPIKeyManagementAuthRejectsPATOnlyConfiguration(t *testing.T) {
	t.Setenv("BRAIDPLANE_API_KEY", "")
	t.Setenv("BRAIDPLANE_PAT", "pat-only")

	_, err := (apiKeyManagementAuthFlags{
		ServerURL:    "https://api.example.test",
		Organization: "acme",
	}).resolveAuthConfig(&cliRuntime{
		configPath:      filepath.Join(t.TempDir(), "missing.toml"),
		credentialStore: newMemoryCredentialStore(),
	})

	require.Error(t, err)
	require.ErrorContains(t, err, "CLI API key")
}

func apiKeyMetadataJSON(name, description string) string {
	return `{
		"slug":"child-key","name":` + string(mustJSON(name)) + `,
		"description":` + string(mustJSON(description)) + `,"token_prefix":"xto_child",
		"role":"viewer","can_manage_api_keys":false,
		"expires_at":"2026-08-01T00:00:00Z",
		"revoked_at":null,"last_used_at":null,
		"created_at":"2026-07-19T00:00:00Z","updated_at":"2026-07-19T00:00:00Z"
	}`
}

func mustJSON(value string) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}
