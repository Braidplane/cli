package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPipelineDraftRefreshProposal(t *testing.T) {
	for _, status := range []int{200, 400, 403, 404} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/api/organizations/acme/projects/data/pipelines/orders/draft:refresh-proposal", r.URL.Path)
				assert.Equal(t, "Bearer fixture-pat", r.Header.Get("Authorization"))
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				assert.Empty(t, body)
				assert.Empty(t, r.URL.RawQuery)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				if status == 200 {
					_, _ = io.WriteString(w, `{"slug":"orders","name":"Orders","has_draft":true}`)
				} else {
					_, _ = io.WriteString(w, `{"error":{"code":"VALIDATION_FAILED","message":"proposal unavailable"}}`)
				}
			}))
			defer server.Close()
			config := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, saveLogin(config, cliAuthConfig{ServerURL: server.URL + "/api", Organization: "acme", PAT: "fixture-pat", APIKey: "fixture-key"}))
			var out, stderr bytes.Buffer
			err := runWithRuntime([]string{"braidplane", "pipeline", "draft", "refresh-proposal", "orders", "--project", "data"}, &out, &stderr, cliRuntime{configPath: config})
			if status == 200 {
				require.NoError(t, err)
				require.Contains(t, out.String(), `"slug": "orders"`)
			} else {
				require.ErrorContains(t, err, "proposal unavailable")
				require.Empty(t, out.String())
			}
			require.Empty(t, stderr.String())
		})
	}
}

func TestPipelineDraftRefreshProposalRejectsAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("API-key request reached server") }))
	defer server.Close()
	config := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, saveLogin(config, cliAuthConfig{ServerURL: server.URL, Organization: "acme", APIKey: "fixture-key"}))
	var out, stderr bytes.Buffer
	err := runWithRuntime([]string{"braidplane", "pipeline", "draft", "refresh-proposal", "orders", "--project", "data"}, &out, &stderr, cliRuntime{configPath: config})
	require.ErrorContains(t, err, "personal access token")
	require.Empty(t, out.String())
}
