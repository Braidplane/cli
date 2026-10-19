package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/xerror"
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
			name:       "connect HTTP OAuth machine",
			args:       []string{"data-source", "provider", "connect-http", "http-app"},
			method:     http.MethodPost,
			path:       "/api/organizations/acme/data-sources/http-app/http-oauth/connect",
			statusCode: http.StatusOK,
			response:   `{"data_source":{"slug":"http-app","is_active":true}}`,
			wantOutput: "http-app",
		},
		{
			name:       "connect HTTP OAuth consent",
			args:       []string{"data-source", "provider", "connect-http", "http-app"},
			method:     http.MethodPost,
			path:       "/api/organizations/acme/data-sources/http-app/http-oauth/connect",
			statusCode: http.StatusOK,
			response:   `{"data_source":{"slug":"http-app"},"install":` + providerInstallPayload + `}`,
			wantOutput: "https://app.hubspot.test/oauth/authorize?state=opaque-one-time-state",
		},
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

func TestGitLabOnboardingRequiresOrganizationConnection(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := runWithRuntime([]string{"braidplane", "data-source", "provider", "onboard", "gitlab", "--name", "Pipeline repository"}, &stdout, &stderr, cliRuntime{configPath: filepath.Join(t.TempDir(), "unused.toml")})
	require.ErrorContains(t, err, "Connect GitLab in organization settings")
	require.ErrorContains(t, err, "project_id")
	require.Empty(t, stdout.String())
}

func TestWorkspaceOnboardingAndRotationCarryConsentMode(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/data-source-installs") {
			raw, _ := io.ReadAll(r.Body)
			body = string(raw)
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"data_source":{},"install":`+providerInstallPayload+`}`)
			return
		}
		assert.Empty(t, r.URL.Query().Get("access_mode"))
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, providerRotateInstallPayload)
	}))
	defer server.Close()
	for _, args := range [][]string{
		{"data-source", "provider", "onboard", "google_workspace", "--name", "Sheet", "--read-only=false"},
		{"data-source", "provider", "grant", "rotate", "sheet"},
	} {
		_, _, err := runCLIForDataSourceTest(t, server.URL, append([]string{"braidplane"}, args...))
		require.NoError(t, err)
	}
	require.JSONEq(t, `{"name":"Sheet","type":"google_workspace","config":{"access_mode":"selected_files"},"read_only":false}`, body)
}

func TestWorkspaceFilesCommandCarriesFiltersAndPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/organizations/acme/data-sources/sheet/google-workspace/files", r.URL.Path)
		assert.Equal(t, "folder-1", r.URL.Query().Get("folder_id"))
		assert.Equal(t, "drive-1", r.URL.Query().Get("drive_id"))
		assert.Equal(t, "application/vnd.google-apps.spreadsheet", r.URL.Query().Get("mime_type"))
		assert.Equal(t, "opaque-page", r.URL.Query().Get("page_token"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"files":[{"file_id":"sheet-1","name":"Synthetic","mime_type":"application/vnd.google-apps.spreadsheet"}],"next_page_token":"more"}`)
	}))
	defer server.Close()
	output, _, err := runCLIForDataSourceTest(t, server.URL, []string{"braidplane", "data-source", "provider", "files", "sheet", "--folder-id", "folder-1", "--drive-id", "drive-1", "--mime-type", "application/vnd.google-apps.spreadsheet", "--page-token", "opaque-page"})
	require.NoError(t, err)
	require.Contains(t, output, "sheet-1")
	require.Contains(t, output, "more")
}

func TestWorkspaceConfigPreservesOmittedConsentMode(t *testing.T) {
	cfg, err := (dataSourceRequestFlags{}).workspaceConfig()
	require.NoError(t, err)
	require.Nil(t, cfg.AccessMode)
	cfg, err = (dataSourceRequestFlags{WorkspaceAccessMode: "selected_files"}).workspaceConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg.AccessMode)
	require.Equal(t, "selected_files", string(*cfg.AccessMode))
	_, err = (dataSourceRequestFlags{WorkspaceAccessMode: "invalid"}).workspaceConfig()
	require.Error(t, err)
}

func TestQueryStatementDiscriminatorErrorIsTyped(t *testing.T) {
	_, err := queryStatement(`{"type":7}`, "")
	require.True(t, xerror.Is(err, xerror.InvalidInput(xerror.JSON)))
}

func TestWorkspaceVerifyUsesAuthorizedIDsAndCurrentRevision(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			assert.Equal(t, "/api/organizations/acme/data-sources/sheet", r.URL.Path)
			_, _ = io.WriteString(w, `{"slug":"sheet","type":"google_workspace","connection_revision":17}`)
			return
		}
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/organizations/acme/data-sources/sheet/google-workspace/files/verify", r.URL.Path)
		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.JSONEq(t, `{"file_ids":["authorized-file","another-file"],"expected_revision":17}`, string(raw))
		_, _ = io.WriteString(w, `{"files":[{"file_id":"authorized-file","name":"Synthetic","mime_type":"application/vnd.google-apps.spreadsheet"}]}`)
	}))
	defer server.Close()
	_, _, err := runCLIForDataSourceTest(t, server.URL, []string{"braidplane", "data-source", "provider", "verify", "sheet", "--file-id", "authorized-file", "--file-id", "another-file"})
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}

func TestWorkspaceMetadataAndWorksheetsCommandsUseTypedRoutes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/organizations/acme/data-sources/sheet/google-workspace/files/file-1":
			_, _ = io.WriteString(w, `{"file_id":"file-1","name":"Synthetic","mime_type":"application/vnd.google-apps.spreadsheet"}`)
		case "/api/organizations/acme/data-sources/sheet/google-workspace/spreadsheets/sheet-1/worksheets":
			_, _ = io.WriteString(w, `{"spreadsheet_id":"sheet-1","title":"Synthetic","worksheets":[{"worksheet_id":0,"title":"Sheet1","row_count":10,"column_count":5}]}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	for _, args := range [][]string{
		{"braidplane", "data-source", "provider", "metadata", "sheet", "--file-id", "file-1"},
		{"braidplane", "data-source", "provider", "worksheets", "sheet", "--spreadsheet-id", "sheet-1"},
	} {
		output, _, err := runCLIForDataSourceTest(t, server.URL, args)
		require.NoError(t, err)
		require.Contains(t, output, "Synthetic")
	}
}
