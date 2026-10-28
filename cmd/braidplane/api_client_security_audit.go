package main

import (
	"context"
	"net/http"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

func (c *cliAPIClient) listSecurityAuditEvents(ctx context.Context, params *api.ListSecurityAuditEventsParams) (*api.EventListResponse, error) {
	resp, err := c.openapi.ListSecurityAuditEventsWithResponse(ctx, c.organization, params)
	if err != nil {
		return nil, xerror.Wrapf("list security audit events: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing security audit events payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) exportSecurityAuditEvents(ctx context.Context, params *api.ExportSecurityAuditEventsParams) ([]byte, error) {
	resp, err := c.openapi.ExportSecurityAuditEventsWithResponse(ctx, c.organization, params)
	if err != nil {
		return nil, xerror.Wrapf("export security audit events: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	return resp.Body, nil
}
