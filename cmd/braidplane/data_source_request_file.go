package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

type dataSourceRequestFile struct {
	Type        string          `json:"type"`
	Slug        string          `json:"slug,omitempty"`
	Name        string          `json:"name,omitempty"`
	ReadOnly    *bool           `json:"read_only,omitempty"`
	Config      json.RawMessage `json:"config,omitempty"`
	Credentials json.RawMessage `json:"credentials,omitempty"`
}

type passwordCredentialFile struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	PasswordFile string `json:"password_file"`
}

type bigQueryCredentialFile struct {
	ServiceAccountJSON     string `json:"service_account_json"`
	ServiceAccountJSONFile string `json:"service_account_json_file"`
}

type snowflakeCredentialFile struct {
	AuthType     string `json:"auth_type"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	PasswordFile string `json:"password_file"`
}

type httpCredentialFile struct {
	BearerToken     string `json:"bearer_token"`
	BearerTokenFile string `json:"bearer_token_file"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	PasswordFile    string `json:"password_file"`
}

type hubSpotCredentialFile struct {
	PrivateAppToken     string `json:"private_app_token"`
	PrivateAppTokenFile string `json:"private_app_token_file"`
}

type gitCredentialFile struct {
	AuthType          string `json:"auth_type"`
	Username          string `json:"username"`
	Token             string `json:"token"`
	TokenFile         string `json:"token_file"`
	SSHPrivateKey     string `json:"ssh_private_key"`
	SSHPrivateKeyFile string `json:"ssh_private_key_file"`
	SSHPassphrase     string `json:"ssh_passphrase"`
	SSHPassphraseFile string `json:"ssh_passphrase_file"`
	SSHKnownHosts     string `json:"ssh_known_hosts"`
	SSHKnownHostsFile string `json:"ssh_known_hosts_file"`
}

type s3CredentialFile struct {
	AccessKeyID         string `json:"access_key_id"`
	AccessKeyIDFile     string `json:"access_key_id_file"`
	SecretAccessKey     string `json:"secret_access_key"`
	SecretAccessKeyFile string `json:"secret_access_key_file"`
	SessionToken        string `json:"session_token"`
	SessionTokenFile    string `json:"session_token_file"`
}

// createDataSourceBodyFromFile builds a create body from an authored JSON request file.
func createDataSourceBodyFromFile(path string) (api.CreateDataSourceRequest, []string, error) {
	file, baseDir, err := loadDataSourceRequestFile(path)
	if err != nil {
		return api.CreateDataSourceRequest{}, nil, err
	}
	if strings.TrimSpace(file.Name) == "" {
		return api.CreateDataSourceRequest{}, nil, xerror.InvalidRequired(xerror.DataSource).Detail("set name in --file")
	}
	flags, secrets, err := file.toFlags(baseDir, true, true)
	if err != nil {
		return api.CreateDataSourceRequest{}, nil, err
	}
	command := createDataSourceCommand{
		Type:                   file.Type,
		Name:                   file.Name,
		Slug:                   file.Slug,
		dataSourceRequestFlags: flags,
	}
	if file.ReadOnly != nil {
		command.ReadOnly = *file.ReadOnly
	}
	body, _, err := command.createBody()
	return body, secrets, err
}

// updateDataSourceBodyFromFile builds an update body and target slug from an authored JSON request file.
func updateDataSourceBodyFromFile(path string) (api.UpdateDataSourceRequest, string, error) {
	file, baseDir, err := loadDataSourceRequestFile(path)
	if err != nil {
		return api.UpdateDataSourceRequest{}, "", err
	}
	if strings.TrimSpace(file.Slug) == "" {
		return api.UpdateDataSourceRequest{}, "", xerror.InvalidRequired(xerror.DataSource).Detail("set slug in --file")
	}
	if len(bytes.TrimSpace(file.Credentials)) != 0 {
		return api.UpdateDataSourceRequest{}, "", xerror.InvalidInput(xerror.DataSource).
			Detail("data-source update files cannot include credentials; use data-source credentials update --file")
	}
	flags, _, err := file.toFlags(baseDir, false, false)
	if err != nil {
		return api.UpdateDataSourceRequest{}, "", err
	}
	command := updateDataSourceCommand{
		Type:                   file.Type,
		DataSourceSlug:         file.Slug,
		Name:                   file.Name,
		dataSourceRequestFlags: flags,
	}
	if file.ReadOnly != nil {
		if *file.ReadOnly {
			command.ReadOnly = true
		} else {
			command.ReadWrite = true
		}
	}
	body, slug, err := command.updateBody()
	return body, slug, err
}

