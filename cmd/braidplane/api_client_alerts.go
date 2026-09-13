package main

import (
	"context"
	"net/http"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

// listAlertDestinations fetches tenant-scoped alert delivery destinations.
func (c *cliAPIClient) listAlertDestinations(
	ctx context.Context,
	params *api.ListAlertDestinationsParams,
) (*api.AlertDestinationListResponse, error) {
	resp, err := c.openapi.ListAlertDestinationsWithResponse(ctx, c.organization, params)
	if err != nil {
		return nil, xerror.Wrapf("list alert destinations: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing alert destination list payload")
	}

	return resp.JSON200, nil
}

// getAlertDestination fetches one alert delivery destination by slug.
func (c *cliAPIClient) getAlertDestination(ctx context.Context, destinationSlug string) (*api.AlertDestination, error) {
	resp, err := c.openapi.GetAlertDestinationWithResponse(ctx, c.organization, destinationSlug)
	if err != nil {
		return nil, xerror.Wrapf("get alert destination: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing alert destination payload")
	}

	return resp.JSON200, nil
}

// createAlertDestination persists a non-secret alert destination configuration.
func (c *cliAPIClient) createAlertDestination(
	ctx context.Context,
	body api.NewAlertDestination,
) (*api.AlertDestination, error) {
	resp, err := c.openapi.CreateAlertDestinationWithResponse(ctx, c.organization, body)
	if err != nil {
		return nil, xerror.Wrapf("create alert destination: %w", err)
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON201 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing created alert destination payload")
	}

	return resp.JSON201, nil
}

// updateAlertDestination fully replaces mutable alert destination fields.
func (c *cliAPIClient) updateAlertDestination(
	ctx context.Context,
	destinationSlug string,
	body api.UpdateAlertDestination,
) (*api.AlertDestination, error) {
	resp, err := c.openapi.UpdateAlertDestinationWithResponse(ctx, c.organization, destinationSlug, body)
	if err != nil {
		return nil, xerror.Wrapf("update alert destination: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing updated alert destination payload")
	}

	return resp.JSON200, nil
}

// deleteAlertDestination deletes one alert delivery destination by slug.
func (c *cliAPIClient) deleteAlertDestination(ctx context.Context, destinationSlug string) error {
	resp, err := c.openapi.DeleteAlertDestinationWithResponse(ctx, c.organization, destinationSlug)
	if err != nil {
		return xerror.Wrapf("delete alert destination: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}

	return nil
}

// testAlertDestination enqueues an ephemeral test notification through one destination.
func (c *cliAPIClient) testAlertDestination(
	ctx context.Context,
	destinationSlug string,
) (*api.AlertDestinationTestResponse, error) {
	resp, err := c.openapi.TestAlertDestinationWithResponse(ctx, c.organization, destinationSlug)
	if err != nil {
		return nil, xerror.Wrapf("test alert destination: %w", err)
	}
	if resp.StatusCode() != http.StatusAccepted {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON202 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing alert destination test payload")
	}

	return resp.JSON202, nil
}

// listAlertPolicies fetches tenant-scoped alert routing policies.
func (c *cliAPIClient) listAlertPolicies(
	ctx context.Context,
	params *api.ListAlertPoliciesParams,
) (*api.AlertPolicyListResponse, error) {
	resp, err := c.openapi.ListAlertPoliciesWithResponse(ctx, c.organization, params)
	if err != nil {
		return nil, xerror.Wrapf("list alert policies: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing alert policy list payload")
	}

	return resp.JSON200, nil
}

// getAlertPolicy fetches one alert routing policy by slug.
func (c *cliAPIClient) getAlertPolicy(ctx context.Context, policySlug string) (*api.AlertPolicy, error) {
	resp, err := c.openapi.GetAlertPolicyWithResponse(ctx, c.organization, policySlug)
	if err != nil {
		return nil, xerror.Wrapf("get alert policy: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing alert policy payload")
	}

	return resp.JSON200, nil
}

// createAlertPolicy persists one alert routing policy and destination bindings.
func (c *cliAPIClient) createAlertPolicy(ctx context.Context, body api.NewAlertPolicy) (*api.AlertPolicy, error) {
	resp, err := c.openapi.CreateAlertPolicyWithResponse(ctx, c.organization, body)
	if err != nil {
		return nil, xerror.Wrapf("create alert policy: %w", err)
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON201 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing created alert policy payload")
	}

	return resp.JSON201, nil
}

// updateAlertPolicy fully replaces mutable alert policy fields and destination bindings.
func (c *cliAPIClient) updateAlertPolicy(
	ctx context.Context,
	policySlug string,
	body api.UpdateAlertPolicy,
) (*api.AlertPolicy, error) {
	resp, err := c.openapi.UpdateAlertPolicyWithResponse(ctx, c.organization, policySlug, body)
	if err != nil {
		return nil, xerror.Wrapf("update alert policy: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing updated alert policy payload")
	}

	return resp.JSON200, nil
}

// deleteAlertPolicy deletes one alert routing policy by slug.
func (c *cliAPIClient) deleteAlertPolicy(ctx context.Context, policySlug string) error {
	resp, err := c.openapi.DeleteAlertPolicyWithResponse(ctx, c.organization, policySlug)
	if err != nil {
		return xerror.Wrapf("delete alert policy: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}

	return nil
}
