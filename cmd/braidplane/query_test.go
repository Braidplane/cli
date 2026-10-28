package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CLI auth resolution reads process environment variables.
func TestRunWithArgsQueryMutationCommandsUseExpectedRequests(t *testing.T) {
	statementJSON := `{"type":"postgres","sql":"SELECT id, email FROM users WHERE {{is_active}} IN (true, false) ORDER BY id LIMIT {{limit}}"}`
	parametersJSON := `[
		{"name":"is_active","data_type":"boolean","is_required":true,"default_value":"true"},
		{"name":"limit","data_type":"integer","is_required":false,"default_value":"100"}
	]`
	queryPayload := `{
		"slug":"list-active-users",
		"data_source_slug":"acme-postgres",
		"name":"List active users",
		"description":"Active users for smoke verification",
		"query_statement":{"type":"postgres","sql":"SELECT id, email FROM users WHERE {{is_active}} IN (true, false) ORDER BY id LIMIT {{limit}}"},
		"parameters":[
			{"name":"is_active","data_type":"boolean","is_required":true,"default_value":"true"},
			{"name":"limit","data_type":"integer","is_required":false,"default_value":"100"}
		],
		"row_limit":100,
		"timeout_seconds":30,
		"created_at":"2026-06-14T12:00:00Z",
		"updated_at":"2026-06-14T12:00:00Z"
	}`
	queryExecutionResultPayload := `{
		"result_type":"sql",
		"type":"postgres",
		"payload":{
			"rows":[{"id":1,"email":"alice@example.com"}],
			"row_count":1,
			"column_names":["id","email"]
		},
		"duration_ms":12,
		"success":true
	}`

	dir := t.TempDir()
	statementPath := filepath.Join(dir, "statement.json")
	parametersPath := filepath.Join(dir, "parameters.json")
	require.NoError(t, os.WriteFile(statementPath, []byte(statementJSON), 0o600))
	require.NoError(t, os.WriteFile(parametersPath, []byte(parametersJSON), 0o600))

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
			name: "QueryCreateFromFiles",
			args: []string{
				"braidplane", "query", "create",
				"--data-source", "acme-postgres",
				"--name", "List active users",
				"--slug", "list-active-users",
				"--description", "Active users for smoke verification",
				"--statement-file", statementPath,
				"--parameters-file", parametersPath,
				"--row-limit", "100",
				"--timeout-seconds", "30",
			},
			method:     http.MethodPost,
			target:     "/api/organizations/acme/data-sources/acme-postgres/queries",
			statusCode: http.StatusCreated,
			response:   queryPayload,
			wantBody: `{
				"name":"List active users",
				"slug":"list-active-users",
				"description":"Active users for smoke verification",
				"query_statement":{"type":"postgres","sql":"SELECT id, email FROM users WHERE {{is_active}} IN (true, false) ORDER BY id LIMIT {{limit}}"},
				"parameters":[
					{"name":"is_active","data_type":"boolean","is_required":true,"default_value":"true"},
					{"name":"limit","data_type":"integer","is_required":false,"default_value":"100"}
				],
				"row_limit":100,
				"timeout_seconds":30
			}`,
		},
		{
			name: "QueryUpdateFromInlineJSON",
			args: []string{
				"braidplane", "query", "update", "list-active-users",
				"--data-source", "acme-postgres",
				"--name", "List active users",
				"--description", "Active users for smoke verification",
				"--statement-json", statementJSON,
				"--parameters-json", parametersJSON,
				"--row-limit", "100",
				"--timeout-seconds", "30",
			},
			method:     http.MethodPut,
			target:     "/api/organizations/acme/data-sources/acme-postgres/queries/list-active-users",
			statusCode: http.StatusOK,
			response:   queryPayload,
			wantBody: `{
				"name":"List active users",
				"description":"Active users for smoke verification",
				"query_statement":{"type":"postgres","sql":"SELECT id, email FROM users WHERE {{is_active}} IN (true, false) ORDER BY id LIMIT {{limit}}"},
				"parameters":[
					{"name":"is_active","data_type":"boolean","is_required":true,"default_value":"true"},
					{"name":"limit","data_type":"integer","is_required":false,"default_value":"100"}
				],
				"row_limit":100,
				"timeout_seconds":30
			}`,
		},
		{
			name: "QueryExecute",
			args: []string{
				"braidplane", "query", "execute", "list-active-users",
				"--data-source", "acme-postgres",
				"--parameters-json", `{"is_active":true,"limit":2}`,
			},
			method:     http.MethodPost,
			target:     "/api/organizations/acme/data-sources/acme-postgres/queries/list-active-users/execute",
			statusCode: http.StatusOK,
			response:   queryExecutionResultPayload,
			wantBody:   `{"parameters":{"is_active":true,"limit":2}}`,
		},
		{
			name:       "QueryDelete",
			args:       []string{"braidplane", "query", "delete", "list-active-users", "--data-source", "acme-postgres"},
			method:     http.MethodDelete,
			target:     "/api/organizations/acme/data-sources/acme-postgres/queries/list-active-users",
			statusCode: http.StatusNoContent,
		},
		{
			name:       "QueryRestore",
			args:       []string{"braidplane", "query", "restore", "list-active-users", "--data-source", "acme-postgres"},
			method:     http.MethodPost,
			target:     "/api/organizations/acme/data-sources/acme-postgres/queries/list-active-users/restore",
			statusCode: http.StatusNoContent,
		},
		{
			name:       "QueryPurge",
			args:       []string{"braidplane", "query", "purge", "list-active-users", "--data-source", "acme-postgres"},
			method:     http.MethodDelete,
			target:     "/api/organizations/acme/data-sources/acme-postgres/queries/list-active-users/purge",
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

			stdout, stderr, err := runCLIForQueryTest(t, server.URL, tt.args)
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
func TestRunWithArgsQueryReadCommandsUseExpectedRequests(t *testing.T) {
	queryPayload := `{
		"slug":"list-active-users",
		"data_source_slug":"acme-postgres",
		"name":"List active users",
		"description":null,
		"query_statement":{"type":"postgres","sql":"SELECT id FROM users"},
		"parameters":[],
		"row_limit":100,
		"timeout_seconds":30,
		"created_at":"2026-06-14T12:00:00Z",
		"updated_at":"2026-06-14T12:00:00Z"
	}`
	executionPayload := `{
		"slug":"exec-1",
		"status":"success",
		"executed_at":"2026-06-14T12:01:00Z",
		"result":{
			"result_type":"sql",
			"type":"postgres",
			"payload":{"rows":[{"id":1}],"row_count":1,"column_names":["id"]},
			"duration_ms":12,
			"success":true
		}
	}`

	tests := []struct {
		name     string
		args     []string
		target   string
		response string
	}{
		{
			name:     "QueryList",
			args:     []string{"braidplane", "query", "list", "--data-source", "acme-postgres", "--limit", "10", "--cursor", "page-2", "--sort-by", "query_name", "--order", "asc", "--direction", "next", "--include-deleted"},
			target:   "/api/organizations/acme/data-sources/acme-postgres/queries?limit=10&cursor=page-2&sort_by=query_name&order=asc&direction=next&include_deleted=true",
			response: `{"queries":[` + queryPayload + `],"has_more":false}`,
		},
		{
			name:     "QueryGet",
			args:     []string{"braidplane", "query", "get", "list-active-users", "--data-source", "acme-postgres"},
			target:   "/api/organizations/acme/data-sources/acme-postgres/queries/list-active-users",
			response: queryPayload,
		},
		{
			name:     "QueryExecutionList",
			args:     []string{"braidplane", "query", "execution", "list", "list-active-users", "--data-source", "acme-postgres", "--limit", "5", "--cursor", "page-2", "--sort-by", "query_execution_executed_at", "--order", "desc", "--direction", "prev"},
			target:   "/api/organizations/acme/data-sources/acme-postgres/queries/list-active-users/executions?limit=5&cursor=page-2&sort_by=query_execution_executed_at&order=desc&direction=prev",
			response: `{"executions":[` + executionPayload + `],"has_more":false}`,
		},
		{
			name:     "QueryExecutionGet",
			args:     []string{"braidplane", "query", "execution", "get", "list-active-users", "exec-1", "--data-source", "acme-postgres"},
			target:   "/api/organizations/acme/data-sources/acme-postgres/queries/list-active-users/executions/exec-1",
			response: executionPayload,
		},
		{
			name:     "QueryExecutionSummary",
			args:     []string{"braidplane", "query", "execution", "summary"},
			target:   "/api/organizations/acme/query-execution-summary",
			response: `{"successful_execution":{"execution_slug":"exec-1","data_source_slug":"warehouse","query_slug":"read-users","executed_at":"2026-06-14T12:01:00Z"}}`,
		},
		{
			name:     "QueryExecutionSummaryWithoutRecordedResult",
			args:     []string{"braidplane", "query", "execution", "summary"},
			target:   "/api/organizations/acme/query-execution-summary",
			response: `{"successful_execution":null}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotMethod string
			var gotTarget string
			var gotAuth string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
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

			stdout, stderr, err := runCLIForQueryTest(t, server.URL, tt.args)
			require.NoError(t, err)
			require.NotEmpty(t, stdout)
			require.Empty(t, stderr)
			require.Equal(t, http.MethodGet, gotMethod)
			require.Equal(t, tt.target, gotTarget)
			require.Equal(t, "Bearer xto_test_token", gotAuth)
			if tt.name == "QueryExecutionSummary" || tt.name == "QueryExecutionSummaryWithoutRecordedResult" {
				require.JSONEq(t, tt.response, stdout)
			}
		})
	}
}

func TestRunWithArgsQueryExecutionSummaryDoesNotHideUnavailableEvidence(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusServiceUnavailable} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"code":"QUERY_SUMMARY_UNAVAILABLE","message":"Cannot inspect query progress"}}`))
		}))
		stdout, _, err := runCLIForQueryTest(t, server.URL, []string{"braidplane", "query", "execution", "summary"})
		server.Close()
		require.Error(t, err)
		require.Empty(t, stdout)
	}
}

