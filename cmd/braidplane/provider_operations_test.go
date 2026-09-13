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

const providerInstallPayload = `{
	"slug":"install-crm",
	"provider":"hubspot",
	"authorization_url":"https://app.hubspot.test/oauth/authorize?state=opaque-one-time-state",
	"expires_at":"2026-07-19T10:10:00Z",
	"intent":"create_grant",
	"requested_scopes":["oauth","crm.objects.contacts.read"]
}`

const providerRotateInstallPayload = `{
	"slug":"install-crm-rotation",
	"provider":"hubspot",
	"authorization_url":"https://app.hubspot.test/oauth/authorize?state=opaque-rotation-state",
	"expires_at":"2026-07-19T10:10:00Z",
	"intent":"rotate_grant",
	"requested_scopes":["oauth","crm.objects.contacts.read"]
}`

const providerCredentialPayload = `{
	"kind":"oauth2_grant",
	"status":"active",
	"updated_at":"2026-07-19T10:00:00Z",
	"oauth":{
		"provider":"hubspot",
		"scopes":["oauth","crm.objects.contacts.read"],
		"last_refreshed_at":"2026-07-19T10:00:00Z"
	}
}`

func TestDataSourceProviderCommandsUseTypedRoutesBodiesAndBearerAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       []string
		method     string
		path       string
		wantBody   string
		statusCode int
		response   string
		wantOutput string
	}{
		{
			name: "onboard provider data source",
			args: []string{
				"data-source", "provider", "onboard", "hubspot",
				"--name", "Customer CRM",
				"--slug", "customer-crm",
				"--timeout-seconds", "15",
				"--read-only=false",
			},
			method:     http.MethodPost,
			path:       "/api/organizations/acme/provider-connectors/hubspot/data-source-installs",
			wantBody:   `{"name":"Customer CRM","slug":"customer-crm","type":"hubspot","config":{"timeout_seconds":15},"read_only":false}`,
			statusCode: http.StatusCreated,
			response:   `{"data_source":{},"install":` + providerInstallPayload + `}`,
			wantOutput: "https://app.hubspot.test/oauth/authorize?state=opaque-one-time-state",
		},
		{
			name: "onboard GitLab data source",
			args: []string{
				"data-source", "provider", "onboard", "gitlab",
				"--name", "Pipeline repository",
				"--slug", "pipelines",
				"--base-path", "pipelines",
				"--read-only=false",
			},
			method:     http.MethodPost,
			path:       "/api/organizations/acme/provider-connectors/gitlab/data-source-installs",
			wantBody:   `{"name":"Pipeline repository","slug":"pipelines","type":"gitlab","config":{"base_path":"pipelines","provider":"gitlab"},"read_only":false}`,
			statusCode: http.StatusCreated,
			response:   `{"data_source":{},"install":` + providerInstallPayload + `}`,
			wantOutput: "https://app.hubspot.test/oauth/authorize?state=opaque-one-time-state",
		},
		{
			name: "install existing data source grant",
			args: []string{
				"data-source", "provider", "install", "hubspot", "customer-crm",
				"--intent", "rotate_grant",
			},
			method:     http.MethodPost,
			path:       "/api/organizations/acme/provider-connectors/hubspot/installs",
			wantBody:   `{"data_source_slug":"customer-crm","intent":"rotate_grant"}`,
			statusCode: http.StatusCreated,
			response:   providerRotateInstallPayload,
			wantOutput: "https://app.hubspot.test/oauth/authorize?state=opaque-rotation-state",
		},
		{
			name:       "refresh provider grant",
			args:       []string{"data-source", "provider", "grant", "refresh", "customer-crm"},
			method:     http.MethodPost,
			path:       "/api/organizations/acme/data-sources/customer-crm/provider-grant/refresh",
			statusCode: http.StatusOK,
			response:   providerCredentialPayload,
			wantOutput: `"kind": "oauth2_grant"`,
		},
		{
			name:       "revoke provider grant",
			args:       []string{"data-source", "provider", "grant", "revoke", "customer-crm"},
			method:     http.MethodPost,
			path:       "/api/organizations/acme/data-sources/customer-crm/provider-grant/revoke",
			statusCode: http.StatusOK,
			response:   providerCredentialPayload,
			wantOutput: `"kind": "oauth2_grant"`,
		},
		{
			name:       "rotate provider grant",
			args:       []string{"data-source", "provider", "grant", "rotate", "customer-crm"},
			method:     http.MethodPost,
			path:       "/api/organizations/acme/data-sources/customer-crm/provider-grant/rotate",
			statusCode: http.StatusCreated,
			response:   providerRotateInstallPayload,
			wantOutput: "https://app.hubspot.test/oauth/authorize?state=opaque-rotation-state",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath, gotAuth string
			var gotBody []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path
				gotAuth = r.Header.Get("Authorization")
				var err error
				gotBody, err = io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read provider request body: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = io.WriteString(w, tt.response)
			}))
			t.Cleanup(server.Close)

			const apiKey = "provider-cli-test-api-key"
			configPath := filepath.Join(t.TempDir(), "braidplane.toml")
			require.NoError(t, saveLogin(configPath, cliAuthConfig{
				ServerURL:    server.URL + "/api",
				Organization: "acme",
				APIKey:       apiKey,
			}))

			var stdout, stderr bytes.Buffer
			err := runWithRuntime(
				append([]string{"braidplane"}, tt.args...),
				&stdout,
				&stderr,
				cliRuntime{configPath: configPath},
			)

			require.NoError(t, err)
			require.Empty(t, stderr.String())
			require.Equal(t, tt.method, gotMethod)
			require.Equal(t, tt.path, gotPath)
			require.Equal(t, "Bearer "+apiKey, gotAuth)
			if tt.wantBody == "" {
				require.Empty(t, gotBody)
			} else {
				require.JSONEq(t, tt.wantBody, string(gotBody))
			}
			require.NotEmpty(t, stdout.String())
			require.Contains(t, stdout.String(), tt.wantOutput)
			require.NotContains(t, stdout.String(), apiKey)
			require.NotContains(t, stdout.String(), "access_token")
			require.NotContains(t, stdout.String(), "refresh_token")
			require.NotContains(t, stdout.String(), "provider_token")
		})
	}
}

func TestDataSourceProviderCommandPreservesStructuredAPIErrorsWithoutCredentialLeakage(t *testing.T) {
	t.Parallel()

	const apiKey = "provider-cli-secret-api-key"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+apiKey {
			t.Errorf("authorization header = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{
			"error":{
				"code":"DATA_SOURCE_FORBIDDEN",
				"message":"provider grant rotation is not allowed",
				"detailed_error":"role does not grant data_sources:update"
			}
		}`)
	}))
	t.Cleanup(server.Close)

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		APIKey:       apiKey,
	}))

	var stdout, stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "data-source", "provider", "grant", "rotate", "customer-crm"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "403")
	require.ErrorContains(t, err, "DATA_SOURCE_FORBIDDEN")
	require.ErrorContains(t, err, "provider grant rotation is not allowed")
	require.ErrorContains(t, err, "role does not grant data_sources:update")
	require.NotContains(t, err.Error(), apiKey)
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())
}
