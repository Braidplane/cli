package main

import (
	"context"
	"net/http"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

func (c *cliAPIClient) listODCSContracts(ctx context.Context, project string, params *api.ListODCSContractsParams) (*api.ODCSContractListResponse, error) {
	resp, err := c.openapi.ListODCSContractsWithResponse(ctx, c.organization, project, params)
	if err != nil {
		return nil, xerror.Wrapf("list ODCS contracts: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing ODCS contract list payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) createODCSContract(ctx context.Context, project string, body api.NewODCSContract) (*api.ODCSContract, error) {
	resp, err := c.openapi.CreateODCSContractWithResponse(ctx, c.organization, project, body)
	if err != nil {
		return nil, xerror.Wrapf("create ODCS contract: %w", err)
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON201 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing created ODCS contract payload")
	}
	return resp.JSON201, nil
}

func (c *cliAPIClient) getODCSContract(ctx context.Context, project, contract string) (*api.ODCSContract, error) {
	resp, err := c.openapi.GetODCSContractWithResponse(ctx, c.organization, project, contract)
	if err != nil {
		return nil, xerror.Wrapf("get ODCS contract: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing ODCS contract payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) updateODCSContract(ctx context.Context, project, contract string, body api.UpdateODCSContract) (*api.ODCSContract, error) {
	resp, err := c.openapi.UpdateODCSContractWithResponse(ctx, c.organization, project, contract, body)
	if err != nil {
		return nil, xerror.Wrapf("update ODCS contract: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing updated ODCS contract payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) listODCSRevisions(ctx context.Context, project, contract string, params *api.ListODCSRevisionsParams) (*api.ODCSRevisionListResponse, error) {
	resp, err := c.openapi.ListODCSRevisionsWithResponse(ctx, c.organization, project, contract, params)
	if err != nil {
		return nil, xerror.Wrapf("list ODCS revisions: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing ODCS revision list payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) createODCSRevision(ctx context.Context, project, contract string, body api.NewODCSRevision) (*api.ODCSRevision, error) {
	resp, err := c.openapi.CreateODCSRevisionWithResponse(ctx, c.organization, project, contract, body)
	if err != nil {
		return nil, xerror.Wrapf("create ODCS revision: %w", err)
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON201 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing created ODCS revision payload")
	}
	return resp.JSON201, nil
}

func (c *cliAPIClient) getODCSRevision(ctx context.Context, project, contract, revision string) (*api.ODCSRevision, error) {
	resp, err := c.openapi.GetODCSRevisionWithResponse(ctx, c.organization, project, contract, revision)
	if err != nil {
		return nil, xerror.Wrapf("get ODCS revision: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing ODCS revision payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) previewODCSRevisionImpact(ctx context.Context, project, contract string, body api.ODCSRevisionImpactRequest) (*api.ODCSRevisionImpact, error) {
	resp, err := c.openapi.PreviewODCSRevisionImpactWithResponse(ctx, c.organization, project, contract, body)
	if err != nil {
		return nil, xerror.Wrapf("preview ODCS revision impact: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing ODCS revision impact payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) inspectODCSRevision(ctx context.Context, project, contract, revision, object string) (*api.ODCSRevisionInspection, error) {
	resp, err := c.openapi.InspectODCSRevisionWithResponse(ctx, c.organization, project, contract, revision, &api.InspectODCSRevisionParams{SchemaObjectId: &object})
	if err != nil {
		return nil, xerror.Wrapf("inspect ODCS revision: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing ODCS inspection payload")
	}
	return resp.JSON200, nil
}
