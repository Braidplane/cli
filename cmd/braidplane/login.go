package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/Braidplane/cli/pkg/xerror"
)

type loginCommand struct {
	apiAuthFlags

	InsecureStoreAPIKeyInConfig bool   `help:"Store the selected API key or PAT in the user-only config file instead of the OS credential store. Intended for headless or unsupported environments." name:"insecure-store-api-key-in-config"`
	Environment                 string `help:"Default project environment slug to save for environment-aware pipeline commands." name:"environment" env:"BRAIDPLANE_ENVIRONMENT"`
}

func (c *loginCommand) Validate() error {
	missing := make([]string, 0, 2)
	if strings.TrimSpace(c.Organization) == "" {
		missing = append(missing, "--org")
	}
	if strings.TrimSpace(c.APIKey) == "" && strings.TrimSpace(c.PAT) == "" {
		missing = append(missing, "--api-key or --pat")
	}
	if len(missing) == 0 {
		return nil
	}

	return xerror.Unexpected().Detail("missing flags: " + strings.Join(missing, ", "))
}

func (c *loginCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	cfg := c.toConfig()
	cfg.Environment = c.Environment
	if strings.TrimSpace(cfg.PAT) != "" {
		cfg.APIKey = ""
	}
	if cfg.ServerURL == "" {
		cfg.ServerURL = defaultServerURL()
	}
	normalizedServerURL, err := normalizeAPIServerURL(cfg.ServerURL)
	if err != nil {
		return err
	}
	cfg.ServerURL = normalizedServerURL

	client, err := newCLIAPIClient(cfg)
	if err != nil {
		return err
	}
	if err := client.verifyOrganizationAccess(context.Background()); err != nil {
		return err
	}

	configPath, err := runtime.resolveConfigPath()
	if err != nil {
		return err
	}
	if c.InsecureStoreAPIKeyInConfig {
		if err := saveLogin(configPath, cfg); err != nil {
			return err
		}
	} else {
		if err := saveLoginWithCredentialStore(configPath, runtime.resolveCredentialStore(), cfg); err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(
		output.stdout,
		"Logged in to %s organization %s\n",
		cfg.ServerURL,
		cfg.Organization,
	)
	if err != nil {
		return xerror.Unexpected().With(err)
	}

	return nil
}