// CLI auth resolution reads process environment variables.
func TestRunWithArgsQueryExecuteRejectsInvalidParametersWithoutEchoingValues(t *testing.T) {
	secretValue := "super-secret-query-token"

	stdout, stderr, err := runCLIForQueryTest(
		t,
		"http://127.0.0.1:1",
		[]string{
			"braidplane", "query", "execute", "list-active-users",
			"--data-source", "acme-postgres",
			"--parameters-json", `{"api_token":"` + secretValue + `"`,
		},
	)

	require.Error(t, err)
	require.Empty(t, stdout)
	require.NotContains(t, stderr, secretValue)
	require.NotContains(t, err.Error(), secretValue)
}

func TestRunWithArgsQueryHelpDescribesCommands(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithArgs([]string{"braidplane", "query", "--help"}, &stdout, &stderr)

	require.NoError(t, err)
	require.Contains(t, stdout.String(), "create")
	require.Contains(t, stdout.String(), "execute")
	require.Contains(t, stdout.String(), "execution")
	require.Empty(t, stderr.String())
}

func runCLIForQueryTest(t *testing.T, serverURL string, args []string) (string, string, error) {
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

func TestRunWithArgsQueryExecuteReportsHTTPRowLimitExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/organizations/acme/data-sources/http-posts/queries/posts/execute", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"EXECUTION_ROW_LIMIT_EXCEEDED","message":"execution row limit exceeded","detailed_error":"HTTP request completed with status 201; verify remote state before retrying a write."}}`))
	}))
	t.Cleanup(server.Close)
	stdout, _, err := runCLIForQueryTest(t, server.URL, []string{
		"braidplane", "query", "execute", "posts", "--data-source", "http-posts",
	})
	require.ErrorContains(t, err, "EXECUTION_ROW_LIMIT_EXCEEDED")
	require.ErrorContains(t, err, "HTTP request completed with status 201")
	require.ErrorContains(t, err, "verify remote state before retrying a write")
	require.Empty(t, stdout, "over-limit execution must not print a successful result")
}
