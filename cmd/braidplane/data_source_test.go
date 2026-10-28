package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

const (
	dataSourceTestSecretValue = "secret-http-bearer-token"
)

const (
	dataSourceTestPasswordValue           = "secret-password"
	dataSourceTestPrivateAppTokenValue    = "secret-private-app-token"
	dataSourceTestServiceAccountJSONValue = `{"client_email":"svc@example.test"}`
	dataSourceTestGitTokenValue           = "secret-git-token"
	dataSourceTestSSHPrivateKeyValue      = "secret-ssh-private-key"
	dataSourceTestSSHPassphraseValue      = "secret-ssh-passphrase"
	dataSourceTestSSHKnownHostsValue      = "github.example.test ssh-ed25519 AAAA"
	dataSourceTestAccessKeyIDValue        = "secret-access-key-id"
	dataSourceTestSecretAccessKeyValue    = "secret-access-key"
	dataSourceTestSessionTokenValue       = "secret-session-token"
)

func TestRunWithArgsDataSourceMutationCommandsUseExpectedRequests(t *testing.T) {
	t.Parallel()

	dataSourcePayload := `{
		"slug":"customer-api",
		"name":"Customer API",
		"type":"http",
		"config":{
			"base_url":"https://customer-api.example.test",
			"health_path":"/health",
			"timeout_seconds":10,
			"openapi_url":"https://customer-api.example.test/openapi.json",
			"openapi_fetch_timeout_seconds":12,
			"allow_private_hosts":true
		},
		"is_active":true,
		"read_only":true,
		"credential":{"kind":"static_secret","status":"active","updated_at":"2026-03-01T00:00:00Z"},
		"last_tested_at":"2026-03-01T00:00:00Z",
		"test_error":null,
		"created_at":"2026-03-01T00:00:00Z",
		"updated_at":"2026-03-01T00:00:00Z"
	}`
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
			name: "HttpDataSourceCreate",
			args: []string{
				"braidplane", "data-source", "create", "http",
				"--name", "Customer API",
				"--slug", "customer-api",
				"--base-url", "https://customer-api.example.test",
				"--health-path", "/health",
				"--timeout-seconds", "10",
				"--openapi-url", "https://customer-api.example.test/openapi.json",
				"--openapi-fetch-timeout-seconds", "12",
				"--allow-private-hosts",
				"--bearer-token", dataSourceTestSecretValue,
				"--read-only",
			},
			method:     http.MethodPost,
			target:     "/api/organizations/acme/data-sources",
			statusCode: http.StatusCreated,
			response:   dataSourcePayload,
			wantBody: `{
				"name":"Customer API",
				"slug":"customer-api",
				"type":"http",
				"config":{
					"base_url":"https://customer-api.example.test",
					"health_path":"/health",
					"timeout_seconds":10,
					"openapi_url":"https://customer-api.example.test/openapi.json",
					"openapi_fetch_timeout_seconds":12,
					"allow_private_hosts":true
				},
				"credentials":{"bearer_token":"secret-http-bearer-token"},
				"read_only":true
			}`,
			wantOutput: true,
		},
		{
			name: "HttpDataSourceUpdate",
			args: []string{
				"braidplane", "data-source", "update", "http", "customer-api",
				"--name", "Customer API Updated",
				"--base-url", "https://customer-api.example.test",
				"--health-path", "/ready",
				"--timeout-seconds", "15",
				"--read-write",
			},
			method:     http.MethodPatch,
			target:     "/api/organizations/acme/data-sources/customer-api",
			statusCode: http.StatusOK,
			response:   dataSourcePayload,
			wantBody: `{
				"name":"Customer API Updated",
				"config":{
					"base_url":"https://customer-api.example.test",
					"health_path":"/ready",
					"timeout_seconds":15
				},
				"read_only":false
			}`,
			wantOutput: true,
		},
		{
			name: "S3DataSourceUpdateExplicitFalseBooleans",
			args: []string{
				"braidplane", "data-source", "update", "s3", "customer-archive",
				"--endpoint", "https://s3.example.test",
				"--region", "us-east-1",
				"--bucket", "exports",
				"--force-path-style=false",
				"--allow-private-hosts=false",
			},
			method:     http.MethodPatch,
			target:     "/api/organizations/acme/data-sources/customer-archive",
			statusCode: http.StatusOK,
			response:   dataSourcePayload,
			wantBody: `{
				"config":{
					"endpoint":"https://s3.example.test",
					"region":"us-east-1",
					"bucket":"exports",
					"force_path_style":false,
					"allow_private_hosts":false
				}
			}`,
			wantOutput: true,
		},
		{
			name: "HttpDataSourceCredentialUpdate",
			args: []string{
				"braidplane", "data-source", "credentials", "update", "http", "customer-api",
				"--bearer-token", dataSourceTestSecretValue,
			},
			method:     http.MethodPatch,
			target:     "/api/organizations/acme/data-sources/customer-api/credentials",
			statusCode: http.StatusOK,
			response:   dataSourcePayload,
			wantBody:   `{"credentials":{"bearer_token":"secret-http-bearer-token"}}`,
			wantOutput: true,
		},
		{
			name: "HttpDataSourcePreSaveTest",
			args: []string{
				"braidplane", "data-source", "test", "http",
				"--base-url", "https://customer-api.example.test",
				"--health-path", "/health",
				"--timeout-seconds", "10",
				"--bearer-token", dataSourceTestSecretValue,
			},
			method:     http.MethodPost,
			target:     "/api/organizations/acme/data-sources/test-connection",
			statusCode: http.StatusNoContent,
			wantOutput: true,
			wantBody: `{
				"type":"http",
				"config":{
					"base_url":"https://customer-api.example.test",
					"health_path":"/health",
					"timeout_seconds":10
				},
				"credentials":{"bearer_token":"secret-http-bearer-token"}
			}`,
		},
		{
			name:       "DataSourceDelete",
			args:       []string{"braidplane", "data-source", "delete", "customer-api"},
			method:     http.MethodDelete,
			target:     "/api/organizations/acme/data-sources/customer-api",
			statusCode: http.StatusNoContent,
		},
		{
			name:       "DataSourceRestore",
			args:       []string{"braidplane", "data-source", "restore", "customer-api"},
			method:     http.MethodPost,
			target:     "/api/organizations/acme/data-sources/customer-api/restore",
			statusCode: http.StatusNoContent,
		},
		{
			name:       "DataSourceSchemaRefresh",
			args:       []string{"braidplane", "data-source", "schema", "refresh", "customer-api"},
			method:     http.MethodPost,
			target:     "/api/organizations/acme/data-sources/customer-api/schema",
			statusCode: http.StatusAccepted,
			response:   `{"data_source_slug":"customer-api","schema_status":"importing","enqueued":true,"job":{"kind":"data_source_schema_import","queue":"data_source_schema_import","state":"queued"}}`,
			wantOutput: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

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

			stdout, stderr, err := runCLIForDataSourceTest(t, server.URL, tt.args)
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
			if tt.wantOutput {
				require.NotEmpty(t, stdout)
			} else {
				require.Empty(t, stdout)
			}
			require.NotContains(t, stdout, dataSourceTestSecretValue)
			require.NotContains(t, stderr, dataSourceTestSecretValue)
		})
	}
}

