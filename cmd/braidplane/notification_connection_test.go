package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	notificationConnectionPayload = `{"slug":"ops-slack","name":"Operations Slack","provider":"slack","status":"active","metadata":{"workspace_id":"TTEST","workspace_name":"Synthetic workspace","channel_id":"CTEST","channel_name":"#operations"},"updated_at":"2026-10-19T10:00:00Z"}`
	slackInstallPayload           = `{"slug":"install-slack","provider":"slack","authorization_url":"https://slack.test/oauth/v2/authorize?state=synthetic-consent-state","expires_at":"2026-10-19T10:10:00Z","intent":"create_grant","requested_scopes":["incoming-webhook"]}`
	slackDestinationPayload       = `{"slug":"ops-alerts","name":"Operations alerts","type":"slack","non_secret_config":{"connection_slug":"ops-slack"},"enabled":true,"created_at":"2026-10-19T10:00:00Z","updated_at":"2026-10-19T10:00:00Z"}`
)

func TestNotificationConnectionCommands(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		method, path   string
		status         int
		response, body string
		consent        bool
	}{
		{name: "ListStatus", args: []string{"connection", "list"}, method: "GET", path: "/notification-connections", status: 200, response: "[" + notificationConnectionPayload + "]"},
		{name: "EmptyList", args: []string{"connection", "list"}, method: "GET", path: "/notification-connections", status: 200, response: "[]"},
		{name: "Create", args: []string{"connection", "create", "--slug", "ops-slack", "--name", "Operations Slack"}, method: "POST", path: "/notification-connections", status: 201, response: notificationConnectionPayload, body: `{"slug":"ops-slack","name":"Operations Slack","provider":"slack"}`},
		{name: "CreateGeneratedSlug", args: []string{"connection", "create", "--name", "Operations Slack"}, method: "POST", path: "/notification-connections", status: 201, response: notificationConnectionPayload, body: `{"name":"Operations Slack","provider":"slack"}`},
		{name: "Connect", args: []string{"connection", "connect", "ops-slack"}, method: "POST", path: "/notification-connections/ops-slack/connect", status: 201, response: slackInstallPayload, consent: true},
		{name: "Reconnect", args: []string{"connection", "reconnect", "ops-slack"}, method: "POST", path: "/notification-connections/ops-slack/connect", status: 201, response: slackInstallPayload, consent: true},
		{name: "Disconnect", args: []string{"connection", "disconnect", "ops-slack"}, method: "POST", path: "/notification-connections/ops-slack/disconnect", status: 200, response: `{"kind":"oauth2_grant","status":"revoked","updated_at":"2026-10-19T10:00:00Z"}`},
		{name: "SlackDestinationCreate", args: []string{"destination", "create", "--type", "slack", "--name", "Operations alerts", "--connection-slug", "ops-slack"}, method: "POST", path: "/alert-destinations", status: 201, response: slackDestinationPayload, body: `{"name":"Operations alerts","type":"slack","non_secret_config":{"connection_slug":"ops-slack"}}`},
		{name: "SlackDestinationUpdate", args: []string{"destination", "update", "ops-alerts", "--type", "slack", "--name", "Operations alerts", "--connection-slug", "replacement-slack", "--disabled"}, method: "PUT", path: "/alert-destinations/ops-alerts", status: 200, response: slackDestinationPayload, body: `{"name":"Operations alerts","type":"slack","non_secret_config":{"connection_slug":"replacement-slack"},"enabled":false}`},
		{name: "SlackDestinationTest", args: []string{"destination", "test", "ops-alerts"}, method: "POST", path: "/alert-destinations/ops-alerts/test", status: 202, response: `{"status":"accepted","destination_slug":"ops-alerts","destination_type":"slack","recipient_count":1,"diagnostics_persisted":false,"enqueued_jobs":[{"kind":"notification_send_test","queue":"notifications","state":"queued"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var method, path, auth string
			var body []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				method, path, auth = r.Method, r.URL.RequestURI(), r.Header.Get("Authorization")
				var err error
				body, err = io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read request: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.response)
			}))
			t.Cleanup(server.Close)
			args := append([]string{"braidplane", "alert"}, tt.args...)
			stdout, stderr, err := runCLIForAlertTest(t, server.URL, args)
			require.NoError(t, err)
			require.Equal(t, tt.method, method)
			require.Equal(t, "/api/organizations/acme"+tt.path, path)
			require.Equal(t, "Bearer xto_test_token", auth)
			if tt.body != "" {
				require.JSONEq(t, tt.body, string(body))
			} else {
				require.Empty(t, body)
			}
			require.JSONEq(t, tt.response, stdout)
			if tt.consent {
				require.Contains(t, stderr, "only starts authorization")
				require.Contains(t, stderr, "alert connection list")
				require.NotContains(t, stderr, "synthetic-consent-state")
			} else {
				require.Empty(t, stderr)
				require.NotContains(t, stdout, "authorization_url")
			}
			require.NotContains(t, stdout, "webhook_url")
			require.NotContains(t, stdout, "access_token")
		})
	}
}

func TestNotificationConnectionCommandValidation(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		detail string
	}{
		{"InvalidConnectionSlug", []string{"connection", "connect", "../other"}, "invalid slug"},
		{"InvalidDisconnectSlug", []string{"connection", "disconnect", "bad/slug"}, "invalid slug"},
		{"BlankName", []string{"connection", "create", "--name", " ", "--slug", "ops-slack"}, "set --name"},
		{"InvalidCreateSlug", []string{"connection", "create", "--name", "Ops", "--slug", "bad/slug"}, "invalid slug"},
		{"UnsupportedProvider", []string{"connection", "create", "--name", "Ops", "--slug", "ops", "--provider", "other"}, "slack"},
		{"SlackNeedsConnection", []string{"destination", "create", "--name", "Ops", "--type", "slack"}, "set --connection-slug"},
		{"SlackRejectsRecipients", []string{"destination", "create", "--name", "Ops", "--type", "slack", "--connection-slug", "ops-slack", "--recipient", "ops@example.com"}, "--recipient is only supported"},
		{"EmailRejectsConnection", []string{"destination", "create", "--name", "Ops", "--connection-slug", "ops-slack", "--recipient", "ops@example.com"}, "--connection-slug is only supported"},
		{"EmailNeedsRecipients", []string{"destination", "create", "--name", "Ops"}, "set --recipient"},
		{"SlackUpdateNeedsConnection", []string{"destination", "update", "ops", "--name", "Ops", "--type", "slack", "--enabled"}, "set --connection-slug"},
		{"SlackUpdateNeedsEnabledChoice", []string{"destination", "update", "ops", "--name", "Ops", "--type", "slack", "--connection-slug", "ops-slack"}, "set --enabled or --disabled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				t.Error("invalid command contacted API")
				w.WriteHeader(500)
			}))
			t.Cleanup(server.Close)
			stdout, _, err := runCLIForAlertTest(t, server.URL, append([]string{"braidplane", "alert"}, tt.args...))
			require.ErrorContains(t, err, tt.detail)
			require.Empty(t, stdout)
		})
	}
}

func TestNotificationConnectionAPIFailures(t *testing.T) {
	commands := [][]string{{"list"}, {"create", "--name", "Ops", "--slug", "ops"}, {"connect", "ops"}, {"disconnect", "ops"}}
	for _, args := range commands {
		t.Run(args[0], func(t *testing.T) {
			for _, status := range []int{401, 403, 404, 409, 503} {
				t.Run(http.StatusText(status), func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(status)
						_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "CONNECTION_UNAVAILABLE", "message": "connection unavailable"}})
					}))
					t.Cleanup(server.Close)
					stdout, stderr, err := runCLIForAlertTest(t, server.URL, append([]string{"braidplane", "alert", "connection"}, args...))
					require.ErrorContains(t, err, "CONNECTION_UNAVAILABLE")
					require.ErrorContains(t, err, "connection unavailable")
					require.Empty(t, stdout)
					require.Empty(t, stderr)
				})
			}
		})
	}
}

func TestNotificationConnectionMissingResponsePayload(t *testing.T) {
	for _, args := range [][]string{{"list"}, {"create", "--name", "Ops", "--slug", "ops"}, {"connect", "ops"}, {"disconnect", "ops"}} {
		t.Run(args[0], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				// An unexpected content type must not be mistaken for authorization success.
				w.Header().Set("Content-Type", "text/html")
				if args[0] == "create" || args[0] == "connect" {
					w.WriteHeader(201)
				}
				_, _ = io.WriteString(w, "<html>upstream unavailable</html>")
			}))
			t.Cleanup(server.Close)
			stdout, stderr, err := runCLIForAlertTest(t, server.URL, append([]string{"braidplane", "alert", "connection"}, args...))
			require.ErrorContains(t, err, "unexpected API response: missing")
			require.Empty(t, stdout)
			require.Empty(t, stderr)
		})
	}
}

func TestNotificationConnectionHelp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { t.Error("help contacted API"); w.WriteHeader(500) }))
	t.Cleanup(server.Close)
	stdout, stderr, err := runCLIForAlertTest(t, server.URL, []string{"braidplane", "alert", "connection", "--help"})
	require.NoError(t, err)
	require.Empty(t, stderr)
	for _, command := range []string{"list", "create", "connect", "reconnect", "disconnect"} {
		require.Contains(t, stdout, command)
	}
}