// testDataSourceBodyFromFile builds a connection-test body from an authored JSON request file.
func testDataSourceBodyFromFile(path string) (api.TestDataSourceConnectionRequest, []string, error) {
	file, baseDir, err := loadDataSourceRequestFile(path)
	if err != nil {
		return api.TestDataSourceConnectionRequest{}, nil, err
	}
	flags, secrets, err := file.toFlags(baseDir, true, true)
	if err != nil {
		return api.TestDataSourceConnectionRequest{}, nil, err
	}
	command := testDataSourceCommand{Type: file.Type, dataSourceRequestFlags: flags}
	body, _, err := command.testBody()
	return body, secrets, err
}

// dataSourceCredentialsBodyFromFile builds a credentials update body and target slug from JSON.
func dataSourceCredentialsBodyFromFile(
	path string,
	required bool,
) (api.DataSourceCredentials, []string, string, error) {
	file, baseDir, err := loadDataSourceRequestFile(path)
	if err != nil {
		return api.DataSourceCredentials{}, nil, "", err
	}
	if strings.TrimSpace(file.Slug) == "" {
		return api.DataSourceCredentials{}, nil, "", xerror.InvalidRequired(xerror.DataSource).Detail("set slug in --file")
	}
	if strings.TrimSpace(file.Name) != "" || file.ReadOnly != nil || len(bytes.TrimSpace(file.Config)) != 0 {
		return api.DataSourceCredentials{}, nil, "", xerror.InvalidInput(xerror.DataSource).
			Detail("credentials update files can include only type, slug, and credentials")
	}
	flags, secrets, err := file.toFlags(baseDir, false, required)
	if err != nil {
		return api.DataSourceCredentials{}, nil, "", err
	}
	command := updateDataSourceCredentialsCommand{
		Type:                   file.Type,
		DataSourceSlug:         file.Slug,
		dataSourceRequestFlags: flags,
	}
	body, _, slug, err := command.credentialsBody(required)
	return body, secrets, slug, err
}

// loadDataSourceRequestFile decodes one strict JSON request file and reports its secret path base.
func loadDataSourceRequestFile(path string) (dataSourceRequestFile, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return dataSourceRequestFile{}, "", xerror.InvalidRequired(xerror.DataSource).Detail("set --file")
	}
	// #nosec G304 -- the CLI intentionally reads a user-selected request file.
	data, err := os.ReadFile(path)
	if err != nil {
		return dataSourceRequestFile{}, "", xerror.Wrapf("read data-source request file: %w", err)
	}
	var file dataSourceRequestFile
	if err := decodeStrictJSON(data, &file, "data-source request file"); err != nil {
		return dataSourceRequestFile{}, "", err
	}
	if strings.TrimSpace(file.Type) == "" {
		return dataSourceRequestFile{}, "", xerror.InvalidRequired(xerror.DataSource).Detail("set type in --file")
	}
	if _, err := parseDataSourceType(file.Type); err != nil {
		return dataSourceRequestFile{}, "", err
	}
	return file, filepath.Dir(path), nil
}

// toFlags converts decoded JSON into the established flag-backed request builders.
func (f dataSourceRequestFile) toFlags(
	baseDir string,
	configRequired bool,
	credentialsRequired bool,
) (dataSourceRequestFlags, []string, error) {
	dataSourceType, err := parseDataSourceType(f.Type)
	if err != nil {
		return dataSourceRequestFlags{}, nil, err
	}
	flags := dataSourceRequestFlags{}
	if f.ReadOnly != nil {
		flags.ReadOnly = *f.ReadOnly
	}
	if err := decodeDataSourceConfigFile(dataSourceType, f.Config, configRequired, &flags); err != nil {
		return dataSourceRequestFlags{}, nil, err
	}
	secrets, err := decodeDataSourceCredentialsFile(dataSourceType, f.Credentials, baseDir, credentialsRequired, &flags)
	if err != nil {
		return dataSourceRequestFlags{}, nil, err
	}
	return flags, secrets, nil
}