func TestRunWithArgsDataSourceRequestFileCommandsUseExpectedRequests(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	requestDir := filepath.Join(tempDir, "requests")
	secretDir := filepath.Join(requestDir, "secrets")
	require.NoError(t, os.MkdirAll(secretDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(secretDir, "postgres-password"), []byte("secret-postgres-password"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(secretDir, "s3-access-key-id"), []byte(dataSourceTestAccessKeyIDValue), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(secretDir, "s3-secret-access-key"), []byte(dataSourceTestSecretAccessKeyValue), 0o600))

	absoluteAccessKeyPath := filepath.Join(tempDir, "absolute-access-key-id")
	require.NoError(t, os.WriteFile(absoluteAccessKeyPath, []byte("secret-absolute-access-key-id"), 0o600))

	createFile := filepath.Join(requestDir, "s3-create.json")
	require.NoError(t, os.WriteFile(createFile, []byte(`{
		"type":"s3",
		"name":"Customer Archive",
		"slug":"customer-archive",
		"read_only":true,
		"config":{
			"endpoint":"https://s3.example.test",
			"region":"us-east-1",
			"bucket":"exports",
			"force_path_style":true
		},
		"credentials":{
			"access_key_id_file":"secrets/s3-access-key-id",
			"secret_access_key_file":"secrets/s3-secret-access-key"
		}
	}`), 0o600))

	testFile := filepath.Join(requestDir, "postgres-test.json")
	require.NoError(t, os.WriteFile(testFile, []byte(`{
		"type":"postgres",
		"config":{
			"host":"postgres.example.test",
			"port":5432,
			"database":"orders",
			"ssl_mode":"disable"
		},
		"credentials":{
			"username":"app",
			"password_file":"secrets/postgres-password"
		}
	}`), 0o600))

	updateFile := filepath.Join(requestDir, "http-update.json")
	require.NoError(t, os.WriteFile(updateFile, []byte(`{
		"type":"http",
		"slug":"customer-api",
		"name":"Customer API Updated",
		"read_only":false,
		"config":{
			"base_url":"https://customer-api.example.test",
			"health_path":"/ready",
			"timeout_seconds":15
		}
	}`), 0o600))

	credentialsFile := filepath.Join(requestDir, "s3-credentials.json")
	require.NoError(t, os.WriteFile(credentialsFile, []byte(`{
		"type":"s3",
		"slug":"customer-archive",
		"credentials":{
			"access_key_id_file":`+jsonString(t, absoluteAccessKeyPath)+`,
			"secret_access_key_file":"secrets/s3-secret-access-key"
		}
	}`), 0o600))

	dataSourcePayload := `{
		"slug":"customer-archive",
		"name":"Customer Archive",
		"type":"s3",
		"config":{"region":"us-east-1","bucket":"exports"},
		"is_active":true,
		"read_only":true,
		"credential":{"kind":"static_secret","status":"active","updated_at":"2026-03-01T00:00:00Z"},
		"created_at":"2026-03-01T00:00:00Z",
		"updated_at":"2026-03-01T00:00:00Z"
	}`

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
			name:       "CreateFromRequestFile",
			args:       []string{"braidplane", "data-source", "create", "--file", createFile},
			method:     http.MethodPost,
			target:     "/api/organizations/acme/data-sources",
			statusCode: http.StatusCreated,
			response:   dataSourcePayload,
			wantBody: `{
				"name":"Customer Archive",
				"slug":"customer-archive",
				"type":"s3",
				"config":{
					"endpoint":"https://s3.example.test",
					"region":"us-east-1",
					"bucket":"exports",
					"force_path_style":true
				},
				"credentials":{
					"access_key_id":"secret-access-key-id",
					"secret_access_key":"secret-access-key"
				},
				"read_only":true
			}`,
			wantOutput: true,
		},
		{
			name:       "TestFromRequestFile",
			args:       []string{"braidplane", "data-source", "test", "--file", testFile},
			method:     http.MethodPost,
			target:     "/api/organizations/acme/data-sources/test-connection",
			statusCode: http.StatusNoContent,
			wantOutput: true,
			wantBody: `{
				"type":"postgres",
				"config":{
					"host":"postgres.example.test",
					"port":5432,
					"database":"orders",
					"ssl_mode":"disable"
				},
				"credentials":{
					"username":"app",
					"password":"secret-postgres-password"
				}
			}`,
		},
		{
			name:       "UpdateFromRequestFile",
			args:       []string{"braidplane", "data-source", "update", "--file", updateFile},
			method:     http.MethodPatch,
			target:     "/api/organizations/acme/data-sources/customer-api",
			statusCode: http.StatusOK,
			response:   dataSourcePayload,
			wantBody: `{
				"name":"Customer API Updated",
				"config":{
					"base_url":"https://customer-api.example.test",
					"health_path":"/ready",
					"timeout_seconds":15
				},
				"read_only":false
			}`,
			wantOutput: true,
		},
		{
			name:       "CredentialsUpdateFromRequestFile",
			args:       []string{"braidplane", "data-source", "credentials", "update", "--file", credentialsFile},
			method:     http.MethodPatch,
			target:     "/api/organizations/acme/data-sources/customer-archive/credentials",
			statusCode: http.StatusOK,
			response:   dataSourcePayload,
			wantBody: `{
				"credentials":{
					"access_key_id":"secret-absolute-access-key-id",
					"secret_access_key":"secret-access-key"
				}
			}`,
			wantOutput: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

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

			stdout, stderr, err := runCLIForDataSourceTest(t, server.URL, tt.args)
			require.NoError(t, err)
			require.Empty(t, stderr)
			require.Equal(t, tt.method, gotMethod)
			require.Equal(t, tt.target, gotTarget)
			require.Equal(t, "Bearer xto_test_token", gotAuth)
			require.JSONEq(t, tt.wantBody, string(gotBody))
			if tt.wantOutput {
				require.NotEmpty(t, stdout)
			} else {
				require.Empty(t, stdout)
			}
			require.NotContains(t, stdout, dataSourceTestAccessKeyIDValue)
			require.NotContains(t, stdout, dataSourceTestSecretAccessKeyValue)
			require.NotContains(t, stderr, dataSourceTestAccessKeyIDValue)
			require.NotContains(t, stderr, dataSourceTestSecretAccessKeyValue)
		})
	}
}

