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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProofReceiptGetPrintsStableStructuredReceipt(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schema_version":1,"id":"proof-123","candidate":{"organization_slug":"acme","project_slug":"ops","environment_slug":"prod","provider_connection_slug":"github-main","repository_id":"repo","change_request_id":"42","head_sha":"abcdef1","target_ref":"main","pipeline_path":"pipelines/orders.yaml","spec_hash":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","binding_fingerprint":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","capability_fingerprint":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","compiler_version":"v1","policy_version":"v1"},"claims":[{"identity":{"family":"write_mode","pipeline_version_hash":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","semantic_path":"/sink"},"description":"append mode","required_evidence_level":"static","actual_evidence_level":null,"observed_at":null,"expires_at":null,"verdict":"unverified","evidence":[],"diagnostics":[]}],"aggregate_verdict":"unverified","supersedes_receipt_id":null,"created_at":"2026-08-31T12:00:00Z","retain_until":"2026-11-29T12:00:00Z","lifecycle_links":[]}`))
	}))
	t.Cleanup(server.Close)
	require.NoError(t, saveLogin(configPath, cliAuthConfig{ServerURL: server.URL + "/api", Organization: "acme"}))

	var stdout, stderr bytes.Buffer
	err := runWithRuntime([]string{"braidplane", "proof", "receipt", "get", "proof-123", "--project", "ops", "--environment", "prod", "--api-key", "token"}, &stdout, &stderr, cliRuntime{configPath: configPath})
	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Equal(t, "/api/organizations/acme/projects/ops/environments/prod/proof-receipts/proof-123", gotPath)
	require.JSONEq(t, `{"schema_version":1,"id":"proof-123","candidate":{"organization_slug":"acme","project_slug":"ops","environment_slug":"prod","provider_connection_slug":"github-main","repository_id":"repo","change_request_id":"42","head_sha":"abcdef1","target_ref":"main","pipeline_path":"pipelines/orders.yaml","spec_hash":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","binding_fingerprint":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","capability_fingerprint":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","compiler_version":"v1","policy_version":"v1"},"claims":[{"identity":{"family":"write_mode","pipeline_version_hash":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","semantic_path":"/sink"},"description":"append mode","required_evidence_level":"static","actual_evidence_level":null,"observed_at":null,"expires_at":null,"verdict":"unverified","evidence":[],"diagnostics":[]}],"aggregate_verdict":"unverified","supersedes_receipt_id":null,"created_at":"2026-08-31T12:00:00Z","retain_until":"2026-11-29T12:00:00Z","lifecycle_links":[]}`, stdout.String())
}

func TestProofConfigurationCommands(t *testing.T) {
	for _, provider := range []string{"github", "gitlab"} {
		for _, operation := range []string{"get", "set"} {
			for _, enabled := range []bool{false, true} {
				for _, publish := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/enabled=%t/publish=%t", provider, operation, enabled, publish), func(t *testing.T) {
						targets := `[{"project_slug":"ops","environment_slug":"prod","target_ref":"main","pipeline_path":"pipelines/orders.yaml"},{"project_slug":"ops","environment_slug":"dev","target_ref":"develop","pipeline_path":"pipelines/inventory.yaml"}]`
						settings := fmt.Sprintf(`{"enabled":%t,"publish_checks":%t,"expected_repository_id":"repo-123","targets":%s}`, enabled, publish, targets)
						response := fmt.Sprintf(`{"data_source_slug":"managed-source","name":"Managed source","provider":%q,"repository_id":"repo-123","enabled":%t,"publish_checks":%t,"webhook_setup_required":false,"status":"Configured","targets":%s,"available_targets":[{"project_slug":"ops","environment_slug":"prod","target_ref":"main","pipeline_directory":"pipelines"}]}`, provider, enabled, publish, targets)
						var gotPath, gotMethod, gotAuth string
						var gotBody []byte
						requests := 0
						server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							requests++
							gotPath, gotMethod, gotAuth = r.URL.Path, r.Method, r.Header.Get("Authorization")
							gotBody, _ = io.ReadAll(r.Body)
							w.Header().Set("Content-Type", "application/json")
							_, _ = w.Write([]byte(response))
						}))
						t.Cleanup(server.Close)
						configPath := filepath.Join(t.TempDir(), "braidplane.toml")
						require.NoError(t, saveLogin(configPath, cliAuthConfig{ServerURL: server.URL + "/api", Organization: "acme"}))
						args := []string{"braidplane", "proof", "configuration", operation, "managed-source", "--api-key", "test-key"}
						if operation == "set" {
							file := filepath.Join(t.TempDir(), "settings.json")
							require.NoError(t, os.WriteFile(file, []byte(settings), 0o600))
							args = append(args, "--file", file)
						}
						var stdout, stderr bytes.Buffer
						require.NoError(t, runWithRuntime(args, &stdout, &stderr, cliRuntime{configPath: configPath}))
						require.Empty(t, stderr.String())
						require.JSONEq(t, response, stdout.String())
						require.Equal(t, 1, requests)
						require.Equal(t, "/api/organizations/acme/data-sources/managed-source/proof", gotPath)
						require.Equal(t, "Bearer test-key", gotAuth)
						if operation == "set" {
							require.Equal(t, http.MethodPut, gotMethod)
							require.JSONEq(t, settings, string(gotBody))
						} else {
							require.Equal(t, http.MethodGet, gotMethod)
							require.Empty(t, gotBody)
						}
					})
				}
			}
		}
	}
}

