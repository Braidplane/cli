package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompletionTriggerPayload(t *testing.T) {
	t.Parallel()
	for _, condition := range []string{"success", "final_failure"} {
		for _, grant := range []bool{false, true} {
			f := completionTriggerFlags()
			f.CompletionCondition, f.ExecutionGrantEnabled, f.ExecutionGrantDisabled = condition, grant, !grant
			created, err := f.newPipelineTrigger("after-upstream")
			require.NoError(t, err)
			updated, err := f.updatePipelineTrigger()
			require.NoError(t, err)
			for _, body := range []any{created, updated} {
				encoded, err := json.Marshal(body)
				require.NoError(t, err)
				var payload map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(encoded, &payload))
				var config api.PipelineTriggerCompletionConfig
				require.NoError(t, json.Unmarshal(payload["completion"], &config))
				require.Equal(t, api.PipelineTriggerCompletionConfig{UpstreamPipelineSlug: "upstream", Condition: api.PipelineCompletionCondition(condition), ExecutionGrantEnabled: grant}, config)
				require.JSONEq(t, `"completion"`, string(payload["type"]))
				require.JSONEq(t, `"downstream"`, string(payload["pipeline_slug"]))
				require.NotContains(t, payload, "schedule")
				require.NotContains(t, payload, "webhook")
			}
		}
	}
}

func completionTriggerFlags() pipelineTriggerPayloadFlags {
	return pipelineTriggerPayloadFlags{Name: "After upstream", PipelineSlug: "downstream", Type: "completion", Disabled: true, VersionPolicy: "latest_deployed", UpstreamPipeline: "upstream", CompletionCondition: "success", ExecutionGrantEnabled: true, RetryMaxAttempts: 1, RetryBackoff: "none"}
}

func TestCompletionTriggerPayloadValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*pipelineTriggerPayloadFlags)
		want   string
	}{
		{"missing upstream", func(f *pipelineTriggerPayloadFlags) { f.UpstreamPipeline = "" }, "valid --upstream-pipeline"},
		{"invalid upstream", func(f *pipelineTriggerPayloadFlags) { f.UpstreamPipeline = "bad/slug" }, "valid --upstream-pipeline"},
		{"missing condition", func(f *pipelineTriggerPayloadFlags) { f.CompletionCondition = "" }, "--completion-condition"},
		{"unsupported cancellation", func(f *pipelineTriggerPayloadFlags) { f.CompletionCondition = "cancelled" }, "--completion-condition"},
		{"missing grant choice", func(f *pipelineTriggerPayloadFlags) { f.ExecutionGrantEnabled = false }, "exactly one"},
		{"conflicting grant choice", func(f *pipelineTriggerPayloadFlags) { f.ExecutionGrantDisabled = true }, "exactly one"},
		{"schedule config", func(f *pipelineTriggerPayloadFlags) { f.Cron = "* * * * *" }, "must not set schedule or webhook"},
		{"webhook config", func(f *pipelineTriggerPayloadFlags) { f.WebhookInputMapping = []string{"/id=id"} }, "must not set schedule or webhook"},
		{"schedule with completion flags", func(f *pipelineTriggerPayloadFlags) { f.Type = "schedule" }, "require --type completion"},
		{"webhook with completion flags", func(f *pipelineTriggerPayloadFlags) { f.Type = "webhook" }, "require --type completion"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := completionTriggerFlags()
			tt.change(&f)
			_, err := f.newPipelineTrigger("after-upstream")
			require.ErrorContains(t, err, tt.want)
			_, err = f.updatePipelineTrigger()
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestCompletionTriggerCommandRequests(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"create", "update"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			called := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				var body api.NewPipelineTrigger
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.Equal(t, api.PipelineTriggerTypeCompletion, body.Type)
				assert.Equal(t, "downstream", body.PipelineSlug)
				assert.Equal(t, &api.PipelineTriggerCompletionConfig{UpstreamPipelineSlug: "upstream", Condition: api.PipelineCompletionConditionSuccess, ExecutionGrantEnabled: operation == "create"}, body.Completion)
				method, path := http.MethodPost, "/api/organizations/acme/projects/etl/environments/dev/pipeline-triggers"
				if operation == "update" {
					method = http.MethodPut
					path += "/after-upstream"
				}
				assert.Equal(t, method, r.Method)
				assert.Equal(t, path, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				if operation == "create" {
					w.WriteHeader(http.StatusCreated)
				}
				response := `{"slug":"after-upstream","type":"completion"}`
				if operation == "create" {
					response = `{"pipeline_trigger":` + response + `}`
				}
				_, _ = w.Write([]byte(response))
			}))
			t.Cleanup(server.Close)
			configPath := filepath.Join(t.TempDir(), "braidplane.toml")
			require.NoError(t, saveLogin(configPath, cliAuthConfig{ServerURL: server.URL + "/api", Organization: "acme", APIKey: "xto_test_token", Environment: "dev"}))
			args := []string{"braidplane", "pipeline", "trigger", operation}
			if operation == "update" {
				args = append(args, "after-upstream")
			}
			grant := "--execution-grant-enabled"
			if operation == "update" {
				grant = "--execution-grant-disabled"
			}
			args = append(args, "--project", "etl", "--name", "After upstream", "--pipeline", "downstream", "--type", "completion", "--disabled", "--version-policy", "latest_deployed", "--upstream-pipeline", "upstream", "--completion-condition", "success", grant)
			var stdout, stderr strings.Builder
			require.NoError(t, runWithRuntime(args, &stdout, &stderr, cliRuntime{configPath: configPath}))
			require.True(t, called)
			require.Contains(t, stdout.String(), `"completion"`)
		})
	}
}

