package main

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/zalando/go-keyring"

	"github.com/Braidplane/cli/pkg/xerror"
)

const credentialStoreService = "braidplane"

type cliAuthConfig struct {
	ServerURL    string `toml:"server_url"`
	Organization string `toml:"organization"`
	APIKey       string `toml:"api_key,omitempty"`
	Environment  string `toml:"environment,omitempty"`
	PAT          string `toml:"pat,omitempty"`
}

type apiAuthFlags struct {
	ServerURL    string `help:"Braidplane server URL." name:"server-url" env:"BRAIDPLANE_SERVER_URL"`
	Organization string `help:"Organization slug." name:"org" env:"BRAIDPLANE_ORG"`
	APIKey       string `help:"Organization API key token for machine-scoped workflows." name:"api-key" env:"BRAIDPLANE_API_KEY"`
	PAT          string `help:"Personal access token for user-scoped workflows." name:"pat" env:"BRAIDPLANE_PAT"`
}

// accountAuthFlags is used by user-scoped API operations that are not tied to
// an organization. These operations deliberately accept PATs only: an API key
// is an organization principal and must never acquire account-wide authority.
type accountAuthFlags struct {
	ServerURL string `help:"Braidplane server URL." name:"server-url" env:"BRAIDPLANE_SERVER_URL"`
	PAT       string `help:"Personal access token for user-scoped workflows." name:"pat" env:"BRAIDPLANE_PAT"`
}

type apiPipelineFlags struct {
	apiAuthFlags

	Project string `help:"Project slug." name:"project" env:"BRAIDPLANE_PROJECT"`
}

type cliAuthSources struct {
	Flags cliAuthConfig
	Env   cliAuthConfig
	Saved cliAuthConfig
}

func (f apiAuthFlags) toConfig() cliAuthConfig {
	return cliAuthConfig{
		ServerURL:    f.ServerURL,
		Organization: f.Organization,
		APIKey:       f.APIKey,
		PAT:          f.PAT,
	}
}

func (f apiAuthFlags) resolveAuthConfig(runtime *cliRuntime) (cliAuthConfig, error) {
	return runtime.resolveAuthConfig(f.toConfig())
}

func (f apiAuthFlags) resolvePATAuthConfig(runtime *cliRuntime) (cliAuthConfig, error) {
	cfg, err := runtime.resolvePATAuthConfig(f.toConfig())
	if err != nil {
		return cliAuthConfig{}, err
	}
	if cfg.PAT == "" {
		return cliAuthConfig{}, xerror.Unexpected().Detail("missing CLI personal access token configuration: set --pat or BRAIDPLANE_PAT")
	}
	cfg.APIKey = ""
	return cfg, nil
}

func (f accountAuthFlags) resolveAuthConfig(runtime *cliRuntime) (cliAuthConfig, error) {
	return runtime.resolveAccountPATAuthConfig(cliAuthConfig{
		ServerURL: f.ServerURL,
		PAT:       f.PAT,
	})
}

func (f apiPipelineFlags) resolveProjectSlug() (string, error) {
	project := strings.TrimSpace(f.Project)
	if project == "" {
		return "", xerror.InvalidRequired(xerror.Project).Detail("set --project or BRAIDPLANE_PROJECT")
	}

	return project, nil
}

func (r cliRuntime) resolveConfigPath() (string, error) {
	if strings.TrimSpace(r.configPath) != "" {
		return r.configPath, nil
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", xerror.Wrapf("resolve user config directory: %w", err)
	}

	return filepath.Join(configDir, "braidplane", "config.toml"), nil
}

func envAuthConfig() cliAuthConfig {
	return cliAuthConfig{
		ServerURL:    os.Getenv("BRAIDPLANE_SERVER_URL"),
		Organization: os.Getenv("BRAIDPLANE_ORG"),
		APIKey:       os.Getenv("BRAIDPLANE_API_KEY"),
		Environment:  os.Getenv("BRAIDPLANE_ENVIRONMENT"),
		PAT:          os.Getenv("BRAIDPLANE_PAT"),
	}
}

