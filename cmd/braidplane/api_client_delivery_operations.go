package main

import (
	"context"
	"net/http"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

func (c *cliAPIClient) listNotificationDeliveryAttempts(ctx context.Context, notification string, params *api.ListNotificationDeliveryAttemptsParams) (*api.AlertDeliveryAttemptListResponse, error) {
	resp, err := c.openapi.ListNotificationDeliveryAttemptsWithResponse(ctx, c.organization, notification, params)
	if err != nil {
		return nil, xerror.Wrapf("list notification delivery attempts: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing delivery attempt list payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) listAlertDeliveryAttempts(ctx context.Context, params *api.ListAlertDeliveryAttemptsParams) (*api.AlertDeliveryAttemptListResponse, error) {
	resp, err := c.openapi.ListAlertDeliveryAttemptsWithResponse(ctx, c.organization, params)
	if err != nil {
		return nil, xerror.Wrapf("list alert delivery attempts: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing delivery attempt list payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) listAlertPolicySourceMetadata(ctx context.Context, params *api.ListAlertPolicySourceMetadataParams) (*api.AlertPolicySourceMetadataResponse, error) {
	resp, err := c.openapi.ListAlertPolicySourceMetadataWithResponse(ctx, c.organization, params)
	if err != nil {
		return nil, xerror.Wrapf("list alert policy source metadata: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing alert policy source metadata payload")
	}
	return resp.JSON200, nil
}