func TestCompletionRunInspectionCommand(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/organizations/acme/projects/etl/environments/dev/runs/retry-2/completion", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"root_run_slug":"root","closed":false,"waiting_for_retry":true,"outcome":null,"final_attempt_run_slug":null,"decision_reason":null,"attempts":[{"run_slug":"root","attempt_number":1,"status":"failed","version_hash":"immutable-v1","deployment_revision":4}],"parent":null,"children":[{"trigger_slug":"after-root","trigger_revision":2,"state":"refused","decision_reason":"deployment_missing","child_run_slug":null}]}`))
	}))
	t.Cleanup(server.Close)
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	require.NoError(t, saveLogin(configPath, cliAuthConfig{ServerURL: server.URL + "/api", Organization: "acme", APIKey: "xto_test_token", Environment: "dev"}))
	var stdout, stderr strings.Builder
	require.NoError(t, runWithRuntime([]string{"braidplane", "pipeline", "run", "completion", "retry-2", "--project", "etl"}, &stdout, &stderr, cliRuntime{configPath: configPath}))
	require.Contains(t, stdout.String(), `"waiting_for_retry": true`)
	require.Contains(t, stdout.String(), `"version_hash": "immutable-v1"`)
	require.Contains(t, stdout.String(), `"decision_reason": "deployment_missing"`)
	require.Contains(t, stdout.String(), `"child_run_slug": null`)
}

func TestCompletionRunSourceTextKeepsAttemptIdentity(t *testing.T) {
	t.Parallel()
	var source api.PipelineRunSourceSummary
	require.NoError(t, json.Unmarshal([]byte(`{"type":"completion","summary":"Completion-triggered run","entrypoint":"scheduler","actor_type":"system","attempt_number":2,"root_run_slug":"completion-root","retry_of_run_slug":"completion-root"}`), &source))
	require.Equal(t, "completion", pipelineRunSourceTypeText(&source))
	require.Equal(t, "Completion-triggered run via scheduler", pipelineRunSourceText(&source))
	require.Equal(t, "2", pipelineRunSourceAttemptText(&source))
	require.Equal(t, "completion-root", pipelineRunSourceRootRunText(&source))
}

func TestConnectedRunInspectionCommand(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/organizations/acme/projects/etl/environments/dev/runs/child/connected", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"selected_run_slug":"child","nodes":[{"pipeline_slug":"upstream","completion":{"root_run_slug":"root","closed":true,"outcome":"succeeded","attempts":[{"run_slug":"root-retry-2","attempt_number":2,"status":"succeeded","version_hash":"immutable-v1","deployment_revision":4}],"children":[{"trigger_slug":"after-root","trigger_revision":7,"state":"accepted","child_run_slug":"child","selected_resolution_digest":"sha256:recorded"}]}}],"truncated":true,"context_unavailable":false}`))
	}))
	t.Cleanup(server.Close)
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	require.NoError(t, saveLogin(configPath, cliAuthConfig{ServerURL: server.URL + "/api", Organization: "acme", APIKey: "xto_test_token", Environment: "dev"}))
	var stdout, stderr strings.Builder
	require.NoError(t, runWithRuntime([]string{"braidplane", "pipeline", "run", "completion", "child", "--connected", "--project", "etl"}, &stdout, &stderr, cliRuntime{configPath: configPath}))
	require.Contains(t, stdout.String(), `"selected_run_slug": "child"`)
	require.Contains(t, stdout.String(), `"version_hash": "immutable-v1"`)
	require.Contains(t, stdout.String(), `"selected_resolution_digest": "sha256:recorded"`)
	require.Contains(t, stdout.String(), `"truncated": true`)
}