// decodeDataSourceConfigFile strictly decodes provider config into flag-builder fields.
func decodeDataSourceConfigFile(
	dataSourceType api.DataSourceType,
	raw json.RawMessage,
	required bool,
	flags *dataSourceRequestFlags,
) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		if required && dataSourceType != api.DataSourceTypeHubspot {
			return xerror.InvalidRequired(xerror.DataSource).Detail("set config in --file")
		}
		return nil
	}
	switch dataSourceType {
	case api.DataSourceTypePostgres:
		var config api.PostgresConfig
		if err := decodeStrictJSON(raw, &config, "postgres config"); err != nil {
			return err
		}
		flags.Host = config.Host
		flags.Port = config.Port
		flags.Database = config.Database
		flags.ConnectTimeout = intValue(config.ConnectTimeout)
		if config.SslMode != nil {
			flags.SSLMode = string(*config.SslMode)
		}
	case api.DataSourceTypeMysql:
		var config api.MySQLConfig
		if err := decodeStrictJSON(raw, &config, "mysql config"); err != nil {
			return err
		}
		flags.Host = config.Host
		flags.Port = config.Port
		flags.Database = config.Database
		flags.ConnectTimeout = intValue(config.ConnectTimeout)
	case api.DataSourceTypeBigquery:
		var config api.BigQueryConfig
		if err := decodeStrictJSON(raw, &config, "bigquery config"); err != nil {
			return err
		}
		flags.ProjectID = config.ProjectId
		flags.DefaultDataset = stringValue(config.DefaultDataset)
		flags.Location = stringValue(config.Location)
		flags.MaximumBytesBilled = int64Value(config.MaximumBytesBilled)
		flags.TimeoutSeconds = intValue(config.TimeoutSeconds)
	case api.DataSourceTypeSnowflake:
		var config api.SnowflakeConfig
		if err := decodeStrictJSON(raw, &config, "snowflake config"); err != nil {
			return err
		}
		flags.Account = config.Account
		flags.Warehouse = config.Warehouse
		flags.Database = config.Database
		flags.Schema = config.Schema
		flags.Role = stringValue(config.Role)
		flags.ConnectTimeout = intValue(config.ConnectTimeout)
	case api.DataSourceTypeHttp:
		var config api.HTTPConfig
		if err := decodeStrictJSON(raw, &config, "http config"); err != nil {
			return err
		}
		flags.BaseURL = config.BaseUrl
		flags.HealthPath = stringValue(config.HealthPath)
		flags.TimeoutSeconds = intValue(config.TimeoutSeconds)
		flags.OpenAPIURL = stringValue(config.OpenapiUrl)
		flags.OpenAPIFetchTimeoutSeconds = intValue(config.OpenapiFetchTimeoutSeconds)
		flags.AllowPrivateHosts = boolValue(config.AllowPrivateHosts)
	case api.DataSourceTypeHubspot:
		var config api.HubSpotConfig
		if err := decodeStrictJSON(raw, &config, "hubspot config"); err != nil {
			return err
		}
		flags.TimeoutSeconds = intValue(config.TimeoutSeconds)
	case api.DataSourceTypeMongodb:
		var config api.MongoDBConfig
		if err := decodeStrictJSON(raw, &config, "mongodb config"); err != nil {
			return err
		}
		flags.Host = config.Host
		flags.Port = config.Port
		flags.Database = config.Database
		flags.AuthDatabase = stringValue(config.AuthDatabase)
		flags.ConnectTimeout = intValue(config.ConnectTimeout)
	case api.DataSourceTypeFtp:
		var config api.FTPConfig
		if err := decodeStrictJSON(raw, &config, "ftp config"); err != nil {
			return err
		}
		flags.Host = config.Host
		flags.Port = config.Port
		flags.BasePath = stringValue(config.BasePath)
		flags.ConnectTimeout = intValue(config.ConnectTimeout)
		flags.AllowPrivateHosts = boolValue(config.AllowPrivateHosts)
	case api.DataSourceTypeGit, api.DataSourceTypeGithub, api.DataSourceTypeGitlab:
		var config api.GitConfig
		if err := decodeStrictJSON(raw, &config, "git config"); err != nil {
			return err
		}
		flags.RepoURL = config.RepoUrl
		flags.DefaultBranch = stringValue(config.DefaultBranch)
		flags.BasePath = stringValue(config.BasePath)
		flags.AllowPrivateHosts = boolValue(config.AllowPrivateHosts)
	case api.DataSourceTypeS3:
		var config api.S3Config
		if err := decodeStrictJSON(raw, &config, "s3 config"); err != nil {
			return err
		}
		flags.Endpoint = stringValue(config.Endpoint)
		flags.Region = config.Region
		flags.Bucket = config.Bucket
		flags.Prefix = stringValue(config.Prefix)
		flags.ForcePathStyle = boolValue(config.ForcePathStyle)
		flags.TimeoutSeconds = intValue(config.TimeoutSeconds)
		flags.AllowPrivateHosts = boolValue(config.AllowPrivateHosts)
	default:
		return xerror.Unsupported(xerror.DataSource).Detail("unsupported data source type")
	}
	return nil
}

