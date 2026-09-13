package main

import (
	"context"
	"net/http"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

func newAccountAPIClient(flags accountAuthFlags, runtime *cliRuntime) (*cliAPIClient, error) {
	cfg, err := flags.resolveAuthConfig(runtime)
	if err != nil {
		return nil, err
	}
	return newCLIAPIClient(cfg)
}

func (c *cliAPIClient) getUserProfile(ctx context.Context) (*api.User, error) {
	resp, err := c.openapi.GetUsersProfileWithResponse(ctx)
	if err != nil {
		return nil, xerror.Wrapf("get user profile: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing user profile payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) updateUserProfile(ctx context.Context, body api.UpdateUserProfile) (*api.User, error) {
	resp, err := c.openapi.PatchUsersProfileWithResponse(ctx, body)
	if err != nil {
		return nil, xerror.Wrapf("update user profile: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing user profile payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) listOrganizations(ctx context.Context, params *api.ListOrganizationsParams) (*api.OrganizationListResponse, error) {
	resp, err := c.openapi.ListOrganizationsWithResponse(ctx, params)
	if err != nil {
		return nil, xerror.Wrapf("list organizations: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing organization list payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) createOrganization(ctx context.Context, body api.NewOrganization) (*api.Organization, error) {
	resp, err := c.openapi.CreateOrganizationWithResponse(ctx, body)
	if err != nil {
		return nil, xerror.Wrapf("create organization: %w", err)
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON201 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing organization payload")
	}
	return resp.JSON201, nil
}

func (c *cliAPIClient) getOrganization(ctx context.Context, organization string) (*api.Organization, error) {
	resp, err := c.openapi.GetOrganizationWithResponse(ctx, organization)
	if err != nil {
		return nil, xerror.Wrapf("get organization: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing organization payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) updateOrganization(ctx context.Context, body api.UpdateOrganization) error {
	resp, err := c.openapi.UpdateOrganizationWithResponse(ctx, c.organization, body)
	if err != nil {
		return xerror.Wrapf("update organization: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}
	return nil
}

func (c *cliAPIClient) listMyMemberInvitations(ctx context.Context, params *api.ListMyMemberInvitationsParams) (*api.MemberInvitationListResponse, error) {
	resp, err := c.openapi.ListMyMemberInvitationsWithResponse(ctx, params)
	if err != nil {
		return nil, xerror.Wrapf("list my member invitations: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing invitation list payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) acceptMyMemberInvitation(
	ctx context.Context,
	organization, invitation, termsDocumentVersion string,
) (*api.MemberInvitationAcceptance, error) {
	resp, err := c.openapi.AcceptMyMemberInvitationWithResponse(ctx, organization, invitation, api.TermsAcceptanceRequest{
		TermsAccepted:        true,
		TermsDocumentVersion: termsDocumentVersion,
	})
	if err != nil {
		return nil, xerror.Wrapf("accept member invitation: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing invitation acceptance payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) declineMyMemberInvitation(ctx context.Context, organization, invitation string) (*api.MemberInvitation, error) {
	resp, err := c.openapi.DeclineMyMemberInvitationWithResponse(ctx, organization, invitation)
	if err != nil {
		return nil, xerror.Wrapf("decline member invitation: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing invitation payload")
	}
	return resp.JSON200, nil
}
