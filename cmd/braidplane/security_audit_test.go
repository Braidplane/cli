package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecurityAuditRequestsAndOutput(t *testing.T) {
	for _, command := range []string{"list", "export"} {
		t.Run(command, func(t *testing.T) {
			const csv = "slug,metadata\r\nevt-1,\"a,b\"\r\n"
			var query url.Values
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "Bearer fixture-key", r.Header.Get("Authorization"))
				path := "/api/organizations/acme/security-audit-events"
				if command == "export" {
					path += "/export"
				}
				assert.Equal(t, path, r.URL.Path)
				query = r.URL.Query()
				if command == "export" {
					w.Header().Set("Content-Type", "text/csv")
					_, _ = io.WriteString(w, csv)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"events":[],"has_more":true,"next_cursor":"next-page","prev_cursor":null}`)
			}))
			defer server.Close()
			config := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, saveLogin(config, cliAuthConfig{ServerURL: server.URL + "/api", Organization: "acme", APIKey: "fixture-key"}))
			args := []string{"braidplane", "security-audit", command, "--actor-type", "user", "--actor-slug", "alice", "--entity-type", "pipeline", "--entity-slug", "orders", "--action", "update", "--request-id", "req-123", "--occurred-from", "2026-10-01T00:00:00Z", "--occurred-before", "2026-10-02T00:00:00Z"}
			want := url.Values{"actor_type": {"user"}, "actor_slug": {"alice"}, "entity_type": {"pipeline"}, "entity_slug": {"orders"}, "action": {"update"}, "request_id": {"req-123"}, "occurred_from": {"2026-10-01T00:00:00Z"}, "occurred_before": {"2026-10-02T00:00:00Z"}}
			if command == "list" {
				args = append(args, "--limit", "2", "--cursor", "page-token", "--direction", "prev", "--sort-by", "event_occurred_at", "--order", "asc")
				want.Set("limit", "2")
				want.Set("cursor", "page-token")
				want.Set("direction", "prev")
				want.Set("sort_by", "event_occurred_at")
				want.Set("order", "asc")
			}
			var out, stderr bytes.Buffer
			require.NoError(t, runWithRuntime(args, &out, &stderr, cliRuntime{configPath: config}))
			require.Equal(t, want, query)
			require.Empty(t, stderr.String())
			if command == "export" {
				require.Equal(t, csv, out.String())
			} else {
				require.JSONEq(t, `{"events":[],"has_more":true,"next_cursor":"next-page"}`, out.String())
			}
		})
	}
}

func TestSecurityAuditInvalidFlags(t *testing.T) {
	cases := [][]string{{"--actor-type", "robot"}, {"--entity-type", "workspace"}, {"--action", "nope"}, {"--actor-slug", "bad slug"}, {"--entity-slug", "bad slug"}, {"--request-id", ""}, {"--request-id", strings.Repeat("x", 256)}, {"--occurred-from", "yesterday"}, {"--occurred-before", "tomorrow"}, {"--occurred-from", "2026-10-02T00:00:00Z", "--occurred-before", "2026-10-02T00:00:00Z"}, {"--limit", "0"}, {"--limit", "101"}, {"--direction", "back"}, {"--order", "latest"}, {"--sort-by", "created_at"}}
	for _, flags := range cases {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("invalid request reached API") }))
			defer server.Close()
			config := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, saveLogin(config, cliAuthConfig{ServerURL: server.URL, Organization: "acme", APIKey: "fixture-key"}))
			for _, cmd := range []string{"list", "export"} {
				args := append([]string{"braidplane", "security-audit", cmd}, flags...)
				var out, stderr bytes.Buffer
				require.Error(t, runWithRuntime(args, &out, &stderr, cliRuntime{configPath: config}))
				require.Empty(t, out.String())
			}
		})
	}
}

func TestSecurityAuditAPIErrorsAndDefaults(t *testing.T) {
	for _, cmd := range []string{"list", "export"} {
		for _, status := range []int{200, 400, 403} {
			t.Run(cmd+http.StatusText(status), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Empty(t, r.URL.RawQuery)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(status)
					if status == 200 {
						_, _ = io.WriteString(w, `{"events":[],"has_more":false}`)
					} else {
						_, _ = io.WriteString(w, `{"error":{"code":"VALIDATION_FAILED","message":"narrow occurrence window"}}`)
					}
				}))
				defer server.Close()
				config := filepath.Join(t.TempDir(), "config.toml")
				require.NoError(t, saveLogin(config, cliAuthConfig{ServerURL: server.URL, Organization: "acme", PAT: "fixture-pat"}))
				var out, stderr bytes.Buffer
				err := runWithRuntime([]string{"braidplane", "security-audit", cmd}, &out, &stderr, cliRuntime{configPath: config})
				if status == 200 {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, "narrow occurrence window")
					require.Empty(t, out.String())
				}
			})
		}
	}
}

type failingAuditWriter struct{}

func (failingAuditWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestSecurityAuditExportWriteError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		_, _ = io.WriteString(w, "slug\n")
	}))
	defer server.Close()
	config := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, saveLogin(config, cliAuthConfig{ServerURL: server.URL, Organization: "acme", APIKey: "fixture-key"}))
	var stderr bytes.Buffer
	require.ErrorContains(t, runWithRuntime([]string{"braidplane", "security-audit", "export"}, failingAuditWriter{}, &stderr, cliRuntime{configPath: config}), "closed pipe")
}