func TestRunWithArgsDataSourceReadCommandsUseExpectedRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		target   string
		response string
	}{
		{
			name: "DataSourceList",
			args: []string{
				"braidplane", "data-source", "list",
				"--limit", "10",
				"--cursor", "page-2",
				"--sort-by", "data_source_name",
				"--order", "asc",
				"--direction", "prev",
			},
			target: "/api/organizations/acme/data-sources?limit=10&cursor=page-2&sort_by=data_source_name&order=asc&direction=prev",
			response: `{
				"data_sources":[{
					"slug":"customer-api",
					"name":"Customer API",
					"type":"http",
					"config":{"base_url":"https://customer-api.example.test"},
					"is_active":true,
					"read_only":false,
					"created_at":"2026-03-01T00:00:00Z",
					"updated_at":"2026-03-01T00:00:00Z"
				}],
				"has_more":false
			}`,
		},
		{
			name:   "DataSourceGet",
			args:   []string{"braidplane", "data-source", "get", "customer-api"},
			target: "/api/organizations/acme/data-sources/customer-api",
			response: `{
				"slug":"customer-api",
				"name":"Customer API",
				"type":"http",
				"config":{"base_url":"https://customer-api.example.test"},
				"is_active":true,
				"read_only":false,
				"created_at":"2026-03-01T00:00:00Z",
				"updated_at":"2026-03-01T00:00:00Z"
			}`,
		},
		{
			name:   "DataSourceSchemaGet",
			args:   []string{"braidplane", "data-source", "schema", "get", "customer-api"},
			target: "/api/organizations/acme/data-sources/customer-api/schema",
			response: `{
				"schema_type":"http",
				"operations":[{"id":"listCustomers","path":"/customers","method":"GET"}]
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

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

			stdout, stderr, err := runCLIForDataSourceTest(t, server.URL, tt.args)
			require.NoError(t, err)
			require.NotEmpty(t, stdout)
			require.Empty(t, stderr)
			require.Equal(t, tt.target, gotTarget)
			require.Equal(t, "Bearer xto_test_token", gotAuth)
		})
	}
}

func TestRunWithArgsDataSourceCredentialErrorRedactsSecretValues(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{
			"error":{
				"code":"DATA_SOURCE_CONNECTION_FAILED",
				"message":"connection test failed",
				"details":"provider rejected bearer token secret-http-bearer-token"
			}
		}`))
	}))
	t.Cleanup(server.Close)

	stdout, stderr, err := runCLIForDataSourceTest(
		t,
		server.URL,
		[]string{
			"braidplane", "data-source", "credentials", "update", "http", "customer-api",
			"--bearer-token", dataSourceTestSecretValue,
		},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "422")
	require.ErrorContains(t, err, "DATA_SOURCE_CONNECTION_FAILED")
	require.ErrorContains(t, err, "connection test failed")
	require.Empty(t, stdout)
	require.Empty(t, stderr)
	require.NotContains(t, err.Error(), dataSourceTestSecretValue)
	require.NotContains(t, stdout, dataSourceTestSecretValue)
	require.NotContains(t, stderr, dataSourceTestSecretValue)
}