// decodeDataSourceCredentialsFile strictly decodes provider credentials and resolves secret files.
func decodeDataSourceCredentialsFile(
	dataSourceType api.DataSourceType,
	raw json.RawMessage,
	baseDir string,
	required bool,
	flags *dataSourceRequestFlags,
) ([]string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		if required && dataSourceType != api.DataSourceTypeHttp {
			return nil, xerror.InvalidRequired(xerror.DataSource).Detail("set credentials in --file")
		}
		return nil, nil
	}
	switch dataSourceType {
	case api.DataSourceTypePostgres, api.DataSourceTypeMysql, api.DataSourceTypeMongodb, api.DataSourceTypeFtp:
		var credentials passwordCredentialFile
		if err := decodeStrictJSON(raw, &credentials, "password credentials"); err != nil {
			return nil, err
		}
		password, secrets, err := readSecretFileValue("password", credentials.Password, credentials.PasswordFile, baseDir, required)
		if err != nil {
			return nil, err
		}
		flags.Username = credentials.Username
		flags.Password = password
		return secrets, nil
	case api.DataSourceTypeBigquery:
		var credentials bigQueryCredentialFile
		if err := decodeStrictJSON(raw, &credentials, "bigquery credentials"); err != nil {
			return nil, err
		}
		value, secrets, err := readSecretFileValue("service-account JSON", credentials.ServiceAccountJSON, credentials.ServiceAccountJSONFile, baseDir, required)
		if err != nil {
			return nil, err
		}
		flags.ServiceAccountJSON = value
		return secrets, nil
	case api.DataSourceTypeSnowflake:
		var credentials snowflakeCredentialFile
		if err := decodeStrictJSON(raw, &credentials, "snowflake credentials"); err != nil {
			return nil, err
		}
		flags.SnowflakeAuthType = credentials.AuthType
		flags.Username = credentials.Username
		if strings.TrimSpace(credentials.AuthType) == string(api.SnowflakeWorkloadIdentityCredentialsAuthTypeWorkloadIdentity) {
			if credentials.Password != "" || strings.TrimSpace(credentials.PasswordFile) != "" {
				return nil, xerror.InvalidInput(xerror.DataSource).
					Detail("do not set Snowflake password or password_file with workload_identity")
			}
			return nil, nil
		}
		password, secrets, err := readSecretFileValue("password", credentials.Password, credentials.PasswordFile, baseDir, required)
		if err != nil {
			return nil, err
		}
		flags.Password = password
		return secrets, nil
	case api.DataSourceTypeHttp:
		var credentials httpCredentialFile
		if err := decodeStrictJSON(raw, &credentials, "http credentials"); err != nil {
			return nil, err
		}
		bearer, bearerSecrets, err := readSecretFileValue("bearer token", credentials.BearerToken, credentials.BearerTokenFile, baseDir, false)
		if err != nil {
			return nil, err
		}
		password, passwordSecrets, err := readSecretFileValue("password", credentials.Password, credentials.PasswordFile, baseDir, false)
		if err != nil {
			return nil, err
		}
		flags.BearerToken = bearer
		flags.Username = credentials.Username
		flags.Password = password
		return append(bearerSecrets, passwordSecrets...), nil
	case api.DataSourceTypeHubspot:
		var credentials hubSpotCredentialFile
		if err := decodeStrictJSON(raw, &credentials, "hubspot credentials"); err != nil {
			return nil, err
		}
		token, secrets, err := readSecretFileValue("private app token", credentials.PrivateAppToken, credentials.PrivateAppTokenFile, baseDir, required)
		if err != nil {
			return nil, err
		}
		flags.PrivateAppToken = token
		return secrets, nil
	case api.DataSourceTypeGit, api.DataSourceTypeGithub, api.DataSourceTypeGitlab:
		var credentials gitCredentialFile
		if err := decodeStrictJSON(raw, &credentials, "git credentials"); err != nil {
			return nil, err
		}
		return applyGitCredentialFile(credentials, baseDir, required, flags)
	case api.DataSourceTypeS3:
		var credentials s3CredentialFile
		if err := decodeStrictJSON(raw, &credentials, "s3 credentials"); err != nil {
			return nil, err
		}
		return applyS3CredentialFile(credentials, baseDir, required, flags)
	default:
		return nil, xerror.Unsupported(xerror.DataSource).Detail("unsupported data source type")
	}
}

