package main

import (
	"context"
	"net/http"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

func (c *cliAPIClient) queryExecutionSummary(ctx context.Context) (*api.QueryExecutionSummary, error) {
	resp, err := c.openapi.GetQueryExecutionSummaryWithResponse(ctx, c.organization)
	if err != nil {
		return nil, xerror.Wrapf("get query execution summary: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("missing query execution summary payload")
	}
	return resp.JSON200, nil
}

// listQueries fetches saved queries for one organization-owned concrete data source.
func (c *cliAPIClient) listQueries(
	ctx context.Context,
	dataSourceSlug string,
	params *api.ListQueriesParams,
) (*api.QueryListResponse, error) {
	resp, err := c.openapi.ListQueriesWithResponse(ctx, c.organization, dataSourceSlug, params)
	if err != nil {
		return nil, xerror.Wrapf("list queries: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing query list payload")
	}

	return resp.JSON200, nil
}

// getQuery fetches one saved query from an organization-owned concrete data source.
func (c *cliAPIClient) getQuery(ctx context.Context, dataSourceSlug, querySlug string) (*api.Query, error) {
	resp, err := c.openapi.GetQueryWithResponse(ctx, c.organization, dataSourceSlug, querySlug)
	if err != nil {
		return nil, xerror.Wrapf("get query: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing query payload")
	}

	return resp.JSON200, nil
}

// createQuery creates one saved query under an organization-owned concrete data source.
func (c *cliAPIClient) createQuery(
	ctx context.Context,
	dataSourceSlug string,
	body api.NewQuery,
) (*api.Query, error) {
	resp, err := c.openapi.CreateQueryWithResponse(ctx, c.organization, dataSourceSlug, body)
	if err != nil {
		return nil, xerror.Wrapf("create query: %w", err)
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON201 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing created query payload")
	}

	return resp.JSON201, nil
}

// updateQuery fully replaces one saved query's mutable fields.
func (c *cliAPIClient) updateQuery(
	ctx context.Context,
	dataSourceSlug string,
	querySlug string,
	body api.UpdateQuery,
) (*api.Query, error) {
	resp, err := c.openapi.UpdateQueryWithResponse(ctx, c.organization, dataSourceSlug, querySlug, body)
	if err != nil {
		return nil, xerror.Wrapf("update query: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing updated query payload")
	}

	return resp.JSON200, nil
}

// deleteQuery soft-deletes one saved query.
func (c *cliAPIClient) deleteQuery(ctx context.Context, dataSourceSlug, querySlug string) error {
	resp, err := c.openapi.DeleteQueryWithResponse(ctx, c.organization, dataSourceSlug, querySlug)
	if err != nil {
		return xerror.Wrapf("delete query: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}

	return nil
}

// restoreQuery restores one soft-deleted saved query.
func (c *cliAPIClient) restoreQuery(ctx context.Context, dataSourceSlug, querySlug string) error {
	resp, err := c.openapi.RestoreQueryWithResponse(ctx, c.organization, dataSourceSlug, querySlug)
	if err != nil {
		return xerror.Wrapf("restore query: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}

	return nil
}

// purgeQuery permanently removes one soft-deleted saved query and its execution history.
func (c *cliAPIClient) purgeQuery(ctx context.Context, dataSourceSlug, querySlug string) error {
	resp, err := c.openapi.PurgeQueryWithResponse(ctx, c.organization, dataSourceSlug, querySlug)
	if err != nil {
		return xerror.Wrapf("purge query: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}

	return nil
}

// executeQuery executes one saved query and returns the API execution result envelope.
func (c *cliAPIClient) executeQuery(
	ctx context.Context,
	dataSourceSlug string,
	querySlug string,
	body api.ExecuteQueryRequest,
) (*api.QueryExecutionResult, error) {
	resp, err := c.openapi.ExecuteQueryWithResponse(ctx, c.organization, dataSourceSlug, querySlug, body)
	if err != nil {
		return nil, xerror.Wrapf("execute query: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing query execution result payload")
	}

	return resp.JSON200, nil
}

// listQueryExecutions fetches persisted execution history for one saved query.
func (c *cliAPIClient) listQueryExecutions(
	ctx context.Context,
	dataSourceSlug string,
	querySlug string,
	params *api.ListQueryExecutionsParams,
) (*api.QueryExecutionListResponse, error) {
	resp, err := c.openapi.ListQueryExecutionsWithResponse(ctx, c.organization, dataSourceSlug, querySlug, params)
	if err != nil {
		return nil, xerror.Wrapf("list query executions: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing query execution list payload")
	}

	return resp.JSON200, nil
}

// getQueryExecution fetches one persisted query execution record.
func (c *cliAPIClient) getQueryExecution(
	ctx context.Context,
	dataSourceSlug string,
	querySlug string,
	executionSlug string,
) (*api.QueryExecutionRecord, error) {
	resp, err := c.openapi.GetQueryExecutionWithResponse(
		ctx,
		c.organization,
		dataSourceSlug,
		querySlug,
		executionSlug,
	)
	if err != nil {
		return nil, xerror.Wrapf("get query execution: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing query execution payload")
	}

	return resp.JSON200, nil
}