func TestRunWithArgsDataSourceRequestFileCredentialErrorRedactsSecretValues(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "token"), []byte(dataSourceTestSecretValue), 0o600))
	requestFile := filepath.Join(tempDir, "http-credentials.json")
	require.NoError(t, os.WriteFile(requestFile, []byte(`{
		"type":"http",
		"slug":"customer-api",
		"credentials":{"bearer_token_file":"token"}
	}`), 0o600))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{
			"error":{
				"code":"DATA_SOURCE_CONNECTION_FAILED",
				"message":"connection test failed",
				"details":"provider rejected bearer token secret-http-bearer-token"
			}
		}`))
	}))
	t.Cleanup(server.Close)

	stdout, stderr, err := runCLIForDataSourceTest(
		t,
		server.URL,
		[]string{"braidplane", "data-source", "credentials", "update", "--file", requestFile},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "422")
	require.ErrorContains(t, err, "DATA_SOURCE_CONNECTION_FAILED")
	require.Empty(t, stdout)
	require.Empty(t, stderr)
	require.NotContains(t, err.Error(), dataSourceTestSecretValue)
	require.NotContains(t, stdout, dataSourceTestSecretValue)
	require.NotContains(t, stderr, dataSourceTestSecretValue)
}

func TestDataSourceConfigFlagsTreatExplicitFalseBooleansAsPresent(t *testing.T) {
	t.Parallel()

	falseValue := false
	tests := []struct {
		name string
		typ  api.DataSourceType
		flag dataSourceRequestFlags
	}{
		{
			name: "HttpAllowPrivateHostsFalse",
			typ:  api.DataSourceTypeHttp,
			flag: dataSourceRequestFlags{AllowPrivateHosts: &falseValue},
		},
		{
			name: "FtpAllowPrivateHostsFalse",
			typ:  api.DataSourceTypeFtp,
			flag: dataSourceRequestFlags{AllowPrivateHosts: &falseValue},
		},
		{
			name: "GitAllowPrivateHostsFalse",
			typ:  api.DataSourceTypeGit,
			flag: dataSourceRequestFlags{AllowPrivateHosts: &falseValue},
		},
		{
			name: "S3ForcePathStyleFalse",
			typ:  api.DataSourceTypeS3,
			flag: dataSourceRequestFlags{ForcePathStyle: &falseValue},
		},
		{
			name: "S3AllowPrivateHostsFalse",
			typ:  api.DataSourceTypeS3,
			flag: dataSourceRequestFlags{AllowPrivateHosts: &falseValue},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.True(t, tt.flag.hasConfigFlags(tt.typ))
		})
	}
}

func TestRedactCredentialErrorPreservesXErrorKind(t *testing.T) {
	t.Parallel()

	err := redactCredentialError(
		xerror.InvalidInput(xerror.DataSource).Detail("provider rejected bearer token "+dataSourceTestSecretValue),
		[]string{dataSourceTestSecretValue},
	)

	var xe *xerror.Error
	require.True(t, xerror.As(err, &xe))
	require.Equal(t, xerror.KindInvalidInput, xe.Kind())
	require.Equal(t, "DATA_SOURCE_INVALID_INPUT", xe.Code())
	require.NotContains(t, err.Error(), dataSourceTestSecretValue)
	require.Contains(t, err.Error(), "[REDACTED]")
}

func TestReadSecretValuePreservesCredentialWhitespace(t *testing.T) {
	t.Parallel()

	value, secrets, err := readSecretValue("token", "  keep me\n", true)
	require.NoError(t, err)
	require.Equal(t, "  keep me\n", value)
	require.Equal(t, []string{"  keep me\n"}, secrets)

	secretDir := t.TempDir()
	secretPath := filepath.Join(secretDir, "secret.txt")
	require.NoError(t, os.WriteFile(secretPath, []byte("\tkeep file secret \n"), 0o600))
	value, secrets, err = readSecretFileValue("token", "", " secret.txt ", secretDir, true)
	require.NoError(t, err)
	require.Equal(t, "\tkeep file secret \n", value)
	require.Equal(t, []string{"\tkeep file secret \n"}, secrets)

	credentials, secrets, err := dataSourceRequestFlags{BearerToken: "  keep me\n"}.httpCredentials(false)
	require.NoError(t, err)
	require.Equal(t, []string{"  keep me\n"}, secrets)
	require.Equal(t, "  keep me\n", jsonObject(t, credentials)["bearer_token"])
}

func TestDataSourceRequestFilesValidateStrictJSONAndInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		run     func(path string) error
		wantErr string
	}{
		{
			name: "UnknownTopLevelField",
			body: `{
				"type":"http",
				"name":"Customer API",
				"config":{"base_url":"https://api.example.test"},
				"unexpected":true
			}`,
			run: func(path string) error {
				_, _, err := createDataSourceBodyFromFile(path)
				return err
			},
			wantErr: `unknown field "unexpected"`,
		},
		{
			name: "UnknownNestedConfigField",
			body: `{
				"type":"http",
				"name":"Customer API",
				"config":{
					"base_url":"https://api.example.test",
					"base_uri":"https://api.example.test"
				}
			}`,
			run: func(path string) error {
				_, _, err := createDataSourceBodyFromFile(path)
				return err
			},
			wantErr: `unknown field "base_uri"`,
		},
		{
			name: "DoesNotExpandEnvironmentVariablesInSecretPaths",
			body: `{
				"type":"http",
				"name":"Customer API",
				"config":{"base_url":"https://api.example.test"},
				"credentials":{"bearer_token_file":"$TOKEN_FILE"}
			}`,
			run: func(path string) error {
				_, _, err := createDataSourceBodyFromFile(path)
				return err
			},
			wantErr: "$TOKEN_FILE",
		},
		{
			name: "RequiresSlugForUpdateFile",
			body: `{
				"type":"http",
				"config":{"base_url":"https://api.example.test"}
			}`,
			run: func(path string) error {
				_, _, err := updateDataSourceBodyFromFile(path)
				return err
			},
			wantErr: "set slug in --file",
		},
		{
			name: "RejectsCredentialsInUpdateFile",
			body: `{
				"type":"http",
				"slug":"customer-api",
				"config":{"base_url":"https://api.example.test"},
				"credentials":{"bearer_token":"secret-token"}
			}`,
			run: func(path string) error {
				_, _, err := updateDataSourceBodyFromFile(path)
				return err
			},
			wantErr: "data-source update files cannot include credentials",
		},
		{
			name: "RejectsConfigInCredentialsUpdateFile",
			body: `{
				"type":"http",
				"slug":"customer-api",
				"config":{"base_url":"https://api.example.test"},
				"credentials":{"bearer_token":"secret-token"}
			}`,
			run: func(path string) error {
				_, _, _, err := dataSourceCredentialsBodyFromFile(path, true)
				return err
			},
			wantErr: "credentials update files can include only type, slug, and credentials",
		},
		{
			name: "RejectsInvalidGitAuthType",
			body: `{
				"type":"git",
				"name":"Pipeline Repo",
				"config":{"repo_url":"https://git.example.test/acme/repo.git"},
				"credentials":{"auth_type":" htps ","token":"secret-token"}
			}`,
			run: func(path string) error {
				_, _, err := createDataSourceBodyFromFile(path)
				return err
			},
			wantErr: "git auth_type must be https or ssh",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "request.json")
			require.NoError(t, os.WriteFile(path, []byte(tt.body), 0o600))
			err := tt.run(path)
			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestDataSourceRequestFileNormalizesGitAuthType(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "git.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"type":"git",
		"name":"Pipeline Repo",
		"config":{"repo_url":"https://git.example.test/acme/repo.git"},
		"credentials":{"auth_type":" https ","token":"secret-token"}
	}`), 0o600))

	body, secrets, err := createDataSourceBodyFromFile(path)

	require.NoError(t, err)
	require.Equal(t, []string{"secret-token"}, secrets)
	object := jsonObject(t, body)
	credentials, ok := object["credentials"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "https", credentials["auth_type"])
}

