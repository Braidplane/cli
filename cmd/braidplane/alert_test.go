package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

const alertDestinationPayload = `{
	"slug":"ops-email",
	"name":"Ops email",
	"type":"email",
	"non_secret_config":{"recipients":["ops@example.com","data@example.com"]},
	"enabled":true,
	"created_at":"2026-07-03T10:00:00Z",
	"updated_at":"2026-07-03T10:05:00Z"
}`

const alertDestinationTestPayload = `{
	"status":"accepted",
	"destination_slug":"ops-email",
	"destination_type":"email",
	"recipient_count":2,
	"diagnostics_persisted":false,
	"enqueued_jobs":[
		{"kind":"email_send","queue":"email","state":"queued"},
		{"kind":"email_send","queue":"email","state":"queued"}
	]
}`

const alertPolicyPayload = `{
	"slug":"critical-pipeline",
	"name":"Critical pipeline",
	"scope":"project_environment",
	"project_slug":"ops",
	"environment_slug":"prod",
	"notification_type":"pipeline.retry_exhausted",
	"severity":"critical",
	"lifecycle_state":"open",
	"source_kind":"pipeline_run",
	"source_action":"execute",
	"source_ref":"daily-ledger",
	"destination_slugs":["ops-email","data-email"],
	"enabled":true,
	"created_at":"2026-07-03T10:00:00Z",
	"updated_at":"2026-07-03T10:05:00Z"
}`

// CLI auth resolution reads process environment variables.
func TestRunWithArgsAlertDestinationCommandsUseExpectedRequests(t *testing.T) {
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
			name: "List",
			args: []string{
				"braidplane", "alert", "destination", "list",
				"--limit", "10",
				"--cursor", "page-2",
				"--direction", "next",
				"--sort-by", "alert_destination_created_at",
				"--order", "asc",
			},
			method:     http.MethodGet,
			target:     "/api/organizations/acme/alert-destinations?limit=10&cursor=page-2&sort_by=alert_destination_created_at&order=asc&direction=next",
			statusCode: http.StatusOK,
			response:   `{"alert_destinations":[` + alertDestinationPayload + `],"has_more":false}`,
			wantOutput: true,
		},
		{
			name:       "Show",
			args:       []string{"braidplane", "alert", "destination", "show", "ops-email"},
			method:     http.MethodGet,
			target:     "/api/organizations/acme/alert-destinations/ops-email",
			statusCode: http.StatusOK,
			response:   alertDestinationPayload,
			wantOutput: true,
		},
		{
			name: "Create",
			args: []string{
				"braidplane", "alert", "destination", "create",
				"--name", "Ops email",
				"--slug", "ops-email",
				"--recipient", "ops@example.com",
				"--recipient", "data@example.com",
			},
			method:     http.MethodPost,
			target:     "/api/organizations/acme/alert-destinations",
			statusCode: http.StatusCreated,
			response:   alertDestinationPayload,
			wantBody:   `{"name":"Ops email","slug":"ops-email","type":"email","non_secret_config":{"recipients":["ops@example.com","data@example.com"]}}`,
			wantOutput: true,
		},
		{
			name: "Update",
			args: []string{
				"braidplane", "alert", "destination", "update", "ops-email",
				"--name", "Ops email",
				"--recipient", "ops@example.com",
				"--disabled",
			},
			method:     http.MethodPut,
			target:     "/api/organizations/acme/alert-destinations/ops-email",
			statusCode: http.StatusOK,
			response:   alertDestinationPayload,
			wantBody:   `{"name":"Ops email","type":"email","non_secret_config":{"recipients":["ops@example.com"]},"enabled":false}`,
			wantOutput: true,
		},
		{
			name:       "Delete",
			args:       []string{"braidplane", "alert", "destination", "delete", "ops-email"},
			method:     http.MethodDelete,
			target:     "/api/organizations/acme/alert-destinations/ops-email",
			statusCode: http.StatusNoContent,
		},
		{
			name:       "Test",
			args:       []string{"braidplane", "alert", "destination", "test", "ops-email"},
			method:     http.MethodPost,
			target:     "/api/organizations/acme/alert-destinations/ops-email/test",
			statusCode: http.StatusAccepted,
			response:   alertDestinationTestPayload,
			wantOutput: true,
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

			stdout, stderr, err := runCLIForAlertTest(t, server.URL, tt.args)

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
		})
	}
}

