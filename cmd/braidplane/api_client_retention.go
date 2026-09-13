package main

import (
	"context"
	"net/http"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

func (c *cliAPIClient) getOrganizationRetentionPolicy(
	ctx context.Context,
) (*api.OrganizationRetentionPolicy, error) {
	resp, err := c.openapi.GetOrganizationRetentionPolicyWithResponse(ctx, c.organization)
	if err != nil {
		return nil, xerror.Wrapf("get organization retention policy: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing organization retention policy")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) updateOrganizationRetentionPolicy(
	ctx context.Context,
	body api.OrganizationRetentionPolicy,
) (*api.OrganizationRetentionPolicy, error) {
	resp, err := c.openapi.UpdateOrganizationRetentionPolicyWithResponse(ctx, c.organization, body)
	if err != nil {
		return nil, xerror.Wrapf("update organization retention policy: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing updated organization retention policy")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) listOrganizationRetentionRuns(
	ctx context.Context,
) (*api.OrganizationRetentionRunList, error) {
	resp, err := c.openapi.ListOrganizationRetentionRunsWithResponse(ctx, c.organization)
	if err != nil {
		return nil, xerror.Wrapf("list organization retention maintenance runs: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing organization retention maintenance history")
	}
	return resp.JSON200, nil
}
