package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunWithArgsODCSContractCommandsUseProjectScopedRequests(t *testing.T) {
	documentPath := filepath.Join(t.TempDir(), "orders.odcs.json")
	require.NoError(t, os.WriteFile(documentPath, []byte(`{"apiVersion":"v3.2.0","id":"acme.orders"}`), 0o600))

	contract := `{"slug":"orders","name":"Orders","odcs_id":"acme.orders","status":"active","created_at":"2026-10-13T00:00:00Z","updated_at":"2026-10-13T00:00:00Z"}`
	revision := `{"slug":"v1","digest":"sha256:abc","document":{"apiVersion":"v3.2.0","id":"acme.orders","customProperties":[{"property":"token","value":"PRIVATE-MUST-NOT-RENDER"}]},"document_version":"v1","odcs_version":"v3.2.0","declared_status":"active","schema_objects":[{"id":"orders","name":"Orders"}],"status":"recorded","created_at":"2026-10-13T00:00:00Z"}`

	tests := []struct {
		name       string
		args       []string
		method     string
		target     string
		statusCode int
		response   string
		wantBody   string
		wantOutput string
	}{
		{"artifact preview", []string{"braidplane", "contract", "import", "--project", "data-pipelines"}, http.MethodPost, "/api/organizations/acme/projects/data-pipelines/contracts/import", http.StatusOK, `{"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","applied":false,"artifacts":[]}`, `{"apply":false,"expected_commit":""}`, `"applied": false`},
		{"artifact apply", []string{"braidplane", "contract", "import", "--project", "data-pipelines", "--apply", "--expected-commit", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, http.MethodPost, "/api/organizations/acme/projects/data-pipelines/contracts/import", http.StatusOK, `{"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","applied":true,"artifacts":[{"contract_slug":"orders","revision_slug":"v1","path":"contracts/orders.yaml","format":"yaml","digest":"sha256:abc","artifact_digest":"sha256:def","original":"UFJJVkFURS1NVVNULU5PVC1SRU5ERVI=","source_repo":"example.test/repo","commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`, `{"apply":true,"expected_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`, `"applied": true`},
		{"catalog list", []string{"braidplane", "contract", "list", "--project", "data-pipelines", "--limit", "10", "--cursor", "page-2", "--order", "asc", "--direction", "prev"}, http.MethodGet, "/api/organizations/acme/projects/data-pipelines/contracts?limit=10&cursor=page-2&order=asc&direction=prev", http.StatusOK, `{"contracts":[],"has_more":false}`, "", ""},
		{"catalog get", []string{"braidplane", "contract", "get", "orders", "--project", "data-pipelines"}, http.MethodGet, "/api/organizations/acme/projects/data-pipelines/contracts/orders", http.StatusOK, contract, "", ""},
		{"catalog create", []string{"braidplane", "contract", "create", "--project", "data-pipelines", "--name", "Orders", "--slug", "orders", "--odcs-id", "acme.orders"}, http.MethodPost, "/api/organizations/acme/projects/data-pipelines/contracts", http.StatusCreated, contract, `{"name":"Orders","slug":"orders","odcs_id":"acme.orders"}`, ""},
		{"catalog create generated slug", []string{"braidplane", "contract", "create", "--project", "data-pipelines", "--name", "Orders", "--odcs-id", "acme.orders"}, http.MethodPost, "/api/organizations/acme/projects/data-pipelines/contracts", http.StatusCreated, contract, `{"name":"Orders","odcs_id":"acme.orders"}`, ""},
		{"catalog update", []string{"braidplane", "contract", "update", "orders", "--project", "data-pipelines", "--name", "Orders archive", "--status", "archived"}, http.MethodPut, "/api/organizations/acme/projects/data-pipelines/contracts/orders", http.StatusOK, contract, `{"name":"Orders archive","status":"archived"}`, ""},
		{"revision list", []string{"braidplane", "contract", "revision", "list", "orders", "--project", "data-pipelines", "--limit", "10", "--cursor", "page-2", "--order", "desc", "--direction", "next"}, http.MethodGet, "/api/organizations/acme/projects/data-pipelines/contracts/orders/revisions?limit=10&cursor=page-2&order=desc&direction=next", http.StatusOK, `{"revisions":[],"has_more":false}`, "", ""},
		{"revision inspect", []string{"braidplane", "contract", "revision", "inspect", "orders", "v1", "--project", "data-pipelines", "--object", "order"}, http.MethodGet, "/api/organizations/acme/projects/data-pipelines/contracts/orders/revisions/v1/inspection?schema_object_id=order", http.StatusOK, `{"revision":{"slug":"v1","digest":"sha256:abc","document":{"description":"meaning retained"}},"compiler_id":"braidplane/odcs-flat-port/v2","schema_object_id":"order","document_valid":true,"coverage_complete":true,"projection_available":true,"coverage":[{"path":"$.quality","state":"unsupported","reason":"no_runtime_evaluator","applicability":"document"}],"diagnostics":[]}`, "", `"state": "unsupported"`},
		{"revision get", []string{"braidplane", "contract", "revision", "get", "orders", "v1", "--project", "data-pipelines"}, http.MethodGet, "/api/organizations/acme/projects/data-pipelines/contracts/orders/revisions/v1", http.StatusOK, revision, "", `"digest": "sha256:abc"`},
		{"revision create", []string{"braidplane", "contract", "revision", "create", "orders", "--project", "data-pipelines", "--slug", "v1", "--file", documentPath}, http.MethodPost, "/api/organizations/acme/projects/data-pipelines/contracts/orders/revisions", http.StatusCreated, revision, `{"slug":"v1","document":{"apiVersion":"v3.2.0","id":"acme.orders"}}`, `"digest": "sha256:abc"`},
		{"revision create generated slug", []string{"braidplane", "contract", "revision", "create", "orders", "--project", "data-pipelines", "--file", documentPath}, http.MethodPost, "/api/organizations/acme/projects/data-pipelines/contracts/orders/revisions", http.StatusCreated, revision, `{"document":{"apiVersion":"v3.2.0","id":"acme.orders"}}`, `"digest": "sha256:abc"`},
		{"revision impact", []string{"braidplane", "contract", "revision", "impact", "orders", "--project", "data-pipelines", "--from", "v1", "--to", "v2"}, http.MethodPost, "/api/organizations/acme/projects/data-pipelines/contracts/orders/impact", http.StatusOK, `{"contract_slug":"orders","project_slug":"data-pipelines","from_revision_slug":"v1","to_revision_slug":"v2","from_digest":"sha256:one","to_digest":"sha256:two","objects":[],"consumers":[],"external_consumers":"unknown"}`, `{"from_revision_slug":"v1","to_revision_slug":"v2"}`, `"external_consumers": "unknown"`},
		{"rich revision impact", []string{"braidplane", "contract", "revision", "impact", "orders", "--project", "data-pipelines", "--from", "v1", "--to", "v2", "--from-pipeline", "sync", "--from-plan", "sha256:old", "--to-pipeline", "sync", "--to-plan", "sha256:new", "--from-projection-input", "0", "--to-projection-input", "1"}, http.MethodPost, "/api/organizations/acme/projects/data-pipelines/contracts/orders/impact", http.StatusOK, `{"contract_slug":"orders","project_slug":"data-pipelines","from_revision_slug":"v1","to_revision_slug":"v2","objects":[{"schema_object_id":"row","comparison":"supported","comparison_scope":"selected_projection","changes":[{"kind":"data_path_changed","severity":"breaking","reason_code":"structural_data_path_changed","path":"$[\"renamed\"]","before":"$[\"id\"]","after":"$[\"renamed\"]"}],"diagnostics":[]}],"consumers":[],"external_consumers":"unknown"}`, `{"from_revision_slug":"v1","to_revision_slug":"v2","from_plan":{"pipeline_slug":"sync","digest":"sha256:old","projection_input":0},"to_plan":{"pipeline_slug":"sync","digest":"sha256:new","projection_input":1}}`, `"comparison_scope": "selected_projection"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotMethod, gotTarget, gotAuth string
			var gotBody []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotTarget, gotAuth = r.Method, r.URL.RequestURI(), r.Header.Get("Authorization")
				var err error
				gotBody, err = io.ReadAll(r.Body)
				assert.NoError(t, err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			t.Cleanup(server.Close)

			stdout, stderr, err := runCLIForEnvironmentTest(t, server.URL, tt.args)
			require.NoError(t, err)
			require.Empty(t, stderr)
			require.Equal(t, tt.method, gotMethod)
			require.Equal(t, tt.target, gotTarget)
			require.Equal(t, "Bearer xto_test_token", gotAuth)
			require.NotEmpty(t, stdout)
			require.NotContains(t, stdout, "PRIVATE-MUST-NOT-RENDER")
			if tt.name == "revision inspect" {
				require.Contains(t, stdout, "meaning retained")
			} else {
				require.NotContains(t, stdout, `"document":`)
			}
			require.NotContains(t, stdout, "UFJJVkFURS1NVVNULU5PVC1SRU5ERVI=")
			if tt.wantOutput != "" {
				require.Contains(t, stdout, tt.wantOutput)
			}
			if tt.wantBody == "" {
				require.Empty(t, gotBody)
			} else {
				require.JSONEq(t, tt.wantBody, string(gotBody))
			}
		})
	}
}

func TestRunWithArgsODCSContractCommandsPreserveAPIErrors(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/organizations/acme/projects/data-pipelines/contracts/orders", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"FORBIDDEN","message":"contract read denied"}}`))
	}))
	t.Cleanup(server.Close)
	require.NoError(t, saveLogin(configPath, cliAuthConfig{ServerURL: server.URL + "/api", Organization: "acme", APIKey: "xto_saved_token"}))

	var stdout, stderr bytes.Buffer
	err := runWithRuntime([]string{"braidplane", "contract", "get", "orders", "--project", "data-pipelines"}, &stdout, &stderr, cliRuntime{configPath: configPath})
	require.ErrorContains(t, err, "403")
	require.ErrorContains(t, err, "FORBIDDEN")
	require.ErrorContains(t, err, "contract read denied")
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())
}

