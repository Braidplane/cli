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

func TestOrganizationRetentionCommandsUseConfiguredTenant(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		args         []string
		method       string
		path         string
		responseBody string
		wantBody     string
		wantOutput   []string
	}{
		{
			name:         "get policy",
			args:         []string{"organization", "retention", "get"},
			method:       http.MethodGet,
			path:         "/organizations/acme/retention",
			responseBody: retentionPolicyJSON(),
			wantOutput:   []string{`"query_execution_retention_days": 2`, `"event_log_retention_days": 5`},
		},
		{
			name: "update policy",
			args: []string{
				"organization", "retention", "update",
				"--query-execution-retention-days", "2",
				"--replay-checkpoint-retention-days", "3",
				"--run-history-retention-days", "4",
				"--event-log-retention-days", "5",
			},
			method:       http.MethodPut,
			path:         "/organizations/acme/retention",
			responseBody: retentionPolicyJSON(),
			wantBody:     retentionPolicyJSON(),
			wantOutput:   []string{`"replay_checkpoint_retention_days": 3`, `"run_history_retention_days": 4`},
		},
		{
			name:   "list maintenance runs",
			args:   []string{"organization", "retention", "run", "list"},
			method: http.MethodGet,
			path:   "/organizations/acme/retention/runs",
			responseBody: `{
				"schedule":{"cadence":"daily","timezone":"UTC","next_run_at":"2026-07-24T00:00:00Z"},
				"runs":[{
					"slug":"retention-run-safe-001",
					"status":"succeeded",
					"attempt":2,
					"policy_snapshot":{
						"query_execution_retention_days":2,
						"replay_checkpoint_retention_days":3,
						"run_history_retention_days":4,
						"event_log_retention_days":5
					},
					"outcome":{
						"query_executions_deleted":4,
						"replay_checkpoints_deleted":3,
						"pipeline_runs_deleted":2,
						"event_logs_deleted":1
					},
					"failure_code":null,
					"started_at":"2026-07-23T23:59:00Z",
					"completed_at":"2026-07-24T00:00:00Z"
				}]
			}`,
			wantOutput: []string{`"cadence": "daily"`, `"slug": "retention-run-safe-001"`, `"event_logs_deleted": 1`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotAuth string
			var gotBody []byte
			var gotMethod string
			var gotPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path
				gotAuth = r.Header.Get("Authorization")
				var err error
				gotBody, err = io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, "read request body", http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, err = w.Write([]byte(tt.responseBody))
				if err != nil {
					t.Errorf("write response: %v", err)
				}
			}))
			t.Cleanup(server.Close)

			args := append(
				[]string{"braidplane"},
				tt.args...,
			)
			args = append(args, "--server-url", server.URL, "--org", "acme", "--api-key", "xto_retention_test")
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			err := runWithRuntime(
				args,
				&stdout,
				&stderr,
				cliRuntime{configPath: filepath.Join(t.TempDir(), "missing.toml")},
			)

			require.NoError(t, err)
			require.Equal(t, tt.method, gotMethod)
			require.Equal(t, tt.path, gotPath)
			require.Equal(t, "Bearer xto_retention_test", gotAuth)
			if tt.wantBody == "" {
				require.Empty(t, gotBody)
			} else {
				require.JSONEq(t, tt.wantBody, string(gotBody))
			}
			for _, expected := range tt.wantOutput {
				require.Contains(t, stdout.String(), expected)
			}
			for _, forbidden := range []string{
				"source_job_id",
				"river_job",
				"executor_queue",
				"credential",
				"DELETE FROM",
			} {
				require.NotContains(t, stdout.String(), forbidden)
			}
			require.Empty(t, stderr.String())
		})
	}
}

func retentionPolicyJSON() string {
	return `{
		"query_execution_retention_days":2,
		"replay_checkpoint_retention_days":3,
		"run_history_retention_days":4,
		"event_log_retention_days":5
	}`
}
