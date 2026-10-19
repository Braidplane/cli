package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/stretchr/testify/require"
)

func TestHTTPAPIKeyCredentialsAndRequestFile(t *testing.T) {
	for _, header := range []string{"X-Api-Key", "Authorization"} {
		t.Run(header, func(t *testing.T) {
			flags := dataSourceRequestFlags{APIKey: "synthetic-key", APIKeyHeader: header}
			credentials, secrets, err := flags.httpCredentials(true)
			require.NoError(t, err)
			require.Equal(t, "synthetic-key", *credentials.ApiKey)
			require.Equal(t, header, string(*credentials.ApiKeyHeader))
			require.Contains(t, secrets, "synthetic-key")
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "key"), []byte("synthetic-key"), 0o600))
			raw, _ := json.Marshal(map[string]string{"api_key_file": "key", "api_key_header": header})
			fileFlags := dataSourceRequestFlags{}
			secrets, err = decodeDataSourceCredentialsFile(api.DataSourceTypeHttp, raw, dir, true, &fileFlags)
			require.NoError(t, err)
			require.Contains(t, secrets, "synthetic-key")
			got, _, err := fileFlags.httpCredentials(true)
			require.NoError(t, err)
			require.Equal(t, credentials, got)
			require.True(t, flags.hasDirectInputFlags())
		})
	}
	for _, flags := range []dataSourceRequestFlags{
		{APIKey: "synthetic-key", APIKeyHeader: "X-Api-Key", HTTPOAuth: &api.HTTPOAuthCredentials{}},
		{APIKey: "synthetic-key"},
		{APIKeyHeader: "X-Api-Key"},
		{APIKey: "synthetic-key", APIKeyHeader: "Cookie"},
		{APIKey: "synthetic-key", APIKeyHeader: "X-Api-Key", BearerToken: "bearer"},
		{APIKey: "synthetic-key", APIKeyHeader: "X-Api-Key", Username: "user", Password: "pass"},
	} {
		_, _, err := flags.httpCredentials(true)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "synthetic-key")
	}
}

func TestHTTPAPIKeyRequestFileRejectsOAuth(t *testing.T) {
	for _, raw := range []string{
		`{"oauth":{},"api_key":"synthetic-key","api_key_header":"X-Api-Key"}`,
		`{"oauth":{},"api_key_file":"key","api_key_header":"X-Api-Key"}`,
	} {
		_, err := decodeDataSourceCredentialsFile(api.DataSourceTypeHttp, json.RawMessage(raw), t.TempDir(), true, &dataSourceRequestFlags{})
		require.ErrorContains(t, err, "cannot be combined")
		require.NotContains(t, err.Error(), "synthetic-key")
	}
}