func TestRunWithArgsDataSourceRequestFileRejectsMixedInput(t *testing.T) {
	t.Parallel()

	requestFile := filepath.Join(t.TempDir(), "request.json")
	require.NoError(t, os.WriteFile(requestFile, []byte(`{
		"type":"http",
		"name":"Customer API",
		"config":{"base_url":"https://api.example.test"}
	}`), 0o600))

	stdout, stderr, err := runCLIForDataSourceTest(
		t,
		"https://api.example.test",
		[]string{"braidplane", "data-source", "create", "http", "--file", requestFile},
	)

	require.Error(t, err)
	require.Empty(t, stdout)
	require.Empty(t, stderr)
	require.ErrorContains(t, err, "do not combine --file with data-source args or flags")
}

func TestDataSourceProviderBuildersUseGeneratedShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		typ  string
		flag dataSourceRequestFlags
	}{
		{
			name: "Postgres",
			typ:  "postgres",
			flag: dataSourceRequestFlags{
				Host: "postgres.example.test", Port: 5432, Database: "app", SSLMode: "require",
				ConnectTimeout: 5, Username: "app", Password: dataSourceTestPasswordValue,
			},
		},
		{
			name: "MySQL",
			typ:  "mysql",
			flag: dataSourceRequestFlags{
				Host: "mysql.example.test", Port: 3306, Database: "app", ConnectTimeout: 5,
				Username: "app", Password: dataSourceTestPasswordValue,
			},
		},
		{
			name: "PasswordlessMySQL",
			typ:  "mysql",
			flag: dataSourceRequestFlags{
				Host: "mysql.example.test", Port: 3306, Database: "app", ConnectTimeout: 5,
				Username: "reader",
			},
		},
		{
			name: "BigQuery",
			typ:  "bigquery",
			flag: dataSourceRequestFlags{
				ProjectID: "analytics-prod", DefaultDataset: "warehouse", Location: "US",
				MaximumBytesBilled: 1000, TimeoutSeconds: 30,
				ServiceAccountJSON: dataSourceTestServiceAccountJSONValue,
			},
		},
		{
			name: "Snowflake",
			typ:  "snowflake",
			flag: dataSourceRequestFlags{
				Account: "acme.eu-central-1", Warehouse: "BRAIDPLANE_WH", Database: "ANALYTICS",
				Schema: "PUBLIC", Role: "BRAIDPLANE_READER", ConnectTimeout: 15,
				SnowflakeAuthType: "password", Username: "svc", Password: dataSourceTestPasswordValue,
			},
		},
		{
			name: "HTTP",
			typ:  "http",
			flag: dataSourceRequestFlags{
				BaseURL: "https://api.example.test", HealthPath: "/health", TimeoutSeconds: 10,
				OpenAPIURL: "https://api.example.test/openapi.json", OpenAPIFetchTimeoutSeconds: 12,
				AllowPrivateHosts: boolPointer(true), BearerToken: dataSourceTestSecretValue,
			},
		},
		{
			name: "HubSpot",
			typ:  "hubspot",
			flag: dataSourceRequestFlags{
				TimeoutSeconds: 15, PrivateAppToken: dataSourceTestPrivateAppTokenValue,
			},
		},
		{
			name: "MongoDB",
			typ:  "mongodb",
			flag: dataSourceRequestFlags{
				Host: "mongo.example.test", Port: 27017, Database: "app", AuthDatabase: "admin",
				ConnectTimeout: 5, Username: "app", Password: dataSourceTestPasswordValue,
			},
		},
		{
			name: "FTP",
			typ:  "ftp",
			flag: dataSourceRequestFlags{
				Host: "ftp.example.test", Port: 21, BasePath: "/exports", ConnectTimeout: 5,
				AllowPrivateHosts: boolPointer(true), Username: "app", Password: dataSourceTestPasswordValue,
			},
		},
		{
			name: "Git",
			typ:  "git",
			flag: dataSourceRequestFlags{
				RepoURL: "https://git.example.test/acme/repo.git", DefaultBranch: "main", BasePath: "pipelines",
				GitAuthType: "https", Username: "app", GitToken: dataSourceTestGitTokenValue,
			},
		},
		{
			name: "GitHub",
			typ:  "github",
			flag: dataSourceRequestFlags{
				RepositoryID: "42", BasePath: "pipelines",
			},
		},
		{
			name: "GitLab",
			typ:  "gitlab",
			flag: dataSourceRequestFlags{
				ProjectID: "42", BasePath: "pipelines",
			},
		},
		{
			name: "S3",
			typ:  "s3",
			flag: dataSourceRequestFlags{
				Endpoint: "https://s3.example.test", Region: "us-east-1", Bucket: "exports", Prefix: "daily",
				ForcePathStyle: boolPointer(true), TimeoutSeconds: 10, AllowPrivateHosts: boolPointer(true),
				AccessKeyID: dataSourceTestAccessKeyIDValue, SecretAccessKey: dataSourceTestSecretAccessKeyValue,
				SessionToken: dataSourceTestSessionTokenValue,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			createCommand := createDataSourceCommand{
				Type:                   tt.typ,
				Name:                   tt.name,
				Slug:                   "slug-" + tt.typ,
				dataSourceRequestFlags: tt.flag,
			}
			createCommand.ReadOnly = true
			createBody, createSecrets, err := createCommand.createBody()
			require.NoError(t, err)
			if tt.typ == "github" || tt.typ == "gitlab" || tt.name == "PasswordlessMySQL" {
				require.Empty(t, createSecrets)
			} else {
				require.NotEmpty(t, createSecrets)
			}
			createJSON := jsonObject(t, createBody)
			require.Equal(t, tt.typ, createJSON["type"])
			require.Equal(t, tt.name, createJSON["name"])
			require.NotEmpty(t, createJSON["config"])
			if tt.typ == "github" || tt.typ == "gitlab" {
				require.NotContains(t, createJSON, "credentials")
				if tt.typ == "github" {
					require.Equal(t, "42", createJSON["config"].(map[string]any)["repository_id"])
				}
			} else {
				require.NotEmpty(t, createJSON["credentials"])
			}
			if tt.name == "PasswordlessMySQL" {
				require.Equal(t, map[string]any{"username": "reader", "password": ""}, createJSON["credentials"])
			}
			require.Equal(t, true, createJSON["read_only"])
			if tt.typ == "gitlab" {
				require.Equal(t, "42", createJSON["config"].(map[string]any)["project_id"])
				require.NotContains(t, createJSON["config"], "repo_url")
			}
			if tt.typ == "github" || tt.typ == "gitlab" {
				testCommand := testDataSourceCommand{Type: tt.typ, dataSourceRequestFlags: tt.flag}
				testBody, testSecrets, err := testCommand.testBody()
				require.NoError(t, err)
				require.Empty(t, testSecrets)
				testJSON := jsonObject(t, testBody)
				key := "repository_id"
				if tt.typ == "gitlab" {
					key = "project_id"
				}
				require.Equal(t, "42", testJSON["config"].(map[string]any)[key])
				require.NotContains(t, testJSON, "credentials")

				updateCommand := updateDataSourceCommand{Type: tt.typ, DataSourceSlug: "slug-github", dataSourceRequestFlags: tt.flag}
				_, _, err = updateCommand.updateBody()
				require.ErrorContains(t, err, "repository selection")

				credentialsCommand := updateDataSourceCredentialsCommand{Type: tt.typ, DataSourceSlug: "slug-github", dataSourceRequestFlags: tt.flag}
				_, _, _, err = credentialsCommand.credentialsBody(true)
				require.ErrorContains(t, err, "organization connection")
				return
			}

			updateCommand := updateDataSourceCommand{
				Type:                   tt.typ,
				DataSourceSlug:         "slug-" + tt.typ,
				Name:                   tt.name + " Updated",
				dataSourceRequestFlags: tt.flag,
			}
			updateCommand.ReadWrite = true
			updateBody, updateSlug, err := updateCommand.updateBody()
			require.NoError(t, err)
			require.Equal(t, "slug-"+tt.typ, updateSlug)
			updateJSON := jsonObject(t, updateBody)
			require.Equal(t, tt.name+" Updated", updateJSON["name"])
			require.NotEmpty(t, updateJSON["config"])
			require.Equal(t, false, updateJSON["read_only"])

			testCommand := testDataSourceCommand{Type: tt.typ, dataSourceRequestFlags: tt.flag}
			testBody, testSecrets, err := testCommand.testBody()
			require.NoError(t, err)
			if tt.name == "PasswordlessMySQL" {
				require.Empty(t, testSecrets)
			} else {
				require.NotEmpty(t, testSecrets)
			}
			testJSON := jsonObject(t, testBody)
			require.Equal(t, tt.typ, testJSON["type"])
			require.NotEmpty(t, testJSON["config"])
			require.NotEmpty(t, testJSON["credentials"])

			credentialsCommand := updateDataSourceCredentialsCommand{
				Type:                   tt.typ,
				DataSourceSlug:         "slug-" + tt.typ,
				dataSourceRequestFlags: tt.flag,
			}
			credentialsBody, credentialSecrets, credentialsSlug, err := credentialsCommand.credentialsBody(true)
			require.NoError(t, err)
			require.Equal(t, "slug-"+tt.typ, credentialsSlug)
			if tt.name == "PasswordlessMySQL" {
				require.Empty(t, credentialSecrets)
				require.Equal(t, map[string]any{"username": "reader", "password": ""}, jsonObject(t, credentialsBody))
			} else {
				require.NotEmpty(t, credentialSecrets)
			}
			require.NotEmpty(t, jsonObject(t, credentialsBody))
		})
	}
}

