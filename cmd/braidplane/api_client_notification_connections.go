package main

import (
	"context"
	"net/http"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

func (c *cliAPIClient) listNotificationConnections(ctx context.Context) (*[]api.NotificationConnection, error) {
	resp, err := c.openapi.ListNotificationConnectionsWithResponse(ctx, c.organization)
	if err != nil {
		return nil, xerror.Wrapf("list notification connections: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing notification connections payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) createNotificationConnection(ctx context.Context, body api.NewNotificationConnection) (*api.NotificationConnection, error) {
	resp, err := c.openapi.CreateNotificationConnectionWithResponse(ctx, c.organization, body)
	if err != nil {
		return nil, xerror.Wrapf("create notification connection: %w", err)
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON201 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing created notification connection payload")
	}
	return resp.JSON201, nil
}

func (c *cliAPIClient) connectNotificationConnection(ctx context.Context, connection string) (*api.ProviderConnectorInstall, error) {
	resp, err := c.openapi.ConnectNotificationConnectionWithResponse(ctx, c.organization, connection)
	if err != nil {
		return nil, xerror.Wrapf("connect notification connection: %w", err)
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON201 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing notification authorization payload")
	}
	return resp.JSON201, nil
}

func (c *cliAPIClient) disconnectNotificationConnection(ctx context.Context, connection string) (*api.CredentialMetadata, error) {
	resp, err := c.openapi.DisconnectNotificationConnectionWithResponse(ctx, c.organization, connection)
	if err != nil {
		return nil, xerror.Wrapf("disconnect notification connection: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing disconnected notification connection payload")
	}
	return resp.JSON200, nil
}
