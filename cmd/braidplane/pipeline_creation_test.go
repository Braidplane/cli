package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/slug"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPipelineCreateProposesDefinitionWithUserCredentials(t *testing.T) {
	file := filepath.Join(t.TempDir(), "first.pipeline.json")
	require.NoError(t, os.WriteFile(file, []byte(`{"metadata":{"slug":"first","name":"First read"},"steps":[{"kind":"query","slug":"fetch","query":{"data_source_ref":"source","operation_type":"sql","operation_ref":"read"}}]}`), 0o600))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/organizations/acme/projects/project/pipeline-creations", r.URL.Path)
		assert.Equal(t, "Bearer xto_pat_fixture", r.Header.Get("Authorization"))
		var spec api.PipelineSpec
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&spec))
		assert.Equal(t, "first", spec.Metadata.Slug)
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(api.PipelineCreationProposal{Slug: "creation-first", ProjectSlug: "project", PipelineSlug: "first", InitiatingMember: "alice", SourcePath: "pipelines/first.pipeline.yaml", SpecHash: "proposed-hash", Status: api.CreationPublishQueued, Spec: spec}))
	}))
	defer server.Close()
	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_PAT", "xto_pat_fixture")
	var stdout, stderr bytes.Buffer
	err := runWithRuntime([]string{"braidplane", "pipeline", "create", "--project", "project", "--file", file}, &stdout, &stderr, cliRuntime{configPath: filepath.Join(t.TempDir(), "config.toml")})
	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Contains(t, stdout.String(), `"slug": "creation-first"`)
	require.Contains(t, stdout.String(), `"status": "publish_queued"`)
	require.NotContains(t, stdout.String(), "xto_pat_fixture")
}

func TestPipelineCreateRequiresOneDefinitionSource(t *testing.T) {
	for _, command := range []createPipelineCommand{{}, {File: "definition.yaml", FromQuery: "read-users"}} {
		require.ErrorContains(t, command.Run(&cliOutput{stdout: io.Discard, stderr: io.Discard}, &cliRuntime{}), "choose exactly one")
	}
	command := createPipelineCommand{File: "definition.yaml", DataSource: "warehouse"}
	require.ErrorContains(t, command.Run(&cliOutput{stdout: io.Discard, stderr: io.Discard}, &cliRuntime{}), "apply only to --from-query")
}

func TestPipelineCreationListKeepsContinuationAndProject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/organizations/acme/projects/project/pipeline-creations", r.URL.Path)
		assert.Equal(t, "creation-before", r.URL.Query().Get("cursor"))
		assert.Equal(t, "1", r.URL.Query().Get("limit"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pipeline_creations":[],"has_more":true,"next_cursor":"creation-next"}`))
	}))
	defer server.Close()
	stdout, _, err := runCLIForEnvironmentTest(t, server.URL, []string{"braidplane", "pipeline", "creation", "list", "--project", "project", "--limit", "1", "--cursor", "creation-before"})
	require.NoError(t, err)
	require.Contains(t, stdout, `"next_cursor": "creation-next"`)
	for _, limit := range []int32{0, -1, 101} {
		command := listPipelineCreationsCommand{Limit: limit}
		require.ErrorContains(t, command.Run(&cliOutput{stdout: io.Discard}, &cliRuntime{}), "between 1 and 100")
	}
}

