package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPipelineCompiledPlanCommandsSelectExactIdentity(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"compile", "compile without environment", "plan", "invalid", "projection"} {
		t.Run(operation, func(t *testing.T) {
			identity := strings.Repeat("a", 64)
			method, suffix := http.MethodPost, "/versions/"+identity+"/compiled-plans"
			command := "compile"
			if operation == "plan" {
				command, identity = "plan", "sha256:"+identity
				method, suffix = http.MethodGet, "/compiled-plans/"+identity
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, method, r.Method)
				assert.Equal(t, "/api/organizations/acme/projects/etl/pipelines/sync"+suffix, r.URL.Path)
				if operation == "compile" || operation == "invalid" {
					assert.Equal(t, "dev", r.URL.Query().Get("environment_slug"))
				} else {
					assert.Empty(t, r.URL.Query().Get("environment_slug"))
				}
				if operation == "projection" {
					var body map[string]any
					if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					projection := body["projection"].(map[string]any)
					assert.Len(t, projection["inputs"], 1)
					input := projection["inputs"].([]any)[0].(map[string]any)
					assert.Equal(t, []any{"object", "integer", "instant", "vector", "number"}, input["mapping"].(map[string]any)["types"])
					assert.Equal(t, map[string]any{"KIND": map[string]any{"value": "string", "revision": "variables-v1"}}, input["variables"])
					assert.NotContains(t, body, "source")
				}
				w.Header().Set("Content-Type", "application/json")
				if operation == "invalid" {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":{"code":"PIPELINE_CONTRACT_PLAN_INVALID","message":"pipeline contract plan is invalid"}}`))
					return
				}
				_, _ = w.Write([]byte(`{"digest":"sha256:retained","spec_hash":"full-retained-version","pipeline_slug":"sync","status":"available","admission_enabled":true,"operations":[],"pins":[]}`))
			}))
			defer server.Close()
			config := filepath.Join(t.TempDir(), "braidplane.toml")
			require.NoError(t, saveLogin(config, cliAuthConfig{ServerURL: server.URL + "/api", Organization: "acme", APIKey: "fixture-key"}))
			var stdout, stderr bytes.Buffer
			args := []string{"braidplane", "pipeline", "version", command, "sync", identity, "--project", "etl"}
			if operation == "compile" || operation == "invalid" {
				args = append(args, "--environment", "dev")
			}
			if operation == "projection" {
				file := filepath.Join(t.TempDir(), "projection.json")
				require.NoError(t, os.WriteFile(file, []byte(`{"inputs":[{"contract_slug":"orders","revision_slug":"v1","schema_object_id":"row","digest":"sha256:exact","mapping":{"id":"mapping/v1","representation":"json","root_shape":"object","types":["object","integer","instant","vector","number"],"physical":{}},"variables":{"KIND":{"value":"string","revision":"variables-v1"}}}]}`), 0o600))
				args = append(args, "--projection-file", file)
			}
			err := runWithRuntime(args, &stdout, &stderr, cliRuntime{configPath: config})
			if operation == "invalid" {
				require.ErrorContains(t, err, "invalid")
				return
			}
			require.NoError(t, err)
			require.Contains(t, stdout.String(), `"digest": "sha256:retained"`)
			require.Contains(t, stdout.String(), `"admission_enabled": true`)
		})
	}
}

func TestCompilePipelineVersionResourceScopesFile(t *testing.T) {
	t.Parallel()
	identity := strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/organizations/acme/projects/etl/pipelines/sync/versions/"+identity+"/compiled-plans", r.URL.Path)
		var body map[string]any
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
			return
		}
		scopes := body["resource_scopes"].([]any)
		if !assert.Len(t, scopes, 1) {
			return
		}
		scope := scopes[0].(map[string]any)
		assert.Equal(t, map[string]any{
			"step": "extract",
			"role": "query",
			"scope": map[string]any{
				"policy_id":      "s3/relative-key/v1",
				"data_source":    "warehouse",
				"operation":      "read-orders",
				"contract":       "orders",
				"revision":       "v1",
				"object_id":      "rows",
				"representation": "rows",
				"s3": map[string]any{
					"endpoint":         "https://s3.example.test",
					"region":           "us-east-1",
					"bucket":           "archive",
					"prefix":           "orders",
					"dynamic_key":      false,
					"force_path_style": true,
				},
			},
		}, scope)
		assert.NotNil(t, body["projection"])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"digest":"sha256:retained","spec_hash":"full-retained-version","pipeline_slug":"sync","status":"available","admission_enabled":true,"operations":[],"pins":[]}`))
	}))
	defer server.Close()
	config := filepath.Join(t.TempDir(), "braidplane.toml")
	require.NoError(t, saveLogin(config, cliAuthConfig{ServerURL: server.URL + "/api", Organization: "acme", APIKey: "fixture-key"}))
	file := filepath.Join(t.TempDir(), "resource-scopes.json")
	require.NoError(t, os.WriteFile(file, []byte(`[{"step":"extract","role":"query","scope":{"policy_id":"s3/relative-key/v1","data_source":"warehouse","operation":"read-orders","contract":"orders","revision":"v1","object_id":"rows","representation":"rows","s3":{"endpoint":"https://s3.example.test","region":"us-east-1","bucket":"archive","prefix":"orders","dynamic_key":false,"force_path_style":true}}}]`), 0o600))
	projectionFile := filepath.Join(t.TempDir(), "projection.json")
	require.NoError(t, os.WriteFile(projectionFile, []byte(`{"inputs":[{"contract_slug":"orders","revision_slug":"v1","schema_object_id":"row","digest":"sha256:exact","mapping":{"id":"mapping/v1","representation":"json","root_shape":"object","types":["object","integer","instant","vector","number"],"physical":{}},"variables":{"KIND":{"value":"string","revision":"variables-v1"}}}]}`), 0o600))
	var stdout, stderr bytes.Buffer
	err := runWithRuntime([]string{"braidplane", "pipeline", "version", "compile", "sync", identity, "--project", "etl", "--projection-file", projectionFile, "--resource-scopes-file", file}, &stdout, &stderr, cliRuntime{configPath: config})
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), `"digest": "sha256:retained"`)
}

func TestCompilePipelineVersionResourceScopesFileRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("invalid resource scopes file must not call the API")
	}))
	defer server.Close()
	config := filepath.Join(t.TempDir(), "braidplane.toml")
	require.NoError(t, saveLogin(config, cliAuthConfig{ServerURL: server.URL + "/api", Organization: "acme", APIKey: "fixture-key"}))
	for _, tc := range []struct {
		name     string
		contents []byte
		wantErr  string
	}{
		{name: "null", contents: []byte(`null`), wantErr: "must be an array"},
		{name: "invalid JSON", contents: []byte(`[{`), wantErr: "decode resource scopes"},
		{name: "unknown field", contents: []byte(`[{"unexpected":true}]`), wantErr: `unknown field "unexpected"`},
		{name: "oversized", contents: bytes.Repeat([]byte("x"), 1<<20+1), wantErr: "resource scopes exceed the 1 MiB limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "resource-scopes.json")
			require.NoError(t, os.WriteFile(file, tc.contents, 0o600))
			var stdout, stderr bytes.Buffer
			err := runWithRuntime([]string{"braidplane", "pipeline", "version", "compile", "sync", strings.Repeat("a", 64), "--project", "etl", "--resource-scopes-file", file}, &stdout, &stderr, cliRuntime{configPath: config})
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}