func TestProofConfigurationRejectsIncompleteFilesBeforeRequest(t *testing.T) {
	complete := map[string]any{"enabled": false, "publish_checks": false, "expected_repository_id": "", "targets": []any{}}
	for _, field := range []string{"enabled", "publish_checks", "expected_repository_id", "targets"} {
		for _, null := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/null=%t", field, null), func(t *testing.T) {
				fields := make(map[string]any)
				for key, value := range complete {
					fields[key] = value
				}
				if null {
					fields[field] = nil
				} else {
					delete(fields, field)
				}
				data, err := json.Marshal(fields)
				require.NoError(t, err)
				assertProofSettingsFileRejected(t, string(data), "require explicit "+field)
			})
		}
	}
	for _, data := range []string{
		`{`, `null`, `[]`,
		`{"enabled":"false","publish_checks":false,"expected_repository_id":"","targets":[]}`,
		`{"enabled":false,"publish_checks":false,"expected_repository_id":"","targets":[],"unexpected":true}`,
		`{"enabled":false,"publish_checks":false,"expected_repository_id":"","targets":[]} {}`,
		`{"enabled":true,"publish_checks":false,"expected_repository_id":"repo","targets":[{"unexpected":true}]}`,
	} {
		t.Run(data, func(t *testing.T) { assertProofSettingsFileRejected(t, data, "") })
	}
}

func assertProofSettingsFileRejected(t *testing.T, data, message string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, os.WriteFile(file, []byte(data), 0o600))
	var stdout, stderr bytes.Buffer
	// No login exists: input errors must be returned before authentication or HTTP.
	err := runWithRuntime([]string{"braidplane", "proof", "configuration", "set", "managed-source", "--file", file}, &stdout, &stderr, cliRuntime{configPath: filepath.Join(t.TempDir(), "missing.toml")})
	require.Error(t, err)
	if message != "" {
		require.ErrorContains(t, err, message)
	}
	require.Empty(t, stdout.String())
}

func TestProofConfigurationAPIFailures(t *testing.T) {
	for _, operation := range []string{"get", "set"} {
		for _, status := range []int{400, 401, 403, 404, 500, 200} {
			t.Run(fmt.Sprintf("%s/%d", operation, status), func(t *testing.T) {
				requests := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests++
					// A 200 without a JSON payload must also fail explicitly.
					if status == 200 {
						w.WriteHeader(status)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(status)
					_, _ = w.Write([]byte(`{"error":{"code":"PROOF_CONFIGURATION_INVALID","message":"Repository changed or permission missing"}}`))
				}))
				t.Cleanup(server.Close)
				configPath := filepath.Join(t.TempDir(), "braidplane.toml")
				require.NoError(t, saveLogin(configPath, cliAuthConfig{ServerURL: server.URL + "/api", Organization: "acme"}))
				args := []string{"braidplane", "proof", "configuration", operation, "managed-source", "--api-key", "read-only-key"}
				if operation == "set" {
					file := filepath.Join(t.TempDir(), "settings.json")
					require.NoError(t, os.WriteFile(file, []byte(`{"enabled":true,"publish_checks":false,"expected_repository_id":"stale-repo","targets":[]}`), 0o600))
					args = append(args, "--file", file)
				}
				var stdout, stderr bytes.Buffer
				err := runWithRuntime(args, &stdout, &stderr, cliRuntime{configPath: configPath})
				require.Error(t, err)
				require.Empty(t, stdout.String())
				require.Equal(t, 1, requests)
				if status == 200 {
					require.ErrorContains(t, err, "missing Proof configuration payload")
				} else {
					require.ErrorContains(t, err, fmt.Sprintf("status %d", status))
					require.ErrorContains(t, err, "Repository changed or permission missing")
				}
			})
		}
	}
}

func TestProofConfigurationTransportFailures(t *testing.T) {
	for _, operation := range []string{"get", "set"} {
		t.Run(operation, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			server.Close()
			configPath := filepath.Join(t.TempDir(), "braidplane.toml")
			require.NoError(t, saveLogin(configPath, cliAuthConfig{ServerURL: server.URL, Organization: "acme"}))
			args := []string{"braidplane", "proof", "configuration", operation, "managed-source", "--api-key", "test-key"}
			if operation == "set" {
				file := filepath.Join(t.TempDir(), "settings.json")
				require.NoError(t, os.WriteFile(file, []byte(`{"enabled":false,"publish_checks":false,"expected_repository_id":"","targets":[]}`), 0o600))
				args = append(args, "--file", file)
			}
			var stdout, stderr bytes.Buffer
			err := runWithRuntime(args, &stdout, &stderr, cliRuntime{configPath: configPath})
			require.ErrorContains(t, err, operation+" Proof configuration")
			require.Empty(t, stdout.String())
		})
	}
}
