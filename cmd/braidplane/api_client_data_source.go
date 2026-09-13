package main

import (
	"context"
	"net/http"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

// listDataSources fetches concrete organization data sources through the generated API client.
func (c *cliAPIClient) listDataSources(
	ctx context.Context,
	params *api.ListDataSourcesParams,
) (*api.DataSourceListResponse, error) {
	resp, err := c.openapi.ListDataSourcesWithResponse(ctx, c.organization, params)
	if err != nil {
		return nil, xerror.Wrapf("list data sources: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing data source list payload")
	}

	return resp.JSON200, nil
}

// getDataSource fetches one concrete organization data source by slug.
func (c *cliAPIClient) getDataSource(ctx context.Context, dataSourceSlug string) (*api.DataSource, error) {
	resp, err := c.openapi.GetDataSourceWithResponse(ctx, c.organization, dataSourceSlug)
	if err != nil {
		return nil, xerror.Wrapf("get data source: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing data source payload")
	}

	return resp.JSON200, nil
}

// createDataSource creates a concrete organization data source with provider-specific typed payloads.
func (c *cliAPIClient) createDataSource(
	ctx context.Context,
	body api.CreateDataSourceRequest,
) (*api.DataSource, error) {
	resp, err := c.openapi.CreateDataSourceWithResponse(ctx, c.organization, body)
	if err != nil {
		return nil, xerror.Wrapf("create data source: %w", err)
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON201 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing created data source payload")
	}

	return resp.JSON201, nil
}

// updateDataSource partially updates mutable concrete data-source metadata and config.
func (c *cliAPIClient) updateDataSource(
	ctx context.Context,
	dataSourceSlug string,
	body api.UpdateDataSourceRequest,
) (*api.DataSource, error) {
	resp, err := c.openapi.UpdateDataSourceWithResponse(ctx, c.organization, dataSourceSlug, body)
	if err != nil {
		return nil, xerror.Wrapf("update data source: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing updated data source payload")
	}

	return resp.JSON200, nil
}

// deleteDataSource soft-deletes one concrete organization data source.
func (c *cliAPIClient) deleteDataSource(ctx context.Context, dataSourceSlug string) error {
	resp, err := c.openapi.DeleteDataSourceWithResponse(ctx, c.organization, dataSourceSlug)
	if err != nil {
		return xerror.Wrapf("delete data source: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}

	return nil
}

// restoreDataSource restores one soft-deleted concrete organization data source.
func (c *cliAPIClient) restoreDataSource(ctx context.Context, dataSourceSlug string) error {
	resp, err := c.openapi.RestoreDataSourceWithResponse(ctx, c.organization, dataSourceSlug)
	if err != nil {
		return xerror.Wrapf("restore data source: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}

	return nil
}

// testDataSourceConnection tests draft config and credentials without persisting them.
func (c *cliAPIClient) testDataSourceConnection(
	ctx context.Context,
	body api.TestDataSourceConnectionRequest,
) error {
	resp, err := c.openapi.TestDataSourceConnectionWithResponse(ctx, c.organization, body)
	if err != nil {
		return xerror.Wrapf("test data source connection: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}

	return nil
}

// updateDataSourceCredentials replaces write-only credentials for one concrete data source.
func (c *cliAPIClient) updateDataSourceCredentials(
	ctx context.Context,
	dataSourceSlug string,
	body api.UpdateDataSourceCredentialsRequest,
) (*api.DataSource, error) {
	resp, err := c.openapi.UpdateDataSourceCredentialsWithResponse(ctx, c.organization, dataSourceSlug, body)
	if err != nil {
		return nil, xerror.Wrapf("update data source credentials: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing credential-updated data source payload")
	}

	return resp.JSON200, nil
}

// introspectDataSourceSchema fetches cached schema metadata for a concrete data source.
func (c *cliAPIClient) introspectDataSourceSchema(
	ctx context.Context,
	dataSourceSlug string,
) (*api.IntrospectSchemaResponse, error) {
	resp, err := c.openapi.IntrospectDataSourceSchemaWithResponse(ctx, c.organization, dataSourceSlug)
	if err != nil {
		return nil, xerror.Wrapf("introspect data source schema: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing data source schema payload")
	}

	return resp.JSON200, nil
}

// refreshDataSourceSchema queues fresh schema introspection for a concrete data source.
func (c *cliAPIClient) refreshDataSourceSchema(
	ctx context.Context,
	dataSourceSlug string,
) (*api.DataSourceSchemaImportResponse, error) {
	resp, err := c.openapi.RefreshDataSourceSchemaWithResponse(ctx, c.organization, dataSourceSlug)
	if err != nil {
		return nil, xerror.Wrapf("refresh data source schema: %w", err)
	}
	if resp.StatusCode() != http.StatusAccepted {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON202 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing data source schema import payload")
	}

	return resp.JSON202, nil
}
