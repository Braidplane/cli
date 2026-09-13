package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
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
