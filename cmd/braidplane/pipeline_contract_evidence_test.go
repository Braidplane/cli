package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPipelineRunContractEvidenceTextAndJSON(t *testing.T) {
	run := &api.PipelineRunRecord{Timeline: api.PipelineRunTimeline{
		DeploymentResolutionDigest: new("sha256:historical"),
		StructuralChecks:           &[]api.PipelineRunStructuralCheck{{Id: "check", Step: "merge", Input: new("orders"), Origin: "operation", Phase: "normalized_input", Status: "skipped", ReasonCode: "not_reached", Declaration: "operation/pre_sink", AttemptNumber: 2, CompiledPlanDigest: new("sha256:executed-plan")}},
	}}
	var out bytes.Buffer
	require.NoError(t, writePipelineRunOutput(&out, run, "text"))
	for _, expected := range []string{"sha256:historical", "sha256:executed-plan", "origin=operation", "Input: orders", "status=skipped", "attempt=2", "conformance", "unverified without an evaluator"} {
		require.Contains(t, out.String(), expected)
	}
	out.Reset()
	require.NoError(t, writePipelineRunOutput(&out, run, "json"))
	require.Contains(t, out.String(), `"deployment_resolution_digest": "sha256:historical"`)
	require.Contains(t, out.String(), `"status": "skipped"`)
	out.Reset()
	require.NoError(t, writePipelineRunOutput(&out, &api.PipelineRunRecord{}, "text"))
	require.Contains(t, out.String(), "no recorded evidence; conformance unverified")
}

func TestPipelineRunGetPreexecutionTransformFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/organizations/acme/projects/etl/environments/dev/runs/rejected", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metadata":{"run_slug":"rejected","pipeline_slug":"orders","status":"failed"},"timeline":{"failure":{"step":"project-orders","kind":"platform","side":"transform","reason_code":"transform_contract_mismatch","error":"PIPELINE_TRANSFORM_CONTRACT_TYPE_MISMATCH: transform output contract type mismatch","action_hint":"Run server-backed pipeline validation before retrying."}}}`))
	}))
	t.Cleanup(server.Close)
	t.Setenv("BRAIDPLANE_SERVER_URL", server.URL+"/api")
	t.Setenv("BRAIDPLANE_ORG", "acme")
	t.Setenv("BRAIDPLANE_API_KEY", "xto_local_fixture")
	t.Setenv("BRAIDPLANE_ENVIRONMENT", "dev")
	for _, format := range []string{"text", "json"} {
		var stdout, stderr bytes.Buffer
		err := runWithRuntime([]string{"braidplane", "pipeline", "run", "get", "rejected", "--project", "etl", "--output", format}, &stdout, &stderr, cliRuntime{configPath: filepath.Join(t.TempDir(), "braidplane.toml")})
		require.NoError(t, err)
		require.Empty(t, stderr.String())
		for _, expected := range []string{"project-orders", "transform_contract_mismatch", "PIPELINE_TRANSFORM_CONTRACT_TYPE_MISMATCH", "Run server-backed pipeline validation before retrying."} {
			require.Contains(t, stdout.String(), expected)
		}
	}
}