func TestSnowflakeWorkloadIdentityCLIBuilderOmitsStaticSecrets(t *testing.T) {
	t.Parallel()

	flags := dataSourceRequestFlags{
		Account: "acme.eu-central-1", Warehouse: "BRAIDPLANE_WH", Database: "ANALYTICS",
		Schema: "PUBLIC", SnowflakeAuthType: "workload_identity",
	}
	command := createDataSourceCommand{
		Type: "snowflake", Name: "Snowflake", dataSourceRequestFlags: flags,
	}

	body, secrets, err := command.createBody()
	require.NoError(t, err)
	require.Empty(t, secrets)
	object := jsonObject(t, body)
	credentials, ok := object["credentials"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "workload_identity", credentials["auth_type"])
	require.NotContains(t, credentials, "username")
	require.NotContains(t, credentials, "password")
}

func TestSnowflakeConfigValidatesConnectTimeout(t *testing.T) {
	t.Parallel()

	for _, timeout := range []int{-1, 301} {
		t.Run(fmt.Sprintf("Rejects%d", timeout), func(t *testing.T) {
			t.Parallel()

			_, err := dataSourceRequestFlags{ConnectTimeout: timeout}.snowflakeConfig(false)

			require.ErrorIs(t, err, xerror.InvalidInput(xerror.DataSource))
			require.ErrorContains(t, err, "connect-timeout must be between 1 and 300 seconds")
		})
	}

	for _, timeout := range []int{1, 300} {
		t.Run(fmt.Sprintf("Accepts%d", timeout), func(t *testing.T) {
			t.Parallel()

			config, err := dataSourceRequestFlags{ConnectTimeout: timeout}.snowflakeConfig(false)

			require.NoError(t, err)
			require.NotNil(t, config.ConnectTimeout)
			require.Equal(t, timeout, *config.ConnectTimeout)
		})
	}

	config, err := dataSourceRequestFlags{}.snowflakeConfig(false)
	require.NoError(t, err)
	require.Nil(t, config.ConnectTimeout)
}