// applyGitCredentialFile resolves git secret files and fills the common flag struct.
func applyGitCredentialFile(
	credentials gitCredentialFile,
	baseDir string,
	required bool,
	flags *dataSourceRequestFlags,
) ([]string, error) {
	authType := api.GitCredentialsAuthType(strings.TrimSpace(credentials.AuthType))
	if !authType.Valid() {
		return nil, xerror.InvalidInput(xerror.DataSource).Detail("git auth_type must be https or ssh")
	}
	flags.GitAuthType = string(authType)
	flags.Username = credentials.Username
	var secrets []string
	switch authType {
	case api.Https:
		token, tokenSecrets, err := readSecretFileValue("git token", credentials.Token, credentials.TokenFile, baseDir, required)
		if err != nil {
			return nil, err
		}
		flags.GitToken = token
		secrets = append(secrets, tokenSecrets...)
	case api.Ssh:
		key, keySecrets, err := readSecretFileValue("ssh private key", credentials.SSHPrivateKey, credentials.SSHPrivateKeyFile, baseDir, required)
		if err != nil {
			return nil, err
		}
		passphrase, passphraseSecrets, err := readSecretFileValue("ssh passphrase", credentials.SSHPassphrase, credentials.SSHPassphraseFile, baseDir, false)
		if err != nil {
			return nil, err
		}
		knownHosts, knownHostsSecrets, err := readSecretFileValue("ssh known hosts", credentials.SSHKnownHosts, credentials.SSHKnownHostsFile, baseDir, required)
		if err != nil {
			return nil, err
		}
		flags.SSHPrivateKey = key
		flags.SSHPassphrase = passphrase
		flags.SSHKnownHosts = knownHosts
		secrets = append(secrets, keySecrets...)
		secrets = append(secrets, passphraseSecrets...)
		secrets = append(secrets, knownHostsSecrets...)
	}
	return secrets, nil
}

// applyS3CredentialFile resolves S3 secret files and fills the common flag struct.
func applyS3CredentialFile(
	credentials s3CredentialFile,
	baseDir string,
	required bool,
	flags *dataSourceRequestFlags,
) ([]string, error) {
	accessKeyID, accessSecrets, err := readSecretFileValue("access key id", credentials.AccessKeyID, credentials.AccessKeyIDFile, baseDir, required)
	if err != nil {
		return nil, err
	}
	secretKey, secretSecrets, err := readSecretFileValue("secret access key", credentials.SecretAccessKey, credentials.SecretAccessKeyFile, baseDir, required)
	if err != nil {
		return nil, err
	}
	sessionToken, sessionSecrets, err := readSecretFileValue("session token", credentials.SessionToken, credentials.SessionTokenFile, baseDir, false)
	if err != nil {
		return nil, err
	}
	flags.AccessKeyID = accessKeyID
	flags.SecretAccessKey = secretKey
	flags.SessionToken = sessionToken
	return append(append(accessSecrets, secretSecrets...), sessionSecrets...), nil
}