func (r cliRuntime) resolveAuthConfig(flags cliAuthConfig) (cliAuthConfig, error) {
	env := envAuthConfig()
	cfg, err := resolveCLIAuthConfig(cliAuthSources{
		Flags: flags,
		Env:   env,
	})
	if err == nil {
		return cfg, nil
	}

	configPath, err := r.resolveConfigPath()
	if err != nil {
		return cliAuthConfig{}, err
	}

	saved, err := loadLogin(configPath)
	if err != nil {
		return cliAuthConfig{}, err
	}

	cfg = mergeCLIAuthConfig(cliAuthSources{
		Flags: flags,
		Env:   env,
		Saved: saved,
	})
	if cfg.APIKey == "" && cfg.PAT == "" && cfg.Organization != "" && cfg.ServerURL != "" {
		credentialServerURL, err := normalizeAPIServerURL(cfg.ServerURL)
		if err != nil {
			return cliAuthConfig{}, err
		}
		apiKey, pat, err := loadStoredCLITokens(r.resolveCredentialStore(), cfg.Organization, credentialServerURL)
		if err != nil {
			return cliAuthConfig{}, err
		}
		cfg.APIKey = apiKey
		cfg.PAT = pat
	}

	return validateCLIAuthConfig(cfg)
}

func (r cliRuntime) resolveEnvironmentSlug(value string) (string, error) {
	environment := firstNonEmpty(value, os.Getenv("BRAIDPLANE_ENVIRONMENT"))
	if environment != "" {
		return environment, nil
	}

	configPath, err := r.resolveConfigPath()
	if err != nil {
		return "", err
	}
	saved, err := loadLogin(configPath)
	if err != nil {
		return "", err
	}

	return resolvePipelineEnvironmentSlug(saved.Environment)
}

func (r cliRuntime) resolvePATAuthConfig(flags cliAuthConfig) (cliAuthConfig, error) {
	flags.APIKey = ""
	env := envAuthConfig()
	env.APIKey = ""

	cfg, err := resolveCLIAuthConfig(cliAuthSources{
		Flags: flags,
		Env:   env,
	})
	if err == nil {
		return cfg, nil
	}

	configPath, err := r.resolveConfigPath()
	if err != nil {
		return cliAuthConfig{}, err
	}

	saved, err := loadLogin(configPath)
	if err != nil {
		return cliAuthConfig{}, err
	}
	savedHasInlineToken := saved.APIKey != "" || saved.PAT != ""
	saved.APIKey = ""

	cfg = mergeCLIAuthConfig(cliAuthSources{
		Flags: flags,
		Env:   env,
		Saved: saved,
	})
	if cfg.PAT == "" && cfg.Organization != "" && cfg.ServerURL != "" && !savedHasInlineToken {
		credentialServerURL, err := normalizeAPIServerURL(cfg.ServerURL)
		if err != nil {
			return cliAuthConfig{}, err
		}
		_, pat, err := loadStoredCLITokens(r.resolveCredentialStore(), cfg.Organization, credentialServerURL)
		if err != nil {
			return cliAuthConfig{}, err
		}
		cfg.PAT = pat
	}

	if cfg.ServerURL == "" || cfg.Organization == "" {
		return validateCLIAuthConfig(cfg)
	}

	return cfg, nil
}

func (r cliRuntime) resolveAccountPATAuthConfig(flags cliAuthConfig) (cliAuthConfig, error) {
	flags.APIKey = ""
	env := envAuthConfig()
	env.APIKey = ""

	configPath, err := r.resolveConfigPath()
	if err != nil {
		return cliAuthConfig{}, err
	}
	saved, err := loadLogin(configPath)
	if err != nil {
		return cliAuthConfig{}, err
	}
	savedHasInlineToken := saved.APIKey != "" || saved.PAT != ""
	saved.APIKey = ""

	cfg := mergeCLIAuthConfig(cliAuthSources{Flags: flags, Env: env, Saved: saved})
	if cfg.PAT == "" && cfg.Organization != "" && cfg.ServerURL != "" && !savedHasInlineToken {
		credentialServerURL, normalizeErr := normalizeAPIServerURL(cfg.ServerURL)
		if normalizeErr != nil {
			return cliAuthConfig{}, normalizeErr
		}
		_, pat, loadErr := loadStoredCLITokens(r.resolveCredentialStore(), cfg.Organization, credentialServerURL)
		if loadErr != nil {
			return cliAuthConfig{}, loadErr
		}
		cfg.PAT = pat
	}

	missing := make([]string, 0, 2)
	if cfg.ServerURL == "" {
		missing = append(missing, "server URL")
	}
	if cfg.PAT == "" {
		missing = append(missing, "personal access token")
	}
	if len(missing) > 0 {
		return cliAuthConfig{}, xerror.Unexpected().Detail(
			"missing CLI account authentication configuration: " + strings.Join(missing, ", "),
		)
	}

	cfg.APIKey = ""
	return cfg, nil
}

