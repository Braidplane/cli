package main

import (
	"context"

	api "github.com/Braidplane/cli/internal/apiclient"
)

type memberCommand struct {
	List   listMembersCommand  `cmd:"" help:"List members of the configured organization."`
	Add    addMemberCommand    `cmd:"" help:"Add an existing user to the configured organization."`
	Update updateMemberCommand `cmd:"" help:"Change an organization member's role."`
	Remove removeMemberCommand `cmd:"" help:"Remove a user from the configured organization."`
}

type listMembersCommand struct {
	apiAuthFlags
	iamPaginationFlags
}

func (c *listMembersCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	if err := c.validate(func(value string) bool {
		return api.ListMembersParamsSortBy(value).Valid()
	}); err != nil {
		return err
	}
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	params := api.ListMembersParams{}
	setMemberPagination(&params, c.iamPaginationFlags)
	members, err := client.listMembers(context.Background(), &params)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, members)
}

func setMemberPagination(params *api.ListMembersParams, flags iamPaginationFlags) {
	if flags.Limit != 0 {
		value := flags.Limit
		params.Limit = &value
	}
	if flags.Cursor != "" {
		value := flags.Cursor
		params.Cursor = &value
	}
	if flags.SortBy != "" {
		value := api.ListMembersParamsSortBy(flags.SortBy)
		params.SortBy = &value
	}
	if flags.Order != "" {
		value := api.ListMembersParamsOrder(flags.Order)
		params.Order = &value
	}
	if flags.Direction != "" {
		value := api.ListMembersParamsDirection(flags.Direction)
		params.Direction = &value
	}
}

type addMemberCommand struct {
	apiAuthFlags
	UserSlug string `arg:"" name:"user-slug" help:"User slug." required:""`
	Role     string `help:"Organization role slug." name:"role" required:""`
}

func (c *addMemberCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	member, err := client.createMember(context.Background(), api.NewMember{UserSlug: c.UserSlug, Role: c.Role})
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, member)
}

type updateMemberCommand struct {
	apiAuthFlags
	UserSlug string `arg:"" name:"user-slug" help:"User slug." required:""`
	Role     string `help:"Organization role slug." name:"role" required:""`
}

func (c *updateMemberCommand) Run(_ *cliOutput, runtime *cliRuntime) error {
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	return client.updateMember(context.Background(), c.UserSlug, api.UpdateMember{Role: c.Role})
}

type removeMemberCommand struct {
	apiAuthFlags
	UserSlug string `arg:"" name:"user-slug" help:"User slug." required:""`
}

func (c *removeMemberCommand) Run(_ *cliOutput, runtime *cliRuntime) error {
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	return client.removeMember(context.Background(), c.UserSlug)
}
