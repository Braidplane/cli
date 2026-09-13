package main

import (
	"context"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

type invitationCommand struct {
	List   listInvitationsCommand  `cmd:"" help:"List invitations for the configured organization."`
	Get    getInvitationCommand    `cmd:"" help:"Get an organization invitation."`
	Create createInvitationCommand `cmd:"" help:"Invite an email address to the configured organization."`
	Resend resendInvitationCommand `cmd:"" help:"Resend a pending organization invitation."`
	Revoke revokeInvitationCommand `cmd:"" help:"Revoke an organization invitation."`
	Mine   myInvitationsCommand    `cmd:"" help:"Manage invitations for the authenticated user (PAT only)."`
}

type myInvitationsCommand struct {
	List    listMyInvitationsCommand   `cmd:"" help:"List invitations addressed to the authenticated user."`
	Accept  acceptMyInvitationCommand  `cmd:"" help:"Accept one invitation addressed to the authenticated user."`
	Decline declineMyInvitationCommand `cmd:"" help:"Decline one invitation addressed to the authenticated user."`
}

type listInvitationsCommand struct {
	apiAuthFlags
	iamPaginationFlags
	Status string `help:"Invitation status filter." name:"status"`
}

func (c *listInvitationsCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	if err := c.validate(func(value string) bool {
		return api.ListMemberInvitationsParamsSortBy(value).Valid()
	}); err != nil {
		return err
	}
	if c.Status != "" && !api.MemberInvitationStatus(c.Status).Valid() {
		return xerror.ValidationFailed().Detail("unsupported invitation status")
	}
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	params := api.ListMemberInvitationsParams{}
	setInvitationPagination(&params, c.iamPaginationFlags)
	if c.Status != "" {
		value := api.MemberInvitationStatusParam(c.Status)
		params.Status = &value
	}
	invitations, err := client.listMemberInvitations(context.Background(), &params)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, invitations)
}

func setInvitationPagination(params *api.ListMemberInvitationsParams, flags iamPaginationFlags) {
	if flags.Limit != 0 {
		value := flags.Limit
		params.Limit = &value
	}
	if flags.Cursor != "" {
		value := flags.Cursor
		params.Cursor = &value
	}
	if flags.SortBy != "" {
		value := api.ListMemberInvitationsParamsSortBy(flags.SortBy)
		params.SortBy = &value
	}
	if flags.Order != "" {
		value := api.ListMemberInvitationsParamsOrder(flags.Order)
		params.Order = &value
	}
	if flags.Direction != "" {
		value := api.ListMemberInvitationsParamsDirection(flags.Direction)
		params.Direction = &value
	}
}

type getInvitationCommand struct {
	apiAuthFlags
	InvitationSlug string `arg:"" name:"invitation-slug" help:"Invitation slug." required:""`
}

func (c *getInvitationCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	invitation, err := client.getMemberInvitation(context.Background(), c.InvitationSlug)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, invitation)
}

type createInvitationCommand struct {
	apiAuthFlags
	Email string `help:"Email address to invite." name:"email" required:""`
	Role  string `help:"Organization role slug." name:"role" required:""`
}

func (c *createInvitationCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	invitation, err := client.createMemberInvitation(context.Background(), api.NewMemberInvitation{Email: api.Email(c.Email), Role: c.Role})
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, invitation)
}

type resendInvitationCommand struct {
	apiAuthFlags
	InvitationSlug string `arg:"" name:"invitation-slug" help:"Invitation slug." required:""`
}

func (c *resendInvitationCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	invitation, err := client.resendMemberInvitation(context.Background(), c.InvitationSlug)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, invitation)
}

type revokeInvitationCommand struct {
	apiAuthFlags
	InvitationSlug string `arg:"" name:"invitation-slug" help:"Invitation slug." required:""`
}

func (c *revokeInvitationCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	invitation, err := client.revokeMemberInvitation(context.Background(), c.InvitationSlug)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, invitation)
}

type listMyInvitationsCommand struct {
	accountAuthFlags
	iamPaginationFlags
}

func (c *listMyInvitationsCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	if err := c.validate(func(value string) bool {
		return api.ListMyMemberInvitationsParamsSortBy(value).Valid()
	}); err != nil {
		return err
	}
	client, err := newAccountAPIClient(c.accountAuthFlags, runtime)
	if err != nil {
		return err
	}
	params := api.ListMyMemberInvitationsParams{}
	setMyInvitationPagination(&params, c.iamPaginationFlags)
	invitations, err := client.listMyMemberInvitations(context.Background(), &params)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, invitations)
}

func setMyInvitationPagination(params *api.ListMyMemberInvitationsParams, flags iamPaginationFlags) {
	if flags.Limit != 0 {
		value := flags.Limit
		params.Limit = &value
	}
	if flags.Cursor != "" {
		value := flags.Cursor
		params.Cursor = &value
	}
	if flags.SortBy != "" {
		value := api.ListMyMemberInvitationsParamsSortBy(flags.SortBy)
		params.SortBy = &value
	}
	if flags.Order != "" {
		value := api.ListMyMemberInvitationsParamsOrder(flags.Order)
		params.Order = &value
	}
	if flags.Direction != "" {
		value := api.ListMyMemberInvitationsParamsDirection(flags.Direction)
		params.Direction = &value
	}
}

type acceptMyInvitationCommand struct {
	accountAuthFlags
	OrganizationSlug     string `arg:"" name:"organization-slug" help:"Organization slug." required:""`
	InvitationSlug       string `arg:"" name:"invitation-slug" help:"Invitation slug." required:""`
	AcceptTerms          bool   `help:"Explicitly accept the displayed terms and privacy notice." required:""`
	TermsDocumentVersion string `help:"Version identifier of the displayed terms and privacy notice." required:""`
}

func (c *acceptMyInvitationCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	if !c.AcceptTerms {
		return xerror.ValidationFailed().Detail("--accept-terms is required")
	}
	client, err := newAccountAPIClient(c.accountAuthFlags, runtime)
	if err != nil {
		return err
	}
	acceptance, err := client.acceptMyMemberInvitation(
		context.Background(),
		c.OrganizationSlug,
		c.InvitationSlug,
		c.TermsDocumentVersion,
	)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, acceptance)
}

type declineMyInvitationCommand struct {
	accountAuthFlags
	OrganizationSlug string `arg:"" name:"organization-slug" help:"Organization slug." required:""`
	InvitationSlug   string `arg:"" name:"invitation-slug" help:"Invitation slug." required:""`
}

func (c *declineMyInvitationCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, err := newAccountAPIClient(c.accountAuthFlags, runtime)
	if err != nil {
		return err
	}
	invitation, err := client.declineMyMemberInvitation(context.Background(), c.OrganizationSlug, c.InvitationSlug)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, invitation)
}