type cliCredentialStore interface {
	Get(credentialType, organization, serverURL string) (string, error)
	Set(credentialType, organization, serverURL, token string) error
	Delete(credentialType, organization, serverURL string) error
}

type osCredentialStore struct{}

const (
	credentialTypeAPIKey = "api-key"
	credentialTypePAT    = "pat"
)

func (osCredentialStore) Get(credentialType, organization, serverURL string) (string, error) {
	token, err := keyring.Get(credentialStoreService, credentialStoreAccount(credentialType, organization, serverURL))
	if err != nil {
		return "", xerror.Unexpected().Detailf("read CLI token from credential store: %w", err)
	}

	return token, nil
}

func (osCredentialStore) Set(credentialType, organization, serverURL, token string) error {
	if err := keyring.Set(credentialStoreService, credentialStoreAccount(credentialType, organization, serverURL), token); err != nil {
		return xerror.Unexpected().Detailf("write CLI token to credential store: %w", err)
	}

	return nil
}

func (osCredentialStore) Delete(credentialType, organization, serverURL string) error {
	err := keyring.Delete(credentialStoreService, credentialStoreAccount(credentialType, organization, serverURL))
	if xerror.Is(err, keyring.ErrNotFound) {
		return nil
	}
	if err != nil {
		return xerror.Unexpected().Detailf("delete CLI token from credential store: %w", err)
	}

	return nil
}

func (r cliRuntime) resolveCredentialStore() cliCredentialStore {
	if r.credentialStore != nil {
		return r.credentialStore
	}

	return osCredentialStore{}
}

func credentialStoreAccount(credentialType, organization, serverURL string) string {
	return credentialType + ":" + organization + "@" + serverURL
}

func saveLogin(path string, cfg cliAuthConfig) error {
	var data bytes.Buffer
	if err := toml.NewEncoder(&data).Encode(cfg); err != nil {
		return xerror.Unexpected().With(err)
	}

	return writeLoginFile(path, data.Bytes())
}

func saveLoginWithCredentialStore(path string, store cliCredentialStore, cfg cliAuthConfig) error {
	if cfg.APIKey != "" {
		if err := store.Set(credentialTypeAPIKey, cfg.Organization, cfg.ServerURL, cfg.APIKey); err != nil {
			return xerror.Wrapf("store CLI API key: %w", err)
		}
		if cfg.PAT == "" {
			if err := store.Delete(credentialTypePAT, cfg.Organization, cfg.ServerURL); err != nil {
				return xerror.Wrapf("remove stale CLI personal access token: %w", err)
			}
		}
	}
	if cfg.PAT != "" {
		if err := store.Set(credentialTypePAT, cfg.Organization, cfg.ServerURL, cfg.PAT); err != nil {
			return xerror.Wrapf("store CLI personal access token: %w", err)
		}
		if cfg.APIKey == "" {
			if err := store.Delete(credentialTypeAPIKey, cfg.Organization, cfg.ServerURL); err != nil {
				return xerror.Wrapf("remove stale CLI API key: %w", err)
			}
		}
	}

	cfg.APIKey = ""
	cfg.PAT = ""
	return saveLoginMetadata(path, cfg)
}

func saveLoginMetadata(path string, cfg cliAuthConfig) error {
	cfg.APIKey = ""
	cfg.PAT = ""
	var data bytes.Buffer
	if err := toml.NewEncoder(&data).Encode(cfg); err != nil {
		return xerror.Unexpected().With(err)
	}

	return writeLoginFile(path, data.Bytes())
}

func writeLoginFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return xerror.Wrapf("create CLI config directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return xerror.Wrapf("write CLI config: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return xerror.Wrapf("set CLI config permissions: %w", err)
	}

	return nil
}

func loadLogin(path string) (cliAuthConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cliAuthConfig{}, nil
		}
		return cliAuthConfig{}, xerror.Wrapf("read CLI config: %w", err)
	}

	var cfg cliAuthConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cliAuthConfig{}, xerror.Wrapf("parse CLI config: %w", err)
	}

	return cfg, nil
}

