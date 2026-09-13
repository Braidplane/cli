package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSaveLoginWithCredentialStoreWritesMetadataOnlyAndRoundTripsSecret(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.toml")
	store := newMemoryCredentialStore()
	want := cliAuthConfig{
		ServerURL:    "http://127.0.0.1:8080",
		Organization: "acme",
		APIKey:       "xto_test_secret",
	}

	require.NoError(t, saveLoginWithCredentialStore(path, store, want))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(data), "xto_test_secret")
	require.NotContains(t, string(data), "api_key")
	require.NotContains(t, string(data), "pat")

	got, err := loadLoginWithCredentialStore(path, store)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestSaveLoginWithCredentialStoreReplacesPreviousCredentialType(t *testing.T) {
	t.Parallel()

	t.Run("PATRemovesPreviousAPIKey", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "config.toml")
		store := newMemoryCredentialStore()
		require.NoError(t, store.Set(credentialTypeAPIKey, "acme", "https://api.example.test", "xto_stale_api_key"))

		require.NoError(t, saveLoginWithCredentialStore(path, store, cliAuthConfig{
			ServerURL:    "https://api.example.test",
			Organization: "acme",
			PAT:          "xto_pat_current",
		}))

		_, err := store.Get(credentialTypeAPIKey, "acme", "https://api.example.test")
		require.ErrorIs(t, err, errMemoryCredentialNotFound)
		got, err := store.Get(credentialTypePAT, "acme", "https://api.example.test")
		require.NoError(t, err)
		require.Equal(t, "xto_pat_current", got)
	})

	t.Run("APIKeyRemovesPreviousPAT", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "config.toml")
		store := newMemoryCredentialStore()
		require.NoError(t, store.Set(credentialTypePAT, "acme", "https://api.example.test", "xto_pat_stale"))

		require.NoError(t, saveLoginWithCredentialStore(path, store, cliAuthConfig{
			ServerURL:    "https://api.example.test",
			Organization: "acme",
			APIKey:       "xto_current_api_key",
		}))

		_, err := store.Get(credentialTypePAT, "acme", "https://api.example.test")
		require.ErrorIs(t, err, errMemoryCredentialNotFound)
		got, err := store.Get(credentialTypeAPIKey, "acme", "https://api.example.test")
		require.NoError(t, err)
		require.Equal(t, "xto_current_api_key", got)
	})
}

func TestLoadLoginWithCredentialStoreReportsMissingCredential(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.toml")
	store := newMemoryCredentialStore()
	require.NoError(t, saveLoginWithCredentialStore(path, store, cliAuthConfig{
		ServerURL:    "https://api.example.test",
		Organization: "acme",
		APIKey:       "xto_test_secret",
	}))
	require.NoError(t, store.Delete(credentialTypeAPIKey, "acme", "https://api.example.test"))

	_, err := loadLoginWithCredentialStore(path, store)

	require.Error(t, err)
	require.ErrorContains(t, err, "stored CLI token")
	require.ErrorContains(t, err, "acme")
}

func TestDeleteLoginWithCredentialStoreRemovesConfigAndCredential(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.toml")
	store := newMemoryCredentialStore()
	require.NoError(t, saveLoginWithCredentialStore(path, store, cliAuthConfig{
		ServerURL:    "https://api.example.test",
		Organization: "acme",
		APIKey:       "xto_test_secret",
		PAT:          "xto_pat_test_secret",
	}))

	require.NoError(t, deleteLoginWithCredentialStore(path, store))

	_, statErr := os.Stat(path)
	require.ErrorIs(t, statErr, os.ErrNotExist)
	_, err := store.Get(credentialTypeAPIKey, "acme", "https://api.example.test")
	require.ErrorIs(t, err, errMemoryCredentialNotFound)
	_, err = store.Get(credentialTypePAT, "acme", "https://api.example.test")
	require.ErrorIs(t, err, errMemoryCredentialNotFound)
}

