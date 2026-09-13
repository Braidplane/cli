package main

import (
	"context"
	"os"
	"strings"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

type environmentSecretCommand struct {
	List   listEnvironmentSecretsCommand  `cmd:"" help:"List safe environment secret metadata."`
	Get    getEnvironmentSecretCommand    `cmd:"" help:"Get safe metadata for one environment secret."`
	Upsert upsertEnvironmentSecretCommand `cmd:"" help:"Create or replace one write-only environment secret."`
	Delete deleteEnvironmentSecretCommand `cmd:"" help:"Delete one environment secret and revoke its encrypted payload."`
}

type environmentSecretFlags struct {
	apiAuthFlags
	EnvironmentSlug string `help:"Organization environment slug." name:"environment" env:"BRAIDPLANE_ENVIRONMENT" required:""`
}

type listEnvironmentSecretsCommand struct {
	environmentSecretFlags
	Limit     int32  `help:"Maximum number of secret metadata records to return." name:"limit"`
	Cursor    string `help:"Pagination cursor from a previous list response." name:"cursor"`
	Order     string `help:"Sort order: asc or desc." name:"order"`
	Direction string `help:"Pagination direction: next or prev." name:"direction"`
}

func (c *listEnvironmentSecretsCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	params, err := listEnvironmentSecretsParams(c.Limit, c.Cursor, c.Order, c.Direction)
	if err != nil {
		return err
	}
	response, err := client.listEnvironmentSecrets(context.Background(), c.EnvironmentSlug, params)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, response)
}

type getEnvironmentSecretCommand struct {
	environmentSecretFlags
	Key string `arg:"" name:"key" help:"Environment secret key." required:""`
}

func (c *getEnvironmentSecretCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	secret, err := client.getEnvironmentSecret(context.Background(), c.EnvironmentSlug, c.Key)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, secret)
}

type upsertEnvironmentSecretCommand struct {
	environmentSecretFlags
	Key         string `arg:"" name:"key" help:"Environment secret key." required:""`
	Value       string `help:"Plaintext secret value. Prefer --value-file to avoid shell history." name:"value"`
	ValueFile   string `help:"Path to a file containing the plaintext secret value." name:"value-file" type:"path"`
	Description string `help:"Optional operator description." name:"description"`
	Confirm     string `help:"Exact environment slug required for protected environments." name:"confirm-environment"`
}

func (c *upsertEnvironmentSecretCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	value, err := environmentSecretValue(c.Value, c.ValueFile)
	if err != nil {
		return err
	}
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	body := api.UpsertEnvironmentSecret{Value: &value, ProtectedEnvironmentConfirmation: optionalAPISlug(c.Confirm)}
	if description := strings.TrimSpace(c.Description); description != "" {
		body.Description = &description
	}
	secret, err := client.upsertEnvironmentSecret(context.Background(), c.EnvironmentSlug, c.Key, body)
	if err != nil {
		return redactCredentialError(err, []string{value})
	}
	return writeCLIJSON(output.stdout, secret)
}

func environmentSecretValue(inline, file string) (string, error) {
	if inline != "" && strings.TrimSpace(file) != "" {
		return "", xerror.InvalidInput(xerror.Environment).Detail("only one of --value or --value-file can be set")
	}
	if strings.TrimSpace(file) != "" {
		contents, err := os.ReadFile(strings.TrimSpace(file))
		if err != nil {
			return "", xerror.Wrapf("read environment secret value file: %w", err)
		}
		if len(contents) == 0 {
			return "", xerror.InvalidInput(xerror.Environment).Detail("environment secret value file must not be empty")
		}
		return string(contents), nil
	}
	if inline == "" {
		return "", xerror.InvalidInput(xerror.Environment).Detail("one of --value or --value-file is required")
	}
	return inline, nil
}

type deleteEnvironmentSecretCommand struct {
	environmentSecretFlags
	Key     string `arg:"" name:"key" help:"Environment secret key." required:""`
	Confirm string `help:"Exact environment slug required for protected environments." name:"confirm-environment"`
}

func (c *deleteEnvironmentSecretCommand) Run(_ *cliOutput, runtime *cliRuntime) error {
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	return client.deleteEnvironmentSecret(context.Background(), c.EnvironmentSlug, c.Key, c.Confirm)
}

func listEnvironmentSecretsParams(limit int32, cursor, order, direction string) (*api.ListEnvironmentSecretsParams, error) {
	params := &api.ListEnvironmentSecretsParams{}
	if limit < 0 || limit > 100 {
		return nil, xerror.InvalidInput(xerror.Environment).Detail("limit must be between 1 and 100")
	}
	if limit > 0 {
		value := limit
		params.Limit = &value
	}
	if cursor != "" {
		value := cursor
		params.Cursor = &value
	}
	if order != "" {
		value := api.ListEnvironmentSecretsParamsOrder(strings.ToLower(strings.TrimSpace(order)))
		if !value.Valid() {
			return nil, xerror.InvalidInput(xerror.Environment).Detail("order must be asc or desc")
		}
		params.Order = &value
	}
	if direction != "" {
		value := api.ListEnvironmentSecretsParamsDirection(strings.ToLower(strings.TrimSpace(direction)))
		if !value.Valid() {
			return nil, xerror.InvalidInput(xerror.Environment).Detail("direction must be next or prev")
		}
		params.Direction = &value
	}
	return params, nil
}