func TestDataSourceProviderBuildersValidateRepresentativeErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		run     func() error
		wantErr string
	}{
		{
			name: "UnsupportedType",
			run: func() error {
				_, err := parseDataSourceType("oracle")
				return err
			},
			wantErr: "unsupported data source type",
		},
		{
			name: "ReadModeConflict",
			run: func() error {
				command := createDataSourceCommand{
					Type: "http", Name: "HTTP", dataSourceRequestFlags: dataSourceRequestFlags{ReadOnly: true, ReadWrite: true},
				}
				_, _, err := command.createBody()
				return err
			},
			wantErr: "only one of --read-only or --read-write",
		},
		{
			name: "MissingRequiredConfig",
			run: func() error {
				command := createDataSourceCommand{Type: "http", Name: "HTTP"}
				_, _, err := command.createBody()
				return err
			},
			wantErr: "set --base-url",
		},
		{
			name: "SnowflakeMissingRequiredConfig",
			run: func() error {
				_, err := dataSourceRequestFlags{
					Account: "acme", Warehouse: "WH", Database: "DB",
				}.snowflakeConfig(true)
				return err
			},
			wantErr: "set --schema",
		},
		{
			name: "SnowflakeWorkloadIdentityRejectsUsername",
			run: func() error {
				_, _, err := dataSourceRequestFlags{
					SnowflakeAuthType: "workload_identity", Username: "svc",
				}.snowflakeCredentials(true)
				return err
			},
			wantErr: "do not set Snowflake username or password",
		},
		{
			name: "SnowflakeWorkloadIdentityRejectsPassword",
			run: func() error {
				_, _, err := dataSourceRequestFlags{
					SnowflakeAuthType: "workload_identity", Password: dataSourceTestPasswordValue,
				}.snowflakeCredentials(true)
				return err
			},
			wantErr: "do not set Snowflake username or password",
		},
		{
			name: "SnowflakeRejectsUnsupportedAuthType",
			run: func() error {
				_, _, err := dataSourceRequestFlags{
					SnowflakeAuthType: "oauth2",
				}.snowflakeCredentials(true)
				return err
			},
			wantErr: "snowflake-auth-type must be password or workload_identity",
		},
		{
			name: "SnowflakePasswordRequiresUsername",
			run: func() error {
				_, _, err := dataSourceRequestFlags{
					SnowflakeAuthType: "password", Password: dataSourceTestPasswordValue,
				}.snowflakeCredentials(true)
				return err
			},
			wantErr: "set --username",
		},
		{
			name: "ConflictingSecretSources",
			run: func() error {
				_, _, err := readSecretFileValue("bearer token", dataSourceTestSecretValue, "token.txt", t.TempDir(), false)
				return err
			},
			wantErr: "set only one bearer token source",
		},
		{
			name: "MissingRequiredCredentials",
			run: func() error {
				_, _, err := dataSourceRequestFlags{BaseURL: "https://api.example.test"}.httpCredentials(true)
				return err
			},
			wantErr: "set --bearer-token",
		},
		{
			name: "EmptyUpdate",
			run: func() error {
				command := updateDataSourceCommand{Type: "http", DataSourceSlug: "customer-api"}
				_, _, err := command.updateBody()
				return err
			},
			wantErr: "set --name",
		},
		{
			name: "InvalidListSort",
			run: func() error {
				_, err := listDataSourcesParams(0, "", "slug", "", "")
				return err
			},
			wantErr: "sort-by must be",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.run()
			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestRunWithArgsDataSourceHelpListsConcreteGroup(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithArgs([]string{"braidplane", "data-source", "--help"}, &stdout, &stderr)

	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Manage concrete organization data sources")
	require.Contains(t, stdout.String(), "credentials")
	require.Contains(t, stdout.String(), "schema")
	require.Empty(t, stderr.String())
}

func TestRunWithArgsDataSourceCreateHelpShowsRequestFile(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithArgs([]string{"braidplane", "data-source", "create", "--help"}, &stdout, &stderr)

	require.NoError(t, err)
	require.Contains(t, stdout.String(), "--file")
	require.Contains(t, strings.Join(strings.Fields(stdout.String()), " "), "omit positional arguments")
	require.Empty(t, stderr.String())
}

func TestDraftDataSourceTestReportsCompletion(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "github.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"type":"github","config":{"repository_id":"123456"}}`), 0o600))
	for _, format := range []string{"json", "text"} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/organizations/acme/data-sources/test-connection", r.URL.Path)
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(server.Close)
			stdout, stderr, err := runCLIForDataSourceTest(t, server.URL, []string{
				"braidplane", "data-source", "test", "--file", path, "--output", format,
			})
			require.NoError(t, err)
			require.Empty(t, stderr)
			if format == "json" {
				require.JSONEq(t, `{"type":"github","status":"succeeded"}`, stdout)
			} else {
				require.Equal(t, "Draft github connection test succeeded.\n", stdout)
			}
		})
	}
}

func TestDraftDataSourceTestFailurePrintsNoSuccess(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"CONNECTION_TEST_FAILED","message":"test-secret refused"}}`))
	}))
	t.Cleanup(server.Close)
	stdout, _, err := runCLIForDataSourceTest(t, server.URL, []string{
		"braidplane", "data-source", "test", "http", "--base-url", "https://example.test", "--bearer-token", "test-secret",
	})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "test-secret")
	require.Empty(t, stdout)
}

func runCLIForDataSourceTest(t *testing.T, serverURL string, args []string) (string, string, error) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	config := []byte("server_url = \"" + serverURL + "/api\"\norganization = \"acme\"\napi_key = \"xto_test_token\"\n")
	require.NoError(t, os.WriteFile(configPath, config, 0o600))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		args,
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)
	return stdout.String(), stderr.String(), err
}

func jsonObject(t *testing.T, value any) map[string]any {
	t.Helper()

	data, err := json.Marshal(value)
	require.NoError(t, err)

	var object map[string]any
	require.NoError(t, json.Unmarshal(data, &object))

	return object
}

func boolPointer(value bool) *bool {
	return &value
}

func jsonString(t *testing.T, value string) string {
	t.Helper()

	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data)
}

func TestSnowflakeOnboardingCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/organizations/acme/data-sources/warehouse/snowflake-onboarding", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"identity_state":"pending","active":false,"issuer":"https://issuer.test","subject":"public-subject","audience":"public-audience","sql":"reviewable SQL","diagnostic":"Provisioning"}`))
	}))
	defer server.Close()
	stdout, stderr, err := runCLIForDataSourceTest(t, server.URL, []string{"braidplane", "data-source", "snowflake-onboarding", "warehouse"})
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "reviewable SQL")
	require.Contains(t, stdout, "pending")
}

