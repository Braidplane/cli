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

const notificationRecordPayload = `{
	"slug":"notif-001",
	"notification_type":"pipeline.retry_exhausted",
	"severity":"critical",
	"lifecycle_state":"open",
	"summary":"Pipeline retry exhausted",
	"source_event_slug":null,
	"source_entity_type":"pipeline_run",
	"source_action":"execute",
	"source_refs":{"pipeline_slug":"daily-ledger","run_slug":"run-001"},
	"safe_metadata":{},
	"occurrence_count":1,
	"first_seen_at":"2026-07-03T10:00:00Z",
	"last_seen_at":"2026-07-03T10:05:00Z",
	"last_occurrence_at":"2026-07-03T10:05:00Z",
	"dispatch_statuses":[],
	"acknowledged_at":null,
	"resolved_at":null,
	"created_at":"2026-07-03T10:00:00Z",
	"updated_at":"2026-07-03T10:05:00Z"
}`

// CLI auth resolution reads process environment variables.
func TestRunWithArgsNotificationCommandsUseExpectedRequests(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		method     string
		path       string
		query      map[string]string
		statusCode int
		response   string
	}{
		{
			name: "List",
			args: []string{
				"braidplane", "notification", "list",
				"--limit", "10",
				"--cursor", "page-2",
				"--direction", "prev",
				"--lifecycle-state", "acknowledged",
				"--notification-type", "pipeline.retry_exhausted",
				"--severity", "critical",
				"--source-kind", "pipeline_run",
				"--source-action", "execute",
				"--source-ref", "daily-ledger",
				"--sort-by", "notification_last_seen_at",
				"--order", "desc",
			},
			method: http.MethodGet,
			path:   "/api/organizations/acme/notifications",
			query: map[string]string{
				"limit":             "10",
				"cursor":            "page-2",
				"direction":         "prev",
				"lifecycle_state":   "acknowledged",
				"notification_type": "pipeline.retry_exhausted",
				"severity":          "critical",
				"source_kind":       "pipeline_run",
				"source_action":     "execute",
				"source_ref":        "daily-ledger",
				"sort_by":           "notification_last_seen_at",
				"order":             "desc",
			},
			statusCode: http.StatusOK,
			response:   `{"notifications":[` + notificationRecordPayload + `],"has_more":false}`,
		},
		{
			name:       "Show",
			args:       []string{"braidplane", "notification", "show", "notif-001"},
			method:     http.MethodGet,
			path:       "/api/organizations/acme/notifications/notif-001",
			statusCode: http.StatusOK,
			response:   notificationRecordPayload,
		},
		{
			name:       "Acknowledge",
			args:       []string{"braidplane", "notification", "acknowledge", "notif-001"},
			method:     http.MethodPost,
			path:       "/api/organizations/acme/notifications/notif-001/acknowledge",
			statusCode: http.StatusOK,
			response:   notificationRecordPayload,
		},
		{
			name:       "Resolve",
			args:       []string{"braidplane", "notification", "resolve", "notif-001"},
			method:     http.MethodPost,
			path:       "/api/organizations/acme/notifications/notif-001/resolve",
			statusCode: http.StatusOK,
			response:   notificationRecordPayload,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotMethod string
			var gotPath string
			var gotAuth string
			var gotBody []byte
			gotQuery := map[string]string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path
				gotAuth = r.Header.Get("Authorization")
				for key := range tt.query {
					gotQuery[key] = r.URL.Query().Get(key)
				}
				var err error
				gotBody, err = io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read request body: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			t.Cleanup(server.Close)

			stdout, stderr, err := runCLIForNotificationTest(t, server.URL, tt.args)

			require.NoError(t, err)
			require.Empty(t, stderr)
			require.NotEmpty(t, stdout)
			require.Equal(t, tt.method, gotMethod)
			require.Equal(t, tt.path, gotPath)
			if tt.query != nil {
				require.Equal(t, tt.query, gotQuery)
			} else {
				require.Empty(t, gotQuery)
			}
			require.Equal(t, "Bearer xto_test_token", gotAuth)
			require.Empty(t, string(gotBody))
		})
	}
}

func TestRunWithArgsNotificationListRejectsInvalidFlagsBeforeAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		wantDetails []string
	}{
		{
			name:        "LimitTooLow",
			args:        []string{"--limit", "0"},
			wantDetails: []string{"limit must be between 1 and 100"},
		},
		{
			name:        "LimitTooHigh",
			args:        []string{"--limit", "101"},
			wantDetails: []string{"limit must be between 1 and 100"},
		},
		{
			name:        "Direction",
			args:        []string{"--direction", "sideways"},
			wantDetails: []string{"direction must be next or prev"},
		},
		{
			name:        "Lifecycle",
			args:        []string{"--lifecycle-state", "closed"},
			wantDetails: []string{"lifecycle-state must be open, acknowledged, or resolved"},
		},
		{
			name:        "Severity",
			args:        []string{"--severity", "emergency"},
			wantDetails: []string{"severity must be info, warning, or critical"},
		},
		{
			name:        "SourceKind",
			args:        []string{"--source-kind", "spaceship"},
			wantDetails: []string{"source-kind must be a supported event entity type"},
		},
		{
			name:        "SourceActionRequiresKind",
			args:        []string{"--source-action", "execute"},
			wantDetails: []string{"source-action requires source-kind"},
		},
		{
			name:        "NotificationType",
			args:        []string{"--notification-type", "billing.failed"},
			wantDetails: []string{"current producer-backed value is pipeline.retry_exhausted"},
		},
		{
			name:        "SortBy",
			args:        []string{"--sort-by", "created_at"},
			wantDetails: []string{"sort-by must be notification_last_seen_at"},
		},
		{
			name:        "Order",
			args:        []string{"--order", "newest"},
			wantDetails: []string{"order must be asc or desc"},
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
			args := append([]string{"braidplane", "notification", "list"}, tt.args...)
			err := runWithRuntime(args, &stdout, &stderr, cliRuntime{configPath: configPath})

			requireValidationFailed(t, err, tt.wantDetails...)
			require.False(t, serverCalled.Load())
			require.Empty(t, stdout.String())
			require.Empty(t, stderr.String())
		})
	}
}

func runCLIForNotificationTest(t *testing.T, serverURL string, args []string) (string, string, error) {
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
