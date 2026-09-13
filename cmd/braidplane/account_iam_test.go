package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountCommandsUsePATWithoutOrganizationScope(t *testing.T) {
	t.Setenv("BRAIDPLANE_API_KEY", "xto_api_key_must_not_be_used")

	var gotAuth string
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"slug":"user-1","email":"user@example.test","full_name":"User One",
			"locale":"en-US","mfa_email_enabled":false,"has_local_password":true,
			"role":"user","created_at":"2026-07-19T00:00:00Z","updated_at":"2026-07-19T00:00:00Z"
		}`))
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "user", "profile", "get", "--server-url", server.URL + "/api", "--pat", "xto_pat_test"},
		&stdout,
		&stderr,
		cliRuntime{configPath: filepath.Join(t.TempDir(), "missing.toml")},
	)

	require.NoError(t, err)
	require.Equal(t, "/api/users/profile", gotPath)
	require.Equal(t, "Bearer xto_pat_test", gotAuth)
	require.Contains(t, stdout.String(), `"email": "user@example.test"`)
	require.Empty(t, stderr.String())
}

func TestAccountAuthRejectsAPIKeyOnlyConfiguration(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, saveLogin(path, cliAuthConfig{
		ServerURL:    "https://api.example.test",
		Organization: "acme",
		APIKey:       "xto_api_key",
	}))

	_, err := (accountAuthFlags{}).resolveAuthConfig(&cliRuntime{configPath: path})

	require.Error(t, err)
	require.ErrorContains(t, err, "personal access token")
}

func TestUserProfileUpdateDoesNotExposeAtomicBillingAddressFields(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "user", "profile", "update", "--billing-city", "Warsaw"},
		&stdout,
		&stderr,
		cliRuntime{},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "unknown flag --billing-city")
}

// CLI auth resolution reads process environment variables.
func TestRoleCreateUsesTypedPermissionAndOrganizationScope(t *testing.T) {
	var gotAuth string
	var gotPath string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{
			"slug":"operators","name":"Operators","permissions":[{"resource":"members","action":"read"}],
			"is_default":false,"deleted_at":null,"created_at":"2026-07-19T00:00:00Z","updated_at":"2026-07-19T00:00:00Z"
		}`))
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane", "role", "create", "--server-url", server.URL + "/api", "--org", "acme",
			"--api-key", "xto_key_test", "--name", "Operators", "--slug", "operators",
			"--permission", "members:read",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: filepath.Join(t.TempDir(), "missing.toml")},
	)

	require.NoError(t, err)
	require.Equal(t, "/api/organizations/acme/roles", gotPath)
	require.Equal(t, "Bearer xto_key_test", gotAuth)
	require.JSONEq(t, `{"name":"Operators","slug":"operators","permissions":[{"resource":"members","action":"read"}]}`, string(gotBody))
	require.Contains(t, stdout.String(), `"slug": "operators"`)
	require.Empty(t, stderr.String())
}

func TestRolePermissionValidationRejectsUnknownAction(t *testing.T) {
	t.Parallel()

	_, err := (roleDefinitionFlags{Permissions: []string{"members:admin"}}).apiPermissions()

	require.Error(t, err)
	require.ErrorContains(t, err, "resource:action")
}

// CLI auth resolution reads process environment variables.
func TestInvitationCreateUsesTypedOrganizationRouteAndAPIKey(t *testing.T) {
	var gotAuth string
	var gotPath string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read invitation request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{
			"slug":"invite-1","organization_slug":"acme","email":"invitee@example.test",
			"role":"viewer","status":"pending","invited_by_user_slug":null,
			"expires_at":"2026-07-26T00:00:00Z","accepted_at":null,"declined_at":null,
			"revoked_at":null,"created_at":"2026-07-19T00:00:00Z","updated_at":"2026-07-19T00:00:00Z"
		}`))
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{
			"braidplane", "invitation", "create",
			"--server-url", server.URL + "/api",
			"--org", "acme",
			"--api-key", "xto_invitation_key",
			"--email", "invitee@example.test",
			"--role", "viewer",
		},
		&stdout,
		&stderr,
		cliRuntime{configPath: filepath.Join(t.TempDir(), "missing.toml")},
	)

	require.NoError(t, err)
	require.Equal(t, "/api/organizations/acme/member-invitations", gotPath)
	require.Equal(t, "Bearer xto_invitation_key", gotAuth)
	require.JSONEq(t, `{"email":"invitee@example.test","role":"viewer"}`, string(gotBody))
	require.Contains(t, stdout.String(), `"slug": "invite-1"`)
	require.NotContains(t, stdout.String(), "xto_invitation_key")
	require.Empty(t, stderr.String())
}

func TestInvitationMineRejectsAPIKeyBeforeRequest(t *testing.T) {
	t.Parallel()

	var serverCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		serverCalled = true
	}))
	t.Cleanup(server.Close)

	configPath := filepath.Join(t.TempDir(), "braidplane.toml")
	require.NoError(t, saveLogin(configPath, cliAuthConfig{
		ServerURL:    server.URL + "/api",
		Organization: "acme",
		APIKey:       "xto_api_key_must_not_be_used",
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithRuntime(
		[]string{"braidplane", "invitation", "mine", "list"},
		&stdout,
		&stderr,
		cliRuntime{configPath: configPath},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "personal access token")
	require.False(t, serverCalled)
	require.NotContains(t, err.Error(), "xto_api_key_must_not_be_used")
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())
}

func TestAccountIAMCommandHelpSurfaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"braidplane", "user", "profile", "--help"}, want: "user profile update"},
		{args: []string{"braidplane", "organization", "--help"}, want: "organization create"},
		{args: []string{"braidplane", "organization", "retention", "--help"}, want: "organization retention run list"},
		{args: []string{"braidplane", "member", "--help"}, want: "member remove"},
		{args: []string{"braidplane", "invitation", "--help"}, want: "invitation mine"},
		{args: []string{"braidplane", "invitation", "mine", "--help"}, want: "invitation mine accept"},
		{args: []string{"braidplane", "role", "--help"}, want: "role restore"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			err := runWithRuntime(tt.args, &stdout, &stderr, cliRuntime{})
			require.NoError(t, err)
			require.Contains(t, stdout.String(), tt.want)
			require.Empty(t, stderr.String())
		})
	}
}