func loadLoginWithCredentialStore(path string, store cliCredentialStore) (cliAuthConfig, error) {
	cfg, err := loadLogin(path)
	if err != nil {
		return cliAuthConfig{}, err
	}
	if cfg.ServerURL == "" && cfg.Organization == "" {
		return cfg, nil
	}
	if cfg.APIKey != "" || cfg.PAT != "" {
		return cfg, nil
	}

	cfg.APIKey, cfg.PAT, err = loadStoredCLITokens(store, cfg.Organization, cfg.ServerURL)
	if err != nil {
		return cliAuthConfig{}, err
	}

	return cfg, nil
}

func deleteLoginWithCredentialStore(path string, store cliCredentialStore) error {
	cfg, err := loadLogin(path)
	if err != nil {
		return err
	}
	if cfg.APIKey == "" && cfg.PAT == "" && cfg.Organization != "" && cfg.ServerURL != "" {
		if err := store.Delete(credentialTypeAPIKey, cfg.Organization, cfg.ServerURL); err != nil {
			return xerror.Wrapf("delete stored CLI API key: %w", err)
		}
		if err := store.Delete(credentialTypePAT, cfg.Organization, cfg.ServerURL); err != nil {
			return xerror.Wrapf("delete stored CLI personal access token: %w", err)
		}
	}

	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return xerror.Wrapf("delete CLI config: %w", err)
	}

	return nil
}

func resolveCLIAuthConfig(sources cliAuthSources) (cliAuthConfig, error) {
	return validateCLIAuthConfig(mergeCLIAuthConfig(sources))
}

func mergeCLIAuthConfig(sources cliAuthSources) cliAuthConfig {
	return cliAuthConfig{
		ServerURL: firstNonEmpty(
			sources.Flags.ServerURL,
			sources.Env.ServerURL,
			sources.Saved.ServerURL,
		),
		Organization: firstNonEmpty(
			sources.Flags.Organization,
			sources.Env.Organization,
			sources.Saved.Organization,
		),
		APIKey: firstNonEmpty(
			sources.Flags.APIKey,
			sources.Env.APIKey,
			sources.Saved.APIKey,
		),
		Environment: firstNonEmpty(
			sources.Flags.Environment,
			sources.Env.Environment,
			sources.Saved.Environment,
		),
		PAT: firstNonEmpty(
			sources.Flags.PAT,
			sources.Env.PAT,
			sources.Saved.PAT,
		),
	}
}

func validateCLIAuthConfig(cfg cliAuthConfig) (cliAuthConfig, error) {
	missing := make([]string, 0, 3)
	if cfg.ServerURL == "" {
		missing = append(missing, "server URL")
	}
	if cfg.Organization == "" {
		missing = append(missing, "organization")
	}
	if cfg.APIKey == "" && cfg.PAT == "" {
		missing = append(missing, "API key or personal access token")
	}
	if len(missing) > 0 {
		return cliAuthConfig{}, xerror.Unexpected().Detail(
			"missing CLI authentication configuration: " + strings.Join(missing, ", "),
		)
	}

	return cfg, nil
}

func loadStoredCLITokens(store cliCredentialStore, organization, serverURL string) (string, string, error) {
	apiKey, apiKeyErr := store.Get(credentialTypeAPIKey, organization, serverURL)
	pat, patErr := store.Get(credentialTypePAT, organization, serverURL)
	if apiKey != "" || pat != "" {
		return apiKey, pat, nil
	}
	if apiKeyErr != nil {
		return "", "", xerror.Wrapf(
			"load stored CLI token for organization %q: %w",
			organization,
			apiKeyErr,
		)
	}
	if patErr != nil {
		return "", "", xerror.Wrapf(
			"load stored CLI token for organization %q: %w",
			organization,
			patErr,
		)
	}

	return "", "", xerror.Unexpected().Detail("stored CLI token is empty")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// defaultServerURL returns the local API target used when login omits server URL input.
func defaultServerURL() string {
	return "http://localhost:3001/api"
}

type bearerTransport struct {
	token string
	next  http.RoundTripper
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	next := t.next
	if next == nil {
		next = http.DefaultTransport
	}

	cloned := req.Clone(req.Context())
	cloned.Header = req.Header.Clone()
	cloned.Header.Set("Authorization", "Bearer "+t.token)

	return next.RoundTrip(cloned)
}

func (cfg cliAuthConfig) authToken() string {
	if cfg.APIKey != "" {
		return cfg.APIKey
	}
	return cfg.PAT
}
