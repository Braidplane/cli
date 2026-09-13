package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	api "github.com/Braidplane/cli/internal/apiclient"
)

func TestNewCLIAPIClientInjectsBearerTokenIntoGeneratedClient(t *testing.T) {
	t.Parallel()

	var gotAuth string
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"organizations":[],"next_cursor":null}`))
	}))
	t.Cleanup(server.Close)

	client, err := newCLIAPIClient(cliAuthConfig{
		ServerURL:    server.URL,
		Organization: "acme",
		APIKey:       "xto_test_secret",
	})
	require.NoError(t, err)

	resp, err := client.openapi.ListOrganizationsWithResponse(
		context.Background(),
		&api.ListOrganizationsParams{},
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode())
	require.Equal(t, "/organizations", gotPath)
	require.Equal(t, "Bearer xto_test_secret", gotAuth)
	require.Equal(t, "acme", client.organization)
}

func TestNewCLIAPIClientConfiguresRequestTimeout(t *testing.T) {
	t.Parallel()

	client, err := newCLIAPIClient(cliAuthConfig{
		ServerURL:    "https://api.example.test",
		Organization: "acme",
		APIKey:       "xto_test_secret",
	})
	require.NoError(t, err)

	openapiClient, ok := client.openapi.ClientInterface.(*api.Client)
	require.True(t, ok)
	httpClient, ok := openapiClient.Client.(*http.Client)
	require.True(t, ok)
	require.Equal(t, cliAPIClientTimeout, httpClient.Timeout)
}

func TestNormalizeAPIServerURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "root server URL",
			raw:  "http://localhost:3001",
			want: "http://localhost:3001",
		},
		{
			name: "already API base URL",
			raw:  "http://localhost:3001/api",
			want: "http://localhost:3001/api",
		},
		{
			name: "nested root server URL",
			raw:  "https://example.test/braidplane",
			want: "https://example.test/braidplane",
		},
		{
			name: "trailing slash",
			raw:  "https://api.example.test/",
			want: "https://api.example.test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizeAPIServerURL(tt.raw)

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestDecodeAPIErrorMapsStructuredError(t *testing.T) {
	t.Parallel()

	detail := "role does not grant pipelines:read"
	body, err := json.Marshal(api.ErrorResponse{
		Error: api.Error{
			Code:          "FORBIDDEN",
			Message:       "not allowed",
			DetailedError: &detail,
		},
	})
	require.NoError(t, err)

	err = decodeAPIError(http.StatusForbidden, body)

	require.Error(t, err)
	require.ErrorContains(t, err, "403")
	require.ErrorContains(t, err, "FORBIDDEN")
	require.ErrorContains(t, err, "not allowed")
	require.ErrorContains(t, err, detail)
}

func TestDecodeAPIErrorFallsBackForInvalidBody(t *testing.T) {
	t.Parallel()

	err := decodeAPIError(http.StatusInternalServerError, []byte(`not-json`))

	require.Error(t, err)
	require.ErrorContains(t, err, "500")
	require.ErrorContains(t, err, "unexpected API response")
}
