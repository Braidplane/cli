package main

import (
	"context"
	"net/http"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

func (c *cliAPIClient) listDataSourceBranches(ctx context.Context, dataSource string) (*api.DataSourceBranchListResponse, error) {
	resp, err := c.openapi.ListDataSourceBranchesWithResponse(ctx, c.organization, dataSource)
	if err != nil {
		return nil, xerror.Wrapf("list data source branches: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing data source branch list payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) testStoredDataSourceConnection(ctx context.Context, dataSource string) error {
	resp, err := c.openapi.TestStoredDataSourceConnectionWithResponse(ctx, c.organization, dataSource)
	if err != nil {
		return xerror.Wrapf("test stored data source connection: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}
	return nil
}

func (c *cliAPIClient) startProviderDataSourceInstall(ctx context.Context, provider string, body api.StartProviderDataSourceInstallRequest) (*api.ProviderDataSourceInstall, error) {
	resp, err := c.openapi.StartProviderDataSourceInstallWithResponse(ctx, c.organization, provider, body)
	if err != nil {
		return nil, xerror.Wrapf("start provider data source install: %w", err)
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON201 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing provider data source install payload")
	}
	return resp.JSON201, nil
}

func (c *cliAPIClient) startProviderConnectorInstall(ctx context.Context, provider string, body api.StartProviderConnectorInstallRequest) (*api.ProviderConnectorInstall, error) {
	resp, err := c.openapi.StartProviderConnectorInstallWithResponse(ctx, c.organization, provider, body)
	if err != nil {
		return nil, xerror.Wrapf("start provider connector install: %w", err)
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON201 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing provider connector install payload")
	}
	return resp.JSON201, nil
}

func (c *cliAPIClient) refreshDataSourceProviderGrant(ctx context.Context, dataSource string) (*api.CredentialMetadata, error) {
	resp, err := c.openapi.RefreshDataSourceProviderGrantWithResponse(ctx, c.organization, dataSource)
	if err != nil {
		return nil, xerror.Wrapf("refresh data source provider grant: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing provider grant metadata")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) revokeDataSourceProviderGrant(ctx context.Context, dataSource string) (*api.CredentialMetadata, error) {
	resp, err := c.openapi.RevokeDataSourceProviderGrantWithResponse(ctx, c.organization, dataSource)
	if err != nil {
		return nil, xerror.Wrapf("revoke data source provider grant: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing provider grant metadata")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) rotateDataSourceProviderGrant(ctx context.Context, dataSource string) (*api.ProviderConnectorInstall, error) {
	resp, err := c.openapi.RotateDataSourceProviderGrantWithResponse(ctx, c.organization, dataSource)
	if err != nil {
		return nil, xerror.Wrapf("rotate data source provider grant: %w", err)
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON201 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing provider connector install payload")
	}
	return resp.JSON201, nil
}
