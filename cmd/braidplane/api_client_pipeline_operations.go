package main

import (
	"context"
	"net/http"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

func (c *cliAPIClient) listPipelineDeployments(ctx context.Context, project, environment string) (*api.PipelineDeploymentListResponse, error) {
	resp, err := c.openapi.ListPipelineDeploymentsWithResponse(ctx, c.organization, project, environment)
	if err != nil {
		return nil, xerror.Wrapf("list pipeline deployments: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing pipeline deployment list payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) getPipelineDeployment(ctx context.Context, project, environment, pipeline string) (*api.PipelineDeployment, error) {
	resp, err := c.openapi.GetPipelineDeploymentWithResponse(ctx, c.organization, project, environment, pipeline)
	if err != nil {
		return nil, xerror.Wrapf("get pipeline deployment: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing pipeline deployment payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) listPipelineValidationResults(ctx context.Context, project, environment string, params *api.ListPipelineValidationResultsParams) (*api.PipelineValidationResultListResponse, error) {
	resp, err := c.openapi.ListPipelineValidationResultsWithResponse(ctx, c.organization, project, environment, params)
	if err != nil {
		return nil, xerror.Wrapf("list pipeline validation results: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing validation result list payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) getPipelineValidationResult(ctx context.Context, project, environment, validation string) (*api.PipelineValidationResultRecord, error) {
	resp, err := c.openapi.GetPipelineValidationResultWithResponse(ctx, c.organization, project, environment, validation)
	if err != nil {
		return nil, xerror.Wrapf("get pipeline validation result: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing validation result payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) revalidatePipeline(ctx context.Context, project, environment, pipeline string) (*api.PipelineValidationResultRecord, error) {
	resp, err := c.openapi.RevalidatePipelineWithResponse(ctx, c.organization, project, environment, pipeline)
	if err != nil {
		return nil, xerror.Wrapf("revalidate pipeline: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing validation result payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) listDriftImpactReports(ctx context.Context, project string, params *api.ListDriftImpactReportsParams) (*api.DriftImpactReportListResponse, error) {
	resp, err := c.openapi.ListDriftImpactReportsWithResponse(ctx, c.organization, project, params)
	if err != nil {
		return nil, xerror.Wrapf("list drift impact reports: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing drift impact report list payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) getDriftImpactReport(ctx context.Context, project, report string) (*api.DriftImpactReport, error) {
	resp, err := c.openapi.GetDriftImpactReportWithResponse(ctx, c.organization, project, report)
	if err != nil {
		return nil, xerror.Wrapf("get drift impact report: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing drift impact report payload")
	}
	return resp.JSON200, nil
}
