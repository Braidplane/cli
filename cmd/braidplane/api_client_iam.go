package main

import (
	"context"
	"net/http"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

func (c *cliAPIClient) listMembers(ctx context.Context, params *api.ListMembersParams) (*api.MemberListResponse, error) {
	resp, err := c.openapi.ListMembersWithResponse(ctx, c.organization, params)
	if err != nil {
		return nil, xerror.Wrapf("list members: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing member list payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) createMember(ctx context.Context, body api.NewMember) (*api.Member, error) {
	resp, err := c.openapi.CreateMemberWithResponse(ctx, c.organization, body)
	if err != nil {
		return nil, xerror.Wrapf("add member: %w", err)
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON201 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing member payload")
	}
	return resp.JSON201, nil
}

func (c *cliAPIClient) updateMember(ctx context.Context, userSlug string, body api.UpdateMember) error {
	resp, err := c.openapi.UpdateMemberWithResponse(ctx, c.organization, userSlug, body)
	if err != nil {
		return xerror.Wrapf("update member: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}
	return nil
}

func (c *cliAPIClient) removeMember(ctx context.Context, userSlug string) error {
	resp, err := c.openapi.RemoveMemberWithResponse(ctx, c.organization, userSlug)
	if err != nil {
		return xerror.Wrapf("remove member: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}
	return nil
}

func (c *cliAPIClient) listMemberInvitations(ctx context.Context, params *api.ListMemberInvitationsParams) (*api.MemberInvitationListResponse, error) {
	resp, err := c.openapi.ListMemberInvitationsWithResponse(ctx, c.organization, params)
	if err != nil {
		return nil, xerror.Wrapf("list member invitations: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing invitation list payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) createMemberInvitation(ctx context.Context, body api.NewMemberInvitation) (*api.MemberInvitation, error) {
	resp, err := c.openapi.CreateMemberInvitationWithResponse(ctx, c.organization, body)
	if err != nil {
		return nil, xerror.Wrapf("create member invitation: %w", err)
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON201 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing invitation payload")
	}
	return resp.JSON201, nil
}

func (c *cliAPIClient) getMemberInvitation(ctx context.Context, invitation string) (*api.MemberInvitation, error) {
	resp, err := c.openapi.GetMemberInvitationWithResponse(ctx, c.organization, invitation)
	if err != nil {
		return nil, xerror.Wrapf("get member invitation: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing invitation payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) resendMemberInvitation(ctx context.Context, invitation string) (*api.MemberInvitation, error) {
	resp, err := c.openapi.ResendMemberInvitationWithResponse(ctx, c.organization, invitation)
	if err != nil {
		return nil, xerror.Wrapf("resend member invitation: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing invitation payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) revokeMemberInvitation(ctx context.Context, invitation string) (*api.MemberInvitation, error) {
	resp, err := c.openapi.RevokeMemberInvitationWithResponse(ctx, c.organization, invitation)
	if err != nil {
		return nil, xerror.Wrapf("revoke member invitation: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing invitation payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) listRoles(ctx context.Context, params *api.ListRolesParams) (*api.RoleListResponse, error) {
	resp, err := c.openapi.ListRolesWithResponse(ctx, c.organization, params)
	if err != nil {
		return nil, xerror.Wrapf("list roles: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing role list payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) createRole(ctx context.Context, body api.NewOrgRole) (*api.OrgRole, error) {
	resp, err := c.openapi.CreateRoleWithResponse(ctx, c.organization, body)
	if err != nil {
		return nil, xerror.Wrapf("create role: %w", err)
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON201 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing role payload")
	}
	return resp.JSON201, nil
}

func (c *cliAPIClient) getRole(ctx context.Context, role string) (*api.OrgRole, error) {
	resp, err := c.openapi.GetRoleWithResponse(ctx, c.organization, role)
	if err != nil {
		return nil, xerror.Wrapf("get role: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing role payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) updateRole(ctx context.Context, role string, body api.UpdateOrgRole) error {
	resp, err := c.openapi.UpdateRoleWithResponse(ctx, c.organization, role, body)
	if err != nil {
		return xerror.Wrapf("update role: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}
	return nil
}

func (c *cliAPIClient) archiveRole(ctx context.Context, role string) error {
	resp, err := c.openapi.DeleteRoleWithResponse(ctx, c.organization, role)
	if err != nil {
		return xerror.Wrapf("archive role: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}
	return nil
}

func (c *cliAPIClient) restoreRole(ctx context.Context, role string) error {
	resp, err := c.openapi.RestoreRoleWithResponse(ctx, c.organization, role)
	if err != nil {
		return xerror.Wrapf("restore role: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}
	return nil
}
