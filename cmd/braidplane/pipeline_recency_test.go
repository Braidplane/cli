package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPipelineRecencyCommands(t *testing.T) {
	for _, operation := range []string{"get", "set", "edit", "disable", "conflict", "denied", "disabled"} {
		t.Run(operation, func(t *testing.T) {
			mutations := 0
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/organizations/acme/pipeline-operational-capabilities" {
					assert.Equal(t, "GET", r.Method)
					if operation == "disabled" {
						_, _ = w.Write([]byte(`{"successful_run_recency":false,"notification_types":[]}`))
					} else {
						_, _ = w.Write([]byte(`{"successful_run_recency":true,"notification_types":["pipeline.freshness_sla_breached"]}`))
					}
					return
				}
				assert.Equal(t, "/api/organizations/acme/projects/ops/environments/dev/pipelines/orders/successful-run-recency", r.URL.Path)
				if operation == "denied" {
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"error":{"code":"FORBIDDEN","message":"Forbidden"}}`))
					return
				}
				if r.Method == http.MethodPut {
					mutations++
					var body map[string]any
					if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					revision, age := float64(0), float64(3600)
					if operation == "edit" {
						revision, age = 1, 7200
					}
					assert.InDelta(t, revision, body["expected_revision"], 0)
					assert.InDelta(t, age, body["maximum_age_seconds"], 0)
					assert.Equal(t, operation != "disable", body["enabled"])
					if operation == "conflict" {
						w.WriteHeader(409)
						_, _ = w.Write([]byte(`{"error":{"code":"RUN_RECENCY_EXPECTATION_CONFLICT","message":"reload before updating"}}`))
						return
					}
				} else {
					assert.Equal(t, "get", operation)
				}
				_, _ = w.Write([]byte(`{"capability_enabled":true,"configured":true,"revision":1,"maximum_age_seconds":3600,"enabled":true,"deployment_revision":7,"version_spec_hash":"exact-version","success_run_slug":"retained-run","outcome":"healthy","episodes":[],"evaluated_at":"2026-10-22T12:00:00Z"}`))
			}))
			defer backend.Close()
			config := filepath.Join(t.TempDir(), "cli.toml")
			require.NoError(t, saveLogin(config, cliAuthConfig{ServerURL: backend.URL + "/api", Organization: "acme", APIKey: "fixture-key"}))
			command := "set"
			if operation == "get" {
				command = "get"
			}
			args := []string{"braidplane", "pipeline", "recency", command, "orders", "--project", "ops", "--environment", "dev"}
			if command == "set" {
				rev, age := "0", "3600"
				if operation == "edit" {
					rev, age = "1", "7200"
				}
				args = append(args, "--expected-revision", rev, "--maximum-age-seconds", age)
				if operation == "disable" {
					args = append(args, "--no-enabled")
				}
			}
			var stdout, stderr bytes.Buffer
			err := runWithRuntime(args, &stdout, &stderr, cliRuntime{configPath: config})
			switch operation {
			case "disabled":
				require.ErrorContains(t, err, "unavailable")
				require.Zero(t, mutations)
			case "denied":
				require.ErrorContains(t, err, "Forbidden")
			case "conflict":
				require.ErrorContains(t, err, "reload")
			default:
				require.NoError(t, err)
				require.Contains(t, stdout.String(), `"deployment_revision": 7`)
				require.Contains(t, stdout.String(), "retained-run")
			}
		})
	}
}

func TestRecencyAlertPolicyUsesCapabilityMetadata(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		writes := 0
		backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/api/organizations/acme/pipeline-operational-capabilities" {
				if enabled {
					_, _ = w.Write([]byte(`{"successful_run_recency":true,"notification_types":["pipeline.freshness_sla_breached"]}`))
				} else {
					_, _ = w.Write([]byte(`{"successful_run_recency":false,"notification_types":[]}`))
				}
				return
			}
			assert.Equal(t, "/api/organizations/acme/alert-policies", r.URL.Path)
			assert.Equal(t, http.MethodPost, r.Method)
			writes++
			var body map[string]any
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			assert.Equal(t, "project_environment", body["scope"])
			assert.Equal(t, "ops", body["project_slug"])
			assert.Equal(t, "dev", body["environment_slug"])
			assert.Equal(t, "orders", body["source_ref"])
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"slug":"recency-alert","name":"Recency","enabled":true}`))
		}))
		config := filepath.Join(t.TempDir(), "cli.toml")
		require.NoError(t, saveLogin(config, cliAuthConfig{ServerURL: backend.URL + "/api", Organization: "acme", APIKey: "fixture-key"}))
		var stdout, stderr bytes.Buffer
		err := runWithRuntime([]string{"braidplane", "alert", "policy", "create", "--name", "Recency", "--scope", "project_environment", "--project", "ops", "--environment", "dev", "--notification-type", "pipeline.freshness_sla_breached", "--source-kind", "pipeline", "--source-ref", "orders", "--destination", "ops-email"}, &stdout, &stderr, cliRuntime{configPath: config})
		if enabled {
			require.NoError(t, err)
			require.Equal(t, 1, writes)
		} else {
			require.ErrorContains(t, err, "unavailable")
			require.Zero(t, writes)
		}
		backend.Close()
	}
}