func TestODCSContractCommandsRejectInvalidInputBeforeRequests(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"inspection rejects latest", []string{"braidplane", "contract", "revision", "inspect", "orders", "latest", "--project", "data-pipelines", "--server-url", "http://example.invalid", "--org", "acme", "--api-key", "token"}, "select an exact revision"},
		{"import requires commit", []string{"braidplane", "contract", "import", "--apply", "--project", "data-pipelines", "--server-url", "http://example.invalid", "--org", "acme", "--api-key", "token"}, "--apply requires --expected-commit from preview"},
		{"contract slug", []string{"braidplane", "contract", "get", "UPPER", "--project", "data-pipelines", "--server-url", "http://example.invalid", "--org", "acme", "--api-key", "token"}, "contract slug must be a valid slug"},
		{"contract create slug", []string{"braidplane", "contract", "create", "--project", "data-pipelines", "--name", "Orders", "--slug", "bad/slug", "--odcs-id", "acme.orders", "--server-url", "http://example.invalid", "--org", "acme", "--api-key", "token"}, "slug must be a valid slug"},
		{"revision create slug", []string{"braidplane", "contract", "revision", "create", "orders", "--project", "data-pipelines", "--slug", "bad/slug", "--file", filepath.Join(t.TempDir(), "missing.json"), "--server-url", "http://example.invalid", "--org", "acme", "--api-key", "token"}, "slug must be a valid slug"},
		{"revision document", []string{"braidplane", "contract", "revision", "create", "orders", "--project", "data-pipelines", "--slug", "v1", "--file", filepath.Join(t.TempDir(), "missing.json"), "--server-url", "http://example.invalid", "--org", "acme", "--api-key", "token"}, "read ODCS JSON document"},
		{"order", []string{"braidplane", "contract", "list", "--project", "data-pipelines", "--order", "sideways", "--server-url", "http://example.invalid", "--org", "acme", "--api-key", "token"}, "order must be asc or desc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := runWithArgs(tt.args, &stdout, &stderr)
			require.Error(t, err)
			require.ErrorContains(t, err, tt.want)
			require.Empty(t, stdout.String())
		})
	}
}

func TestODCSInspectionDenialDoesNotPrintDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/organizations/acme/projects/etl/contracts/orders/revisions/v1/inspection", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"FORBIDDEN","message":"project read denied"}}`))
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	err := runWithArgs([]string{"braidplane", "contract", "revision", "inspect", "orders", "v1", "--project", "etl", "--server-url", server.URL + "/api", "--org", "acme", "--api-key", "test-token"}, &stdout, &stderr)
	require.ErrorContains(t, err, "403")
	require.Empty(t, stdout.String())
}

func TestODCSInspectionShapeExportPreservesExactConstraints(t *testing.T) {
	for _, available := range []bool{true, false} {
		t.Run(fmt.Sprint(available), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "product", r.URL.Query().Get("schema_object_id"))
				w.Header().Set("Content-Type", "application/json")
				body := `{"revision":{"slug":"v1","digest":"sha256:exact","document":{}},"projection_available":false}`
				if available {
					body = `{"revision":{"slug":"v1","digest":"sha256:exact","document":{}},"schema_object_id":"product","document_valid":true,"projection_available":true,"shape":{"name":"product","fields":[{"name":"price","type":"decimal","precision":20,"scale":2,"required":true,"validations":{"min":9007199254740993.01}}]}}`
				}
				_, err := w.Write([]byte(body))
				assert.NoError(t, err)
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			err := runWithArgs([]string{"braidplane", "contract", "revision", "inspect", "products", "v1", "--object", "product", "--shape-only", "--project", "catalog", "--server-url", server.URL + "/api", "--org", "acme", "--api-key", "fixture"}, &stdout, &stderr)
			if available {
				require.NoError(t, err)
				require.Contains(t, stdout.String(), "9007199254740993.01")
				require.NotContains(t, stdout.String(), "revision")
			} else {
				require.Error(t, err)
				require.Empty(t, stdout.String())
			}
		})
	}
}