func TestPipelineCreateFromSavedQueryWithoutLocalFile(t *testing.T) {
	t.Setenv("BRAIDPLANE_PAT", "xto_pat_fixture")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer xto_pat_fixture", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/organizations/acme/data-sources/warehouse/queries/read-users":
			assert.Equal(t, http.MethodGet, r.Method)
			_, _ = io.WriteString(w, `{"slug":"read-users","name":"Read users","data_source_slug":"warehouse","parameters":[{"name":"limit","data_type":"number","is_required":true}],"query_statement":{"type":"postgres","sql":"SELECT id FROM users LIMIT {{limit}}"}}`)
		case "/api/organizations/acme/projects/project/pipeline-creations":
			assert.Equal(t, http.MethodPost, r.Method)
			raw, err := io.ReadAll(r.Body)
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			assert.Contains(t, string(raw), "9007199254740993")
			var spec api.PipelineSpec
			if !assert.NoError(t, json.Unmarshal(raw, &spec)) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			assert.Equal(t, "read-users-pipeline", spec.Metadata.Slug)
			step, err := spec.Steps[0].AsPipelineQueryStep()
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			assert.Equal(t, "source", step.Query.DataSourceRef)
			assert.Equal(t, "sql", string(step.Query.OperationType))
			assert.Equal(t, "read-users", *step.Query.OperationRef)
			assert.NoError(t, json.NewEncoder(w).Encode(api.PipelineCreationProposal{Slug: "creation-first", ProjectSlug: "project", PipelineSlug: spec.Metadata.Slug, InitiatingMember: "alice", SourcePath: "pipelines/read-users-pipeline.pipeline.yaml", SpecHash: "proposed-hash", Status: api.CreationPublishQueued, Spec: spec}))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	args := []string{"braidplane", "pipeline", "create", "--project", "project", "--from-query", "read-users", "--data-source", "warehouse", "--parameters-json", `{"limit":9007199254740993}`}
	stdout, stderr, err := runCLIForEnvironmentTest(t, server.URL, args)
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, `"status": "publish_queued"`)
	_, _, err = runCLIForEnvironmentTest(t, server.URL, args[:len(args)-2])
	require.ErrorContains(t, err, "supply required query parameter")
	_, _, err = runCLIForEnvironmentTest(t, server.URL, append(append([]string(nil), args...), "--slug", "Invalid Slug"))
	require.ErrorIs(t, err, slug.ErrInvalidSlug)
	for _, value := range []string{`[]`, `{"limit":5} true`} {
		invalid := append([]string(nil), args...)
		invalid[len(invalid)-1] = value
		_, _, err = runCLIForEnvironmentTest(t, server.URL, invalid)
		require.ErrorContains(t, err, "must be a JSON object")
	}
}

func TestPipelineCreationRetryUsesRecordedSpecWithoutReloadingQuery(t *testing.T) {
	t.Setenv("BRAIDPLANE_PAT", "xto_pat_fixture")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer xto_pat_fixture", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /api/organizations/acme/projects/project/pipeline-creations/creation-first":
			_, _ = io.WriteString(w, `{"slug":"creation-first","project_slug":"project","pipeline_slug":"first","initiating_member":"alice","source_path":"pipelines/first.pipeline.yaml","spec_hash":"proposed-hash","status":"publish_failed","spec":{"metadata":{"slug":"first","name":"Original read"},"steps":[{"kind":"query","slug":"fetch","query":{"data_source_ref":"source","operation_type":"sql","operation_ref":"deleted-query","parameters":{"limit":9007199254740993}}}]}}`)
		case "POST /api/organizations/acme/projects/project/pipeline-creations":
			body, err := io.ReadAll(r.Body)
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			assert.Contains(t, string(body), `"name":"Original read"`)
			assert.Contains(t, string(body), `"operation_ref":"deleted-query"`)
			assert.Contains(t, string(body), "9007199254740993")
			var spec api.PipelineSpec
			if !assert.NoError(t, json.Unmarshal(body, &spec)) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			assert.NoError(t, json.NewEncoder(w).Encode(api.PipelineCreationProposal{Slug: "creation-first", ProjectSlug: "project", PipelineSlug: "first", InitiatingMember: "alice", SourcePath: "pipelines/first.pipeline.yaml", SpecHash: "proposed-hash", Status: api.CreationPublishQueued, Spec: spec}))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	stdout, _, err := runCLIForEnvironmentTest(t, server.URL, []string{"braidplane", "pipeline", "creation", "retry", "creation-first", "--project", "project"})
	require.NoError(t, err)
	require.Contains(t, stdout, `"slug": "creation-first"`)
	require.Contains(t, stdout, `"status": "publish_queued"`)
}

func TestPipelineCreationGetInspectsExactPendingProposal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/organizations/acme/projects/project/pipeline-creations/creation-first", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"slug":"creation-first","project_slug":"project","pipeline_slug":"first","initiating_member":"alice","source_path":"pipelines/first.pipeline.yaml","spec_hash":"proposed-hash","status":"proposal_open","proposal_url":"https://github.com/customer/repository/pull/42","spec":{"metadata":{"slug":"first","name":"Original definition"},"steps":[{"kind":"query","slug":"fetch","query":{"data_source_ref":"source","operation_type":"sql","operation_ref":"read"}}]}}`)
	}))
	defer server.Close()
	stdout, stderr, err := runCLIForEnvironmentTest(t, server.URL, []string{"braidplane", "pipeline", "creation", "get", "creation-first", "--project", "project"})
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, `"status": "proposal_open"`)
	require.Contains(t, stdout, "pull/42")
	require.Contains(t, stdout, `"name": "Original definition"`)
}
