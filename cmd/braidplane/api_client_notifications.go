package main

import (
	"context"
	"net/http"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

// listNotifications fetches tenant-scoped notification records for operator review.
func (c *cliAPIClient) listNotifications(
	ctx context.Context,
	params *api.ListNotificationsParams,
) (*api.NotificationListResponse, error) {
	resp, err := c.openapi.ListNotificationsWithResponse(ctx, c.organization, params)
	if err != nil {
		return nil, xerror.Wrapf("list notifications: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing notification list payload")
	}

	return resp.JSON200, nil
}

// getNotification fetches one tenant-scoped notification record by slug.
func (c *cliAPIClient) getNotification(ctx context.Context, notificationSlug string) (*api.NotificationRecord, error) {
	resp, err := c.openapi.GetNotificationWithResponse(ctx, c.organization, notificationSlug)
	if err != nil {
		return nil, xerror.Wrapf("get notification: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing notification payload")
	}

	return resp.JSON200, nil
}

// acknowledgeNotification marks one notification as acknowledged and returns the updated record.
func (c *cliAPIClient) acknowledgeNotification(
	ctx context.Context,
	notificationSlug string,
) (*api.NotificationRecord, error) {
	resp, err := c.openapi.AcknowledgeNotificationWithResponse(ctx, c.organization, notificationSlug)
	if err != nil {
		return nil, xerror.Wrapf("acknowledge notification: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing acknowledged notification payload")
	}

	return resp.JSON200, nil
}

// resolveNotification marks one notification as resolved and returns the updated record.
func (c *cliAPIClient) resolveNotification(ctx context.Context, notificationSlug string) (*api.NotificationRecord, error) {
	resp, err := c.openapi.ResolveNotificationWithResponse(ctx, c.organization, notificationSlug)
	if err != nil {
		return nil, xerror.Wrapf("resolve notification: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing resolved notification payload")
	}

	return resp.JSON200, nil
}
