package main

import (
	"bytes"
	"context"
	"net/http"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

func (c *cliAPIClient) listPersonalAccessTokens(
	ctx context.Context,
	params *api.ListPersonalAccessTokensParams,
) (*api.PersonalAccessTokenListResponse, error) {
	resp, err := c.openapi.ListPersonalAccessTokensWithResponse(ctx, params)
	if err != nil {
		return nil, xerror.Wrapf("list personal access tokens: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing personal access token list payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) createPersonalAccessToken(
	ctx context.Context,
	body api.NewPersonalAccessToken,
) (*api.PersonalAccessTokenWithToken, error) {
	resp, err := c.openapi.CreatePersonalAccessTokenWithResponse(ctx, body)
	if err != nil {
		return nil, xerror.Wrapf("create personal access token: %w", err)
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON201 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing created personal access token payload")
	}
	return resp.JSON201, nil
}

func (c *cliAPIClient) getPersonalAccessToken(
	ctx context.Context,
	patSlug string,
) (*api.PersonalAccessToken, error) {
	resp, err := c.openapi.GetPersonalAccessTokenWithResponse(ctx, patSlug)
	if err != nil {
		return nil, xerror.Wrapf("get personal access token: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing personal access token payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) updatePersonalAccessToken(
	ctx context.Context,
	patSlug string,
	body []byte,
) (*api.PersonalAccessToken, error) {
	resp, err := c.openapi.UpdatePersonalAccessTokenWithBodyWithResponse(
		ctx,
		patSlug,
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, xerror.Wrapf("update personal access token: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing updated personal access token payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) revokePersonalAccessToken(ctx context.Context, patSlug string) error {
	resp, err := c.openapi.DeletePersonalAccessTokenWithResponse(ctx, patSlug)
	if err != nil {
		return xerror.Wrapf("revoke personal access token: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}
	return nil
}

func (c *cliAPIClient) rotatePersonalAccessToken(
	ctx context.Context,
	patSlug string,
) (*api.PersonalAccessTokenWithToken, error) {
	resp, err := c.openapi.RotatePersonalAccessTokenWithResponse(ctx, patSlug)
	if err != nil {
		return nil, xerror.Wrapf("rotate personal access token: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing rotated personal access token payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) listAPIKeys(
	ctx context.Context,
	params *api.ListAPIKeysParams,
) (*api.APIKeyListResponse, error) {
	resp, err := c.openapi.ListAPIKeysWithResponse(ctx, c.organization, params)
	if err != nil {
		return nil, xerror.Wrapf("list API keys: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing API key list payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) createAPIKey(ctx context.Context, body api.NewAPIKey) (*api.APIKeyWithToken, error) {
	resp, err := c.openapi.CreateAPIKeyWithResponse(ctx, c.organization, body)
	if err != nil {
		return nil, xerror.Wrapf("create API key: %w", err)
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON201 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing created API key payload")
	}
	return resp.JSON201, nil
}

func (c *cliAPIClient) getAPIKey(ctx context.Context, apiKeySlug string) (*api.APIKey, error) {
	resp, err := c.openapi.GetAPIKeyWithResponse(ctx, c.organization, apiKeySlug)
	if err != nil {
		return nil, xerror.Wrapf("get API key: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing API key payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) updateAPIKey(ctx context.Context, apiKeySlug string, body []byte) (*api.APIKey, error) {
	resp, err := c.openapi.UpdateAPIKeyWithBodyWithResponse(
		ctx,
		c.organization,
		apiKeySlug,
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, xerror.Wrapf("update API key: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing updated API key payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) revokeAPIKey(ctx context.Context, apiKeySlug string) error {
	resp, err := c.openapi.DeleteAPIKeyWithResponse(ctx, c.organization, apiKeySlug)
	if err != nil {
		return xerror.Wrapf("revoke API key: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}
	return nil
}

func (c *cliAPIClient) rotateAPIKey(ctx context.Context, apiKeySlug string) (*api.APIKeyWithToken, error) {
	resp, err := c.openapi.RotateAPIKeyWithResponse(ctx, c.organization, apiKeySlug)
	if err != nil {
		return nil, xerror.Wrapf("rotate API key: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing rotated API key payload")
	}
	return resp.JSON200, nil
}