func TestSnowflakeRequestFilePreservesObjectGrantsAndDisable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"type":"snowflake","slug":"source","name":"Source","read_only":true,"config":{"account":"acme-account","warehouse":"WH","database":"DB","schema":"PUBLIC","role":"DEDICATED","wif_objects":[{"name":"Orders.with dots","privilege":"SELECT"},{"name":"EXPORT","privilege":"INSERT"}]},"credentials":{"auth_type":"workload_identity"}}`), 0o600))
	body, _, err := (&createDataSourceCommand{dataSourceRequestFlags: dataSourceRequestFlags{RequestFile: path}}).createBody()
	require.NoError(t, err)
	object := jsonObject(t, body)
	config := object["config"].(map[string]any)
	require.Len(t, config["wif_objects"], 2)
	require.Contains(t, fmt.Sprint(config["wif_objects"]), "Orders.with dots")
	require.NoError(t, os.WriteFile(path, []byte(`{"type":"snowflake","slug":"source","is_active":false}`), 0o600))
	update, ds, err := (&updateDataSourceCommand{dataSourceRequestFlags: dataSourceRequestFlags{RequestFile: path}}).updateBody()
	require.NoError(t, err)
	require.Equal(t, "source", ds)
	require.NotNil(t, update.IsActive)
	require.False(t, *update.IsActive)
}

func TestManagedGitRequestFilesUseOrganizationCredentials(t *testing.T) {
	t.Parallel()
	for _, provider := range []struct{ name, key string }{{"github", "repository_id"}, {"gitlab", "project_id"}} {
		t.Run(provider.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "source.json")
			body := fmt.Sprintf(`{"type":%q,"name":"Managed repository","config":{%q:"42","base_path":"pipelines"}}`, provider.name, provider.key)
			require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
			created, secrets, err := createDataSourceBodyFromFile(path)
			require.NoError(t, err)
			require.Empty(t, secrets)
			require.NotContains(t, jsonObject(t, created), "credentials")
			tested, secrets, err := testDataSourceBodyFromFile(path)
			require.NoError(t, err)
			require.Empty(t, secrets)
			require.NotContains(t, jsonObject(t, tested), "credentials")
			body = fmt.Sprintf(`{"type":%q,"name":"Managed repository","config":{%q:"42"},"credentials":{"token_file":"missing-secret"}}`, provider.name, provider.key)
			require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
			_, _, err = createDataSourceBodyFromFile(path)
			require.ErrorContains(t, err, "Generic Git")
			flags := dataSourceRequestFlags{ProjectID: "42", RepositoryID: "42", GitToken: "fake-token"}
			_, _, err = (&createDataSourceCommand{Type: provider.name, Name: "Managed repository", dataSourceRequestFlags: flags}).createBody()
			require.ErrorContains(t, err, "Generic Git")
		})
	}
}

func TestWorkspaceVerifyPreservesAPIErrorsAndRejectsMissingPayload(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		loadStatus, verifyStatus int
		want                     string
	}{
		{"source denied", http.StatusForbidden, http.StatusOK, "403"},
		{"verification denied", http.StatusOK, http.StatusForbidden, "403"},
		{"missing verification payload", http.StatusOK, http.StatusOK, "missing Google Workspace verification payload"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verifications := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					w.WriteHeader(tc.loadStatus)
					if tc.loadStatus == http.StatusOK {
						_, _ = io.WriteString(w, `{"connection_revision":7}`)
					} else {
						_, _ = io.WriteString(w, `{"error":{"code":"FORBIDDEN","message":"access denied"}}`)
					}
					return
				}
				verifications++
				if tc.verifyStatus == http.StatusOK {
					w.Header().Set("Content-Type", "text/plain")
				}
				w.WriteHeader(tc.verifyStatus)
				if tc.verifyStatus != http.StatusOK {
					_, _ = io.WriteString(w, `{"error":{"code":"FORBIDDEN","message":"access denied"}}`)
				}
			}))
			defer server.Close()
			stdout, _, err := runCLIForDataSourceTest(t, server.URL, []string{"braidplane", "data-source", "provider", "verify", "sheet", "--file-id", "book"})
			require.ErrorContains(t, err, tc.want)
			require.Empty(t, stdout)
			if tc.loadStatus != http.StatusOK {
				require.Zero(t, verifications)
			} else {
				require.Equal(t, 1, verifications)
			}
		})
	}
}

func TestHTTPOAuthRequestFileLoadsSecretAndPreservesApp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "secret"), []byte("customer-secret"), 0o600))
	path := filepath.Join(dir, "http.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"type":"http","name":"HTTP app","slug":"http-app","config":{"base_url":"https://api.example/v1"},"credentials":{"oauth":{"issuer":"https://issuer.example","token_endpoint":"https://issuer.example/token","client_id":"customer-app","client_secret_file":"secret","grant_type":"client_credentials","scopes":["read"],"audience":"api"}}}`), 0o600))
	body, secrets, err := createDataSourceBodyFromFile(path)
	require.NoError(t, err)
	require.Contains(t, secrets, "customer-secret")
	object := jsonObject(t, body)
	creds := object["credentials"].(map[string]any)
	oauth := creds["oauth"].(map[string]any)
	require.Equal(t, "customer-secret", oauth["client_secret"])
	require.Equal(t, "api", oauth["audience"])
	require.NotContains(t, oauth, "client_secret_file")
	_, secrets, err = testDataSourceBodyFromFile(path)
	require.NoError(t, err)
	require.Contains(t, secrets, "customer-secret")
	delete(object, "name")
	delete(object, "config")
	payload, err := json.Marshal(object)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, payload, 0o600))
	_, secrets, _, err = dataSourceCredentialsBodyFromFile(path, true)
	require.NoError(t, err)
	require.Contains(t, secrets, "customer-secret")
}

func TestHTTPOAuthRequestFileRejectsMixedCredentials(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "http.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"type":"http","name":"HTTP app","config":{"base_url":"https://api.example"},"credentials":{"bearer_token_file":"do-not-read","oauth":{"issuer":"https://issuer.example","token_endpoint":"https://issuer.example/token","client_id":"app","grant_type":"authorization_code"}}}`), 0o600))
	_, _, err := createDataSourceBodyFromFile(path)
	require.ErrorContains(t, err, "cannot be combined")
}

func TestPostgresRequestFilePreservesPrivateHostOptIn(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		flags := dataSourceRequestFlags{}
		raw := json.RawMessage(fmt.Sprintf(`{"host":"localhost","port":5432,"database":"demo","allow_private_hosts":%t}`, enabled))
		require.NoError(t, decodeDataSourceConfigFile(api.DataSourceTypePostgres, raw, true, &flags))
		config, err := flags.postgresConfig(true)
		require.NoError(t, err)
		require.NotNil(t, config.AllowPrivateHosts)
		require.Equal(t, enabled, *config.AllowPrivateHosts)
		require.True(t, (dataSourceRequestFlags{AllowPrivateHosts: &enabled}).hasConfigFlags(api.DataSourceTypePostgres))
	}
}