func TestDeleteLoginWithCredentialStoreSkipsCredentialWhenConfigContainsAPIKey(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.toml")
	store := newMemoryCredentialStore()
	require.NoError(t, store.Set(credentialTypeAPIKey, "acme", "https://api.example.test", "xto_keyring_secret"))
	require.NoError(t, store.Set(credentialTypePAT, "acme", "https://api.example.test", "xto_pat_keyring_secret"))
	require.NoError(t, saveLogin(path, cliAuthConfig{
		ServerURL:    "https://api.example.test",
		Organization: "acme",
		APIKey:       "xto_plaintext_secret",
	}))

	require.NoError(t, deleteLoginWithCredentialStore(path, store))

	_, statErr := os.Stat(path)
	require.ErrorIs(t, statErr, os.ErrNotExist)
	got, err := store.Get(credentialTypeAPIKey, "acme", "https://api.example.test")
	require.NoError(t, err)
	require.Equal(t, "xto_keyring_secret", got)
	got, err = store.Get(credentialTypePAT, "acme", "https://api.example.test")
	require.NoError(t, err)
	require.Equal(t, "xto_pat_keyring_secret", got)
}

func TestDeleteLoginWithCredentialStoreSkipsCredentialDeleteFailureWhenConfigContainsAPIKey(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, saveLogin(path, cliAuthConfig{
		ServerURL:    "https://api.example.test",
		Organization: "acme",
		APIKey:       "xto_plaintext_secret",
	}))

	err := deleteLoginWithCredentialStore(path, failingDeleteCredentialStore{
		err: errMemoryCredentialDeleteFailed,
	})

	require.NoError(t, err)
	_, statErr := os.Stat(path)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestDeleteLoginWithCredentialStorePreservesMetadataWhenCredentialDeleteFails(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, saveLoginMetadata(path, cliAuthConfig{
		ServerURL:    "https://api.example.test",
		Organization: "acme",
	}))

	err := deleteLoginWithCredentialStore(path, failingDeleteCredentialStore{
		err: errMemoryCredentialDeleteFailed,
	})

	require.ErrorIs(t, err, errMemoryCredentialDeleteFailed)
	require.ErrorContains(t, err, "delete stored CLI API key")
	got, loadErr := loadLogin(path)
	require.NoError(t, loadErr)
	require.Equal(t, cliAuthConfig{
		ServerURL:    "https://api.example.test",
		Organization: "acme",
	}, got)
}

func TestResolveCLIAuthConfigPrecedence(t *testing.T) {
	t.Parallel()

	got, err := resolveCLIAuthConfig(cliAuthSources{
		Flags: cliAuthConfig{
			ServerURL: "https://flag.example",
		},
		Env: cliAuthConfig{
			ServerURL:    "https://env.example",
			Organization: "env-org",
		},
		Saved: cliAuthConfig{
			ServerURL:    "https://saved.example",
			Organization: "saved-org",
			APIKey:       "saved-token",
			PAT:          "saved-pat",
		},
	})

	require.NoError(t, err)
	require.Equal(t, cliAuthConfig{
		ServerURL:    "https://flag.example",
		Organization: "env-org",
		APIKey:       "saved-token",
		PAT:          "saved-pat",
	}, got)
}

func TestResolveCLIAuthConfigRequiresAPIFields(t *testing.T) {
	t.Parallel()

	_, err := resolveCLIAuthConfig(cliAuthSources{})

	require.Error(t, err)
	require.ErrorContains(t, err, "server URL")
	require.ErrorContains(t, err, "organization")
	require.ErrorContains(t, err, "API key or personal access token")
}

func TestResolvePATAuthConfigRequiresPATAndDropsAPIKey(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, saveLogin(path, cliAuthConfig{
		ServerURL:    "https://saved.example",
		Organization: "saved-org",
		APIKey:       "saved-api-key",
		PAT:          "saved-pat",
	}))

	got, err := apiAuthFlags{}.resolvePATAuthConfig(&cliRuntime{configPath: path})

	require.NoError(t, err)
	require.Equal(t, cliAuthConfig{
		ServerURL:    "https://saved.example",
		Organization: "saved-org",
		PAT:          "saved-pat",
	}, got)
}