// CLI auth resolution reads process environment variables.
func TestRunWithArgsAlertPolicyCommandsUseExpectedRequests(t *testing.T) {
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
			name: "List",
			args: []string{
				"braidplane", "alert", "policy", "list",
				"--limit", "10",
				"--cursor", "page-2",
				"--direction", "prev",
				"--sort-by", "alert_policy_created_at",
				"--order", "desc",
			},
			method:     http.MethodGet,
			target:     "/api/organizations/acme/alert-policies?limit=10&cursor=page-2&sort_by=alert_policy_created_at&order=desc&direction=prev",
			statusCode: http.StatusOK,
			response:   `{"alert_policies":[` + alertPolicyPayload + `],"has_more":false}`,
			wantOutput: true,
		},
		{
			name:       "Show",
			args:       []string{"braidplane", "alert", "policy", "show", "critical-pipeline"},
			method:     http.MethodGet,
			target:     "/api/organizations/acme/alert-policies/critical-pipeline",
			statusCode: http.StatusOK,
			response:   alertPolicyPayload,
			wantOutput: true,
		},
		{
			name: "Create",
			args: []string{
				"braidplane", "alert", "policy", "create",
				"--name", "Critical pipeline",
				"--slug", "critical-pipeline",
				"--scope", "project_environment",
				"--project", "ops",
				"--environment", "prod",
				"--notification-type", "pipeline.retry_exhausted",
				"--severity", "critical",
				"--lifecycle-state", "open",
				"--source-kind", "pipeline_run",
				"--source-action", "execute",
				"--source-ref", "daily-ledger",
				"--destination", "ops-email",
				"--destination", "data-email",
			},
			method:     http.MethodPost,
			target:     "/api/organizations/acme/alert-policies",
			statusCode: http.StatusCreated,
			response:   alertPolicyPayload,
			wantBody: `{
				"name":"Critical pipeline",
				"slug":"critical-pipeline",
				"scope":"project_environment",
				"project_slug":"ops",
				"environment_slug":"prod",
				"notification_type":"pipeline.retry_exhausted",
				"severity":"critical",
				"lifecycle_state":"open",
				"source_kind":"pipeline_run",
				"source_action":"execute",
				"source_ref":"daily-ledger",
				"destination_slugs":["ops-email","data-email"]
			}`,
			wantOutput: true,
		},
		{
			name: "Update",
			args: []string{
				"braidplane", "alert", "policy", "update", "critical-pipeline",
				"--name", "Critical pipeline",
				"--scope", "project",
				"--project", "ops",
				"--severity", "warning",
				"--destination", "ops-email",
				"--enabled",
			},
			method:     http.MethodPut,
			target:     "/api/organizations/acme/alert-policies/critical-pipeline",
			statusCode: http.StatusOK,
			response:   alertPolicyPayload,
			wantBody: `{
				"name":"Critical pipeline",
				"scope":"project",
				"project_slug":"ops",
				"severity":"warning",
				"destination_slugs":["ops-email"],
				"enabled":true
			}`,
			wantOutput: true,
		},
		{
			name:       "Delete",
			args:       []string{"braidplane", "alert", "policy", "delete", "critical-pipeline"},
			method:     http.MethodDelete,
			target:     "/api/organizations/acme/alert-policies/critical-pipeline",
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

			stdout, stderr, err := runCLIForAlertTest(t, server.URL, tt.args)

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
		})
	}
}

func TestRunWithArgsAlertCommandsRejectInvalidFlagsBeforeAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		wantDetails []string
	}{
		{
			name: "DestinationCreateRejectsBlankName",
			args: []string{
				"alert", "destination", "create",
				"--name", "   ",
				"--recipient", "ops@example.com",
			},
			wantDetails: []string{"set --name"},
		},
		{
			name: "DestinationUpdateRejectsBlankName",
			args: []string{
				"alert", "destination", "update", "ops-email",
				"--name", "   ",
				"--recipient", "ops@example.com",
				"--enabled",
			},
			wantDetails: []string{"set --name"},
		},
		{
			name: "DestinationUpdateRequiresEnabledChoice",
			args: []string{
				"alert", "destination", "update", "ops-email",
				"--name", "Ops email",
				"--recipient", "ops@example.com",
			},
			wantDetails: []string{"set --enabled or --disabled"},
		},
		{
			name: "PolicyCreateRejectsBlankName",
			args: []string{
				"alert", "policy", "create",
				"--name", "   ",
				"--severity", "critical",
				"--destination", "ops-email",
			},
			wantDetails: []string{"set --name"},
		},
		{
			name: "PolicyUpdateRejectsBlankName",
			args: []string{
				"alert", "policy", "update", "critical-retries",
				"--name", "   ",
				"--severity", "critical",
				"--destination", "ops-email",
				"--enabled",
			},
			wantDetails: []string{"set --name"},
		},
		{
			name: "PolicyRequiresPrimarySelector",
			args: []string{
				"alert", "policy", "create",
				"--name", "Policy",
				"--destination", "ops-email",
			},
			wantDetails: []string{"set --notification-type, --severity, or --source-kind"},
		},
		{
			name: "PolicyProjectScopeRequiresProject",
			args: []string{
				"alert", "policy", "create",
				"--name", "Policy",
				"--scope", "project",
				"--severity", "critical",
				"--destination", "ops-email",
			},
			wantDetails: []string{"set --project for project scope"},
		},
		{
			name: "PolicySourceRefRequiresSourceKind",
			args: []string{
				"alert", "policy", "create",
				"--name", "Policy",
				"--severity", "critical",
				"--source-ref", "daily-ledger",
				"--destination", "ops-email",
			},
			wantDetails: []string{"source-ref requires source-kind"},
		},
		{
			name: "PolicyDuplicateDestinations",
			args: []string{
				"alert", "policy", "create",
				"--name", "Policy",
				"--severity", "critical",
				"--destination", "ops-email",
				"--destination", "ops-email",
			},
			wantDetails: []string{"destination slugs must be unique"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
			args := append([]string{"braidplane"}, tt.args...)
			err := runWithRuntime(args, &stdout, &stderr, cliRuntime{configPath: configPath})

			requireValidationFailed(t, err, tt.wantDetails...)
			require.False(t, serverCalled.Load())
			require.Empty(t, stdout.String())
			require.Empty(t, stderr.String())
		})
	}
}

func TestRunWithArgsAlertHelpKeepsNotificationTypeGuidanceNarrow(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithArgs([]string{"braidplane", "alert", "policy", "create", "--help"}, &stdout, &stderr)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Contains(t, stdout.String(), "pipeline.retry_exhausted")
	require.NotContains(t, stdout.String(), "pipeline.run_failed")
	require.NotContains(t, stdout.String(), "pipeline.contract_failure")
}

func runCLIForAlertTest(t *testing.T, serverURL string, args []string) (string, string, error) {
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
