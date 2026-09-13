package main

import (
	"context"

	api "github.com/Braidplane/cli/internal/apiclient"
)

type userCommand struct {
	Profile             userProfileCommand         `cmd:"" help:"Manage the authenticated user's profile (PAT only)."`
	PersonalAccessToken personalAccessTokenCommand `cmd:"" name:"personal-access-token" help:"Manage personal access tokens using an explicitly opted-in PAT."`
}

type userProfileCommand struct {
	Get    getUserCommand    `cmd:"" help:"Get the authenticated user's profile."`
	Update updateUserCommand `cmd:"" help:"Update non-security fields on the authenticated user's profile."`
}

type getUserCommand struct{ accountAuthFlags }

func (c *getUserCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, err := newAccountAPIClient(c.accountAuthFlags, runtime)
	if err != nil {
		return err
	}
	user, err := client.getUserProfile(context.Background())
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, user)
}

type updateUserCommand struct {
	accountAuthFlags

	FullName  *string `help:"Full display name." name:"full-name"`
	Locale    *string `help:"Preferred BCP 47 locale." name:"locale"`
	AvatarURL *string `help:"Avatar URL; pass an empty value to clear." name:"avatar-url"`
	Phone     *string `help:"Phone number; pass an empty value to clear." name:"phone-number"`
	JobTitle  *string `help:"Job title; pass an empty value to clear." name:"job-title"`
	Timezone  *string `help:"IANA timezone; pass an empty value to clear." name:"timezone"`
}

func (c *updateUserCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, err := newAccountAPIClient(c.accountAuthFlags, runtime)
	if err != nil {
		return err
	}

	user, err := client.updateUserProfile(context.Background(), api.UpdateUserProfile{
		FullName:    c.FullName,
		Locale:      c.Locale,
		AvatarUrl:   c.AvatarURL,
		PhoneNumber: c.Phone,
		JobTitle:    c.JobTitle,
		Timezone:    c.Timezone,
	})
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, user)
}