// readSecretFileValue resolves either an inline JSON secret or a JSON-referenced file path.
func readSecretFileValue(
	label string,
	secretValue string,
	filePath string,
	baseDir string,
	required bool,
) (string, []string, error) {
	filePath = strings.TrimSpace(filePath)
	if secretValue != "" && filePath != "" {
		return "", nil, xerror.InvalidInput(xerror.DataSource).Detail("set only one " + label + " source")
	}
	var value string
	switch {
	case secretValue != "":
		value = secretValue
	case filePath != "":
		path := filePath
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		path = filepath.Clean(path)
		// #nosec G304 -- the CLI intentionally reads user-selected credential files.
		data, err := os.ReadFile(path)
		if err != nil {
			return "", nil, xerror.Wrapf("read %s file: %w", label, err)
		}
		value = string(data)
		if value == "" {
			return "", nil, xerror.InvalidRequired(xerror.DataSource).Detail(label + " file is empty")
		}
	case required:
		return "", nil, xerror.InvalidRequired(xerror.DataSource).Detail("set " + label + " value or file path in --file")
	}
	if value == "" {
		return "", nil, nil
	}
	return value, []string{value}, nil
}

// rejectMixedFileInput keeps request-file semantics unambiguous.
func (f dataSourceRequestFlags) rejectMixedFileInput(values ...string) error {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return xerror.InvalidInput(xerror.DataSource).Detail("do not combine --file with data-source args or flags")
		}
	}
	if f.hasDirectInputFlags() {
		return xerror.InvalidInput(xerror.DataSource).Detail("do not combine --file with data-source args or flags")
	}
	return nil
}

// hasDirectInputFlags reports whether provider or credential flags were supplied outside --file.
func (f dataSourceRequestFlags) hasDirectInputFlags() bool {
	return f.Host != "" || f.Port != 0 || f.Database != "" || f.SSLMode != "" ||
		f.ConnectTimeout != 0 || f.AuthDatabase != "" || f.BaseURL != "" ||
		f.HealthPath != "" || f.TimeoutSeconds != 0 || f.OpenAPIURL != "" ||
		f.OpenAPIFetchTimeoutSeconds != 0 || f.ProjectID != "" || f.DefaultDataset != "" ||
		f.Location != "" || f.MaximumBytesBilled != 0 || f.Account != "" ||
		f.Warehouse != "" || f.Schema != "" || f.Role != "" || f.SnowflakeAuthType != "" || f.RepoURL != "" ||
		f.DefaultBranch != "" || f.BasePath != "" || f.Endpoint != "" || f.Region != "" ||
		f.Bucket != "" || f.Prefix != "" || f.ForcePathStyle != nil ||
		f.AllowPrivateHosts != nil || f.ReadOnly || f.ReadWrite || f.Username != "" ||
		f.Password != "" || f.BearerToken != "" || f.PrivateAppToken != "" ||
		f.ServiceAccountJSON != "" || f.GitAuthType != "" || f.GitToken != "" ||
		f.SSHPrivateKey != "" || f.SSHPassphrase != "" || f.SSHKnownHosts != "" ||
		f.AccessKeyID != "" || f.SecretAccessKey != "" || f.SessionToken != ""
}

// decodeStrictJSON rejects unknown fields and trailing JSON tokens for user-authored files.
func decodeStrictJSON(data []byte, value any, label string) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return xerror.InvalidInput(xerror.DataSource).Detailf("decode %s: %w", label, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return xerror.InvalidInput(xerror.DataSource).Detailf("decode %s: unexpected trailing JSON", label)
	}
	return nil
}

// stringValue unwraps optional generated string fields into the flag-builder shape.
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// intValue unwraps optional generated int fields into the flag-builder shape.
func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

// int64Value unwraps optional generated int64 fields into the flag-builder shape.
func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

// boolValue preserves explicit false booleans from JSON provider config.
func boolValue(value *bool) *bool {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