func TestResolvePATAuthConfigLoadsSavedPATWhenAPIKeyEnvIsSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	store := newMemoryCredentialStore()
	require.NoError(t, saveLoginMetadata(path, cliAuthConfig{
		ServerURL:    "https://saved.example/api",
		Organization: "saved-org",
	}))
	require.NoError(t, store.Set(credentialTypePAT, "saved-org", "https://saved.example/api", "saved-pat"))
	t.Setenv("BRAIDPLANE_API_KEY", "env-api-key")

	got, err := apiAuthFlags{}.resolvePATAuthConfig(&cliRuntime{
		configPath:      path,
		credentialStore: store,
	})

	require.NoError(t, err)
	require.Equal(t, cliAuthConfig{
		ServerURL:    "https://saved.example/api",
		Organization: "saved-org",
		PAT:          "saved-pat",
	}, got)
}

func TestResolvePATAuthConfigRejectsAPIKeyOnlyConfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, saveLogin(path, cliAuthConfig{
		ServerURL:    "https://saved.example",
		Organization: "saved-org",
		APIKey:       "saved-api-key",
	}))

	_, err := apiAuthFlags{}.resolvePATAuthConfig(&cliRuntime{configPath: path})

	require.Error(t, err)
	require.ErrorContains(t, err, "personal access token")
}

func TestRuntimeResolveAuthConfigUsesEnvAndSavedLogin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, saveLogin(path, cliAuthConfig{
		ServerURL:    "https://saved.example",
		Organization: "saved-org",
		APIKey:       "saved-token",
	}))
	t.Setenv("BRAIDPLANE_ORG", "env-org")

	got, err := cliRuntime{configPath: path}.resolveAuthConfig(cliAuthConfig{})

	require.NoError(t, err)
	require.Equal(t, cliAuthConfig{
		ServerURL:    "https://saved.example",
		Organization: "env-org",
		APIKey:       "saved-token",
	}, got)
}

func TestRuntimeResolveAuthConfigDoesNotReadCredentialStoreWhenAPIKeyComesFromFlags(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, saveLoginMetadata(path, cliAuthConfig{
		ServerURL:    "https://saved.example",
		Organization: "saved-org",
	}))

	got, err := cliRuntime{
		configPath:      path,
		credentialStore: newMemoryCredentialStore(),
	}.resolveAuthConfig(cliAuthConfig{APIKey: "flag-token"})

	require.NoError(t, err)
	require.Equal(t, cliAuthConfig{
		ServerURL:    "https://saved.example",
		Organization: "saved-org",
		APIKey:       "flag-token",
	}, got)
}

func TestRuntimeResolveAuthConfigDoesNotReadCredentialStoreWhenAPIKeyComesFromEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, saveLoginMetadata(path, cliAuthConfig{
		ServerURL:    "https://saved.example",
		Organization: "saved-org",
	}))
	t.Setenv("BRAIDPLANE_API_KEY", "env-token")

	got, err := cliRuntime{
		configPath:      path,
		credentialStore: newMemoryCredentialStore(),
	}.resolveAuthConfig(cliAuthConfig{})

	require.NoError(t, err)
	require.Equal(t, cliAuthConfig{
		ServerURL:    "https://saved.example",
		Organization: "saved-org",
		APIKey:       "env-token",
	}, got)
}

func TestRuntimeResolveAuthConfigNormalizesTrailingSlashBeforeCredentialStoreLookup(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.toml")
	store := newMemoryCredentialStore()
	require.NoError(t, saveLoginMetadata(path, cliAuthConfig{
		ServerURL:    "https://saved.example.test",
		Organization: "saved-org",
	}))
	require.NoError(t, store.Set(credentialTypeAPIKey, "saved-org", "https://saved.example.test", "stored-token"))

	got, err := cliRuntime{
		configPath:      path,
		credentialStore: store,
	}.resolveAuthConfig(cliAuthConfig{ServerURL: "https://saved.example.test/"})

	require.NoError(t, err)
	require.Equal(t, cliAuthConfig{
		ServerURL:    "https://saved.example.test/",
		Organization: "saved-org",
		APIKey:       "stored-token",
	}, got)
}

func TestRuntimeResolveAuthConfigIgnoresMalformedSavedLoginWhenFlagsAreComplete(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("server_url = ["), 0o600))

	flags := cliAuthConfig{
		ServerURL:    "https://flag.example",
		Organization: "flag-org",
		APIKey:       "flag-token",
	}
	got, err := cliRuntime{configPath: path}.resolveAuthConfig(flags)

	require.NoError(t, err)
	require.Equal(t, flags, got)
}

func TestRuntimeResolveEnvironmentSlugPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, saveLogin(path, cliAuthConfig{Environment: "saved"}))

	t.Run("flag", func(t *testing.T) {
		t.Setenv("BRAIDPLANE_ENVIRONMENT", "env")

		got, err := cliRuntime{configPath: path}.resolveEnvironmentSlug("flag")

		require.NoError(t, err)
		require.Equal(t, "flag", got)
	})

	t.Run("env", func(t *testing.T) {
		t.Setenv("BRAIDPLANE_ENVIRONMENT", "env")

		got, err := cliRuntime{configPath: path}.resolveEnvironmentSlug("")

		require.NoError(t, err)
		require.Equal(t, "env", got)
	})

	t.Run("saved", func(t *testing.T) {
		t.Setenv("BRAIDPLANE_ENVIRONMENT", "")

		got, err := cliRuntime{configPath: path}.resolveEnvironmentSlug("")

		require.NoError(t, err)
		require.Equal(t, "saved", got)
	})
}

func TestRuntimeResolveEnvironmentSlugRequiresConfiguredEnvironment(t *testing.T) {
	t.Parallel()

	_, err := cliRuntime{configPath: filepath.Join(t.TempDir(), "missing.toml")}.resolveEnvironmentSlug("")

	require.Error(t, err)
	require.ErrorContains(t, err, "project environment is required")
	require.ErrorContains(t, err, "set --environment or BRAIDPLANE_ENVIRONMENT")
}

func TestBearerTransportInjectsAuthorizationHeader(t *testing.T) {
	t.Parallel()

	var gotAuth string
	transport := bearerTransport{
		token: "xto_test_secret",
		next: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotAuth = req.Header.Get("Authorization")
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Body:       http.NoBody,
				Request:    req,
			}, nil
		}),
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.example.test/pipelines", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, "Bearer xto_test_secret", gotAuth)
}

func TestDefaultServerURLDoesNotReadRuntimeConfigEnv(t *testing.T) {
	t.Setenv("BACKEND_API_URL", "http://backend.example.test:49912")
	t.Setenv("SERVER_PORT", "49912")

	got := defaultServerURL()

	require.Equal(t, "http://localhost:3001/api", got)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

var (
	errMemoryCredentialNotFound     error = memoryCredentialNotFoundError{}
	errMemoryCredentialDeleteFailed error = memoryCredentialDeleteFailedError{}
)

type memoryCredentialNotFoundError struct{}

func (memoryCredentialNotFoundError) Error() string {
	return "credential not found"
}

type memoryCredentialDeleteFailedError struct{}

func (memoryCredentialDeleteFailedError) Error() string {
	return "credential delete failed"
}

type memoryCredentialStore struct {
	values map[string]string
}

func newMemoryCredentialStore() *memoryCredentialStore {
	return &memoryCredentialStore{values: make(map[string]string)}
}

func (s *memoryCredentialStore) Get(credentialType, organization, serverURL string) (string, error) {
	value, ok := s.values[memoryCredentialKey(credentialType, organization, serverURL)]
	if !ok {
		return "", errMemoryCredentialNotFound
	}

	return value, nil
}

func (s *memoryCredentialStore) Set(credentialType, organization, serverURL, token string) error {
	s.values[memoryCredentialKey(credentialType, organization, serverURL)] = token
	return nil
}

func (s *memoryCredentialStore) Delete(credentialType, organization, serverURL string) error {
	key := memoryCredentialKey(credentialType, organization, serverURL)
	delete(s.values, key)

	return nil
}

func memoryCredentialKey(credentialType, organization, serverURL string) string {
	return credentialType + "\x00" + organization + "\x00" + serverURL
}

type failingDeleteCredentialStore struct {
	err error
}

func (s failingDeleteCredentialStore) Get(_, _, _ string) (string, error) {
	return "", s.err
}

func (s failingDeleteCredentialStore) Set(_, _, _, _ string) error {
	return s.err
}

func (s failingDeleteCredentialStore) Delete(_, _, _ string) error {
	return s.err
}
