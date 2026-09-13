package main

import (
	"context"
	"net/http"
	"strings"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

func (c *cliAPIClient) listEnvironments(
	ctx context.Context,
	params *api.ListEnvironmentsParams,
) (*api.EnvironmentListResponse, error) {
	resp, err := c.openapi.ListEnvironmentsWithResponse(ctx, c.organization, params)
	if err != nil {
		return nil, xerror.Wrapf("list environments: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing environment list payload")
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) getEnvironment(ctx context.Context, environmentSlug string) (*api.Environment, error) {
	resp, err := c.openapi.GetEnvironmentWithResponse(ctx, c.organization, environmentSlug)
	if err != nil {
		return nil, xerror.Wrapf("get environment: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing environment payload")
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) createEnvironment(
	ctx context.Context,
	body api.NewEnvironment,
) (*api.Environment, error) {
	resp, err := c.openapi.CreateEnvironmentWithResponse(ctx, c.organization, body)
	if err != nil {
		return nil, xerror.Wrapf("create environment: %w", err)
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON201 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing created environment payload")
	}

	return resp.JSON201, nil
}

func (c *cliAPIClient) updateEnvironment(
	ctx context.Context,
	environmentSlug string,
	body api.UpdateEnvironment,
) (*api.Environment, error) {
	resp, err := c.openapi.UpdateEnvironmentWithResponse(ctx, c.organization, environmentSlug, body)
	if err != nil {
		return nil, xerror.Wrapf("update environment: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing updated environment payload")
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) deleteEnvironment(
	ctx context.Context,
	environmentSlug string,
	deleteReason string,
	confirmation string,
) error {
	params := &api.DeleteEnvironmentParams{}
	if reason := optionalString(deleteReason); reason != nil {
		params.DeleteReason = reason
	}
	if confirm := optionalAPISlug(confirmation); confirm != nil {
		params.ProtectedEnvironmentConfirmation = confirm
	}
	resp, err := c.openapi.DeleteEnvironmentWithResponse(ctx, c.organization, environmentSlug, params)
	if err != nil {
		return xerror.Wrapf("delete environment: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}

	return nil
}

func (c *cliAPIClient) listProjectEnvironments(
	ctx context.Context,
	projectSlug string,
	params *api.ListProjectEnvironmentsParams,
) (*api.ProjectEnvironmentListResponse, error) {
	resp, err := c.openapi.ListProjectEnvironmentsWithResponse(ctx, c.organization, projectSlug, params)
	if err != nil {
		return nil, xerror.Wrapf("list project environments: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing project environment list payload")
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) enableProjectEnvironment(
	ctx context.Context,
	projectSlug string,
	environmentSlug string,
	body api.EnableProjectEnvironmentRequest,
) (*api.ProjectEnvironment, error) {
	resp, err := c.openapi.EnableProjectEnvironmentWithResponse(
		ctx,
		c.organization,
		projectSlug,
		environmentSlug,
		body,
	)
	if err != nil {
		return nil, xerror.Wrapf("enable project environment: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing project environment payload")
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) disableProjectEnvironment(
	ctx context.Context,
	projectSlug string,
	environmentSlug string,
	confirmation string,
) error {
	params := &api.DisableProjectEnvironmentParams{}
	if confirm := optionalAPISlug(confirmation); confirm != nil {
		params.ProtectedEnvironmentConfirmation = confirm
	}
	resp, err := c.openapi.DisableProjectEnvironmentWithResponse(ctx, c.organization, projectSlug, environmentSlug, params)
	if err != nil {
		return xerror.Wrapf("disable project environment: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}

	return nil
}

func (c *cliAPIClient) listRuntimeVariables(
	ctx context.Context,
	environmentSlug string,
	params *api.ListEnvironmentRuntimeVariablesParams,
) (*api.RuntimeVariableListResponse, error) {
	resp, err := c.openapi.ListEnvironmentRuntimeVariablesWithResponse(ctx, c.organization, environmentSlug, params)
	if err != nil {
		return nil, xerror.Wrapf("list runtime variables: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing runtime variable list payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) getRuntimeVariable(
	ctx context.Context,
	environmentSlug string,
	key string,
) (*api.RuntimeVariable, error) {
	resp, err := c.openapi.GetEnvironmentRuntimeVariableWithResponse(ctx, c.organization, environmentSlug, key)
	if err != nil {
		return nil, xerror.Wrapf("get runtime variable: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing runtime variable payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) upsertRuntimeVariable(
	ctx context.Context,
	environmentSlug string,
	key string,
	body api.UpsertRuntimeVariable,
) (*api.RuntimeVariable, error) {
	resp, err := c.openapi.UpsertEnvironmentRuntimeVariableWithResponse(ctx, c.organization, environmentSlug, key, body)
	if err != nil {
		return nil, xerror.Wrapf("upsert runtime variable: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing runtime variable payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) deleteRuntimeVariable(
	ctx context.Context,
	environmentSlug string,
	key string,
	confirmation string,
) error {
	params := &api.DeleteEnvironmentRuntimeVariableParams{}
	if confirm := optionalAPISlug(confirmation); confirm != nil {
		params.ProtectedEnvironmentConfirmation = confirm
	}
	resp, err := c.openapi.DeleteEnvironmentRuntimeVariableWithResponse(ctx, c.organization, environmentSlug, key, params)
	if err != nil {
		return xerror.Wrapf("delete runtime variable: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}
	return nil
}

func (c *cliAPIClient) listDataSourceRefs(
	ctx context.Context,
	projectSlug string,
	params *api.ListDataSourceRefsParams,
) (*api.DataSourceRefListResponse, error) {
	resp, err := c.openapi.ListDataSourceRefsWithResponse(ctx, c.organization, projectSlug, params)
	if err != nil {
		return nil, xerror.Wrapf("list data-source refs: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing data-source ref list payload")
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) getDataSourceRef(
	ctx context.Context,
	projectSlug string,
	pipelineSlug string,
	refSlug string,
) (*api.DataSourceRef, error) {
	resp, err := c.openapi.GetDataSourceRefWithResponse(ctx, c.organization, projectSlug, pipelineSlug, refSlug)
	if err != nil {
		return nil, xerror.Wrapf("get data-source ref: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing data-source ref payload")
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) createDataSourceRef(
	ctx context.Context,
	projectSlug string,
	pipelineSlug string,
	body api.NewDataSourceRef,
) (*api.DataSourceRef, error) {
	resp, err := c.openapi.CreateDataSourceRefWithResponse(ctx, c.organization, projectSlug, pipelineSlug, body)
	if err != nil {
		return nil, xerror.Wrapf("create data-source ref: %w", err)
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON201 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing created data-source ref payload")
	}

	return resp.JSON201, nil
}

func (c *cliAPIClient) updateDataSourceRef(
	ctx context.Context,
	projectSlug string,
	pipelineSlug string,
	refSlug string,
	body api.UpdateDataSourceRef,
) (*api.DataSourceRef, error) {
	resp, err := c.openapi.UpdateDataSourceRefWithResponse(ctx, c.organization, projectSlug, pipelineSlug, refSlug, body)
	if err != nil {
		return nil, xerror.Wrapf("update data-source ref: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing updated data-source ref payload")
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) deleteDataSourceRef(
	ctx context.Context,
	projectSlug string,
	pipelineSlug string,
	refSlug string,
) error {
	resp, err := c.openapi.DeleteDataSourceRefWithResponse(ctx, c.organization, projectSlug, pipelineSlug, refSlug)
	if err != nil {
		return xerror.Wrapf("delete data-source ref: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}

	return nil
}

func (c *cliAPIClient) listDataSourceBindings(
	ctx context.Context,
	projectSlug string,
	environmentSlug string,
	params *api.ListDataSourceBindingsParams,
) (*api.DataSourceBindingListResponse, error) {
	resp, err := c.openapi.ListDataSourceBindingsWithResponse(ctx, c.organization, projectSlug, environmentSlug, params)
	if err != nil {
		return nil, xerror.Wrapf("list data-source bindings: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing data-source binding list payload")
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) getDataSourceBinding(
	ctx context.Context,
	projectSlug string,
	pipelineSlug string,
	environmentSlug string,
	refSlug string,
) (*api.DataSourceBinding, error) {
	resp, err := c.openapi.GetDataSourceBindingWithResponse(
		ctx,
		c.organization,
		projectSlug,
		pipelineSlug,
		environmentSlug,
		refSlug,
	)
	if err != nil {
		return nil, xerror.Wrapf("get data-source binding: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing data-source binding payload")
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) upsertDataSourceBinding(
	ctx context.Context,
	projectSlug string,
	pipelineSlug string,
	environmentSlug string,
	refSlug string,
	body api.UpsertDataSourceBinding,
) (*api.DataSourceBinding, error) {
	resp, err := c.openapi.UpsertDataSourceBindingWithResponse(
		ctx,
		c.organization,
		projectSlug,
		pipelineSlug,
		environmentSlug,
		refSlug,
		body,
	)
	if err != nil {
		return nil, xerror.Wrapf("upsert data-source binding: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing data-source binding payload")
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) deleteDataSourceBinding(
	ctx context.Context,
	projectSlug string,
	pipelineSlug string,
	environmentSlug string,
	refSlug string,
	confirmation string,
) error {
	params := &api.DeleteDataSourceBindingParams{}
	if confirm := optionalAPISlug(confirmation); confirm != nil {
		params.ProtectedEnvironmentConfirmation = confirm
	}
	resp, err := c.openapi.DeleteDataSourceBindingWithResponse(
		ctx,
		c.organization,
		projectSlug,
		pipelineSlug,
		environmentSlug,
		refSlug,
		params,
	)
	if err != nil {
		return xerror.Wrapf("delete data-source binding: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}

	return nil
}

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
