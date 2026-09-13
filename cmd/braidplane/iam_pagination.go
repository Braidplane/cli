package main

import "github.com/Braidplane/cli/pkg/xerror"

type iamPaginationFlags struct {
	Limit     int32  `help:"Maximum number of items to return (1-100)." name:"limit"`
	Cursor    string `help:"Pagination cursor from a previous list response." name:"cursor"`
	SortBy    string `help:"Sort field using the API resource prefix, for example organization_name, member_email, invitation_created_at, or role_updated_at." name:"sort-by"`
	Order     string `help:"Sort order: asc or desc." name:"order"`
	Direction string `help:"Pagination direction: next or prev." name:"direction"`
}

func (f iamPaginationFlags) validate(validSort func(string) bool) error {
	if f.Limit < 0 || f.Limit > 100 {
		return xerror.ValidationFailed().Detail("limit must be between 1 and 100")
	}
	if f.SortBy != "" && !validSort(f.SortBy) {
		return xerror.ValidationFailed().Detail("unsupported sort-by value")
	}
	if f.Order != "" && f.Order != "asc" && f.Order != "desc" {
		return xerror.ValidationFailed().Detail("order must be asc or desc")
	}
	if f.Direction != "" && f.Direction != "next" && f.Direction != "prev" {
		return xerror.ValidationFailed().Detail("direction must be next or prev")
	}
	return nil
}
