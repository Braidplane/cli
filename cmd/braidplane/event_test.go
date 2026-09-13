package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/xerror"
)

func TestRunWithArgsEventListUsesSavedLoginAndPrintsEvents(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotAuth string
	var gotPath string
	var gotMethod string
	var gotLimit string
	var gotCursor string
	var gotDirection string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotLimit = r.URL.Query().Get("limit")
		gotCursor = r.URL.Query().Get("cursor")
		gotDirection = r.URL.Query().Get("direction")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"events": [
				{
					"slug": "evt-run-execute-001",
					"actor_type": "api_key",
					"actor_slug": "ci-key",
					"entity_type": "pipeline_run",
					"entity_slug": "run-001",
					"action": "execute",
					"occurred_at": "2026-05-15T08:00:00Z",
					"metadata": {
						"pipeline_slug": "acme-users-sync",
						"environment_slug": "prod",
						"status": "failed"
					},
					"created_at": "2026-05-15T08:00:01Z"
				}
			],
			"has_more": true,
			"next_cursor": "2026-05-15T08:00:00Z_evt-run-execute-001",
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
			"braidplane", "event", "list",
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
	require.Equal(t, "/api/organizations/acme/events", gotPath)
	require.Equal(t, "2", gotLimit)
	require.Equal(t, "cursor-123", gotCursor)
	require.Equal(t, "prev", gotDirection)
	require.Contains(t, stdout.String(), `"events": [`)
	require.Contains(t, stdout.String(), `"slug": "evt-run-execute-001"`)
	require.Contains(t, stdout.String(), `"pipeline_slug": "acme-users-sync"`)
}

func TestRunWithArgsEventListPassesFilters(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	gotQuery := make(map[string]string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, key := range []string{
			"limit",
			"cursor",
			"actor_type",
			"actor_slug",
			"entity_type",
			"entity_slug",
			"action",
			"sort_by",
			"order",
			"direction",
		} {
			gotQuery[key] = r.URL.Query().Get(key)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"events": [],
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
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane", "event", "list",
			"--limit", "10",
			"--cursor", "cursor-123",
			"--actor-type", "user",
			"--actor-slug", "alice",
			"--entity-type", "pipeline_run",
			"--entity-slug", "run-001",
			"--action", "execute",
			"--sort-by", "event_occurred_at",
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
		"limit":       "10",
		"cursor":      "cursor-123",
		"actor_type":  "user",
		"actor_slug":  "alice",
		"entity_type": "pipeline_run",
		"entity_slug": "run-001",
		"action":      "execute",
		"sort_by":     "event_occurred_at",
		"order":       "asc",
		"direction":   "next",
	}, gotQuery)
	require.Contains(t, stdout.String(), `"events": []`)
}

func TestRunWithArgsEventListMapsStructuredAPIError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{
			"error": {
				"code": "VALIDATION_FAILED",
				"message": "validation failed",
				"detailed_error": "cursor is invalid for event pagination"
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
			"braidplane", "event", "list",
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

func TestRunWithArgsEventListUsesDefaultsWhenOptionalFiltersOmitted(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotAuth string
	var gotPath string
	var gotMethod string
	var gotRawQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"events": [],
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
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "event", "list"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Equal(t, http.MethodGet, gotMethod)
	require.Equal(t, "Bearer xto_saved_token", gotAuth)
	require.Equal(t, "/api/organizations/acme/events", gotPath)
	require.Empty(t, gotRawQuery)
	require.Contains(t, stdout.String(), `"events": []`)
}

func TestRunWithArgsEventListRejectsInvalidEnumFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		wantDetails []string
	}{
		{
			name:        "ActorType",
			args:        []string{"--actor-type", "robot"},
			wantDetails: []string{"actor-type must be one of api_key, external, system, unknown, or user"},
		},
		{
			name: "EntityType",
			args: []string{"--entity-type", "workspace"},
			wantDetails: []string{
				"entity-type must be one of",
				"pipeline_run",
				"query_execution",
				"unknown",
			},
		},
		{
			name: "Action",
			args: []string{"--action", "archive"},
			wantDetails: []string{
				"action must be one of",
				"test_connection",
				"unknown",
				"update",
			},
		},
		{
			name:        "SortBy",
			args:        []string{"--sort-by", "created_at"},
			wantDetails: []string{"sort-by must be event_occurred_at"},
		},
		{
			name:        "Order",
			args:        []string{"--order", "latest"},
			wantDetails: []string{"order must be asc or desc"},
		},
		{
			name:        "Direction",
			args:        []string{"--direction", "forward"},
			wantDetails: []string{"direction must be next or prev"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			configPath := filepath.Join(t.TempDir(), "braidplane.toml")
			var serverCalled atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				serverCalled.Store(true)
			}))
			t.Cleanup(server.Close)

			require.NoError(t, saveLogin(configPath, cliAuthConfig{
				ServerURL:    server.URL + "/api",
				Organization: "acme",
				APIKey:       "xto_saved_token",
			}))

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			args := append([]string{"braidplane", "event", "list"}, tc.args...)
			err := runWithRuntime(args, &stdout, &stderr, cliRuntime{configPath: configPath})

			requireValidationFailed(t, err, tc.wantDetails...)
			require.False(t, serverCalled.Load())
			require.Empty(t, stdout.String())
			require.Empty(t, stderr.String())
		})
	}
}

func TestRunWithArgsEventListRejectsInvalidLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
	}{
		{name: "Negative", value: "-1"},
		{name: "Zero", value: "0"},
		{name: "AboveMaximum", value: "101"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			configPath := filepath.Join(t.TempDir(), "braidplane.toml")
			var serverCalled atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				serverCalled.Store(true)
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
				[]string{"braidplane", "event", "list", "--limit=" + tc.value},
				&stdout,
				&stderr,
				cliRuntime{configPath: configPath},
			)

			requireValidationFailed(t, err, "limit must be between 1 and 100")
			require.False(t, serverCalled.Load())
			require.Empty(t, stdout.String())
			require.Empty(t, stderr.String())
		})
	}
}

func requireValidationFailed(t *testing.T, err error, wantDetails ...string) {
	t.Helper()

	require.Error(t, err)
	var got *xerror.Error
	require.True(t, xerror.As(err, &got))
	require.Equal(t, xerror.KindInvalidInput, got.Kind())
	require.Equal(t, "VALIDATION_FAILED", got.Code())
	require.ErrorContains(t, err, "validation failed")
	for _, wantDetail := range wantDetails {
		require.ErrorContains(t, err, wantDetail)
	}
}
