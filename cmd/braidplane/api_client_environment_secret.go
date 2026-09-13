package main

import (
	"context"
	"net/http"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

func (c *cliAPIClient) listEnvironmentSecrets(ctx context.Context, environment string, params *api.ListEnvironmentSecretsParams) (*api.EnvironmentSecretListResponse, error) {
	resp, err := c.openapi.ListEnvironmentSecretsWithResponse(ctx, c.organization, environment, params)
	if err != nil {
		return nil, xerror.Wrapf("list environment secrets: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing environment secret list payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) getEnvironmentSecret(ctx context.Context, environment, key string) (*api.EnvironmentSecret, error) {
	resp, err := c.openapi.GetEnvironmentSecretWithResponse(ctx, c.organization, environment, key)
	if err != nil {
		return nil, xerror.Wrapf("get environment secret: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing environment secret payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) upsertEnvironmentSecret(ctx context.Context, environment, key string, body api.UpsertEnvironmentSecret) (*api.EnvironmentSecret, error) {
	resp, err := c.openapi.UpsertEnvironmentSecretWithResponse(ctx, c.organization, environment, key, body)
	if err != nil {
		return nil, xerror.Wrapf("upsert environment secret: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing environment secret payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) deleteEnvironmentSecret(ctx context.Context, environment, key, confirmation string) error {
	params := &api.DeleteEnvironmentSecretParams{}
	if value := optionalAPISlug(confirmation); value != nil {
		params.ProtectedEnvironmentConfirmation = value
	}
	resp, err := c.openapi.DeleteEnvironmentSecretWithResponse(ctx, c.organization, environment, key, params)
	if err != nil {
		return xerror.Wrapf("delete environment secret: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}
	return nil
}
