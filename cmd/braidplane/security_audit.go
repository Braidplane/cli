package main

import (
	"context"
	"time"
	"unicode/utf8"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/slug"
	"github.com/Braidplane/cli/pkg/xerror"
)

type securityAuditCommand struct {
	List   listSecurityAuditCommand   `cmd:"" help:"List retained security audit events as JSON."`
	Export exportSecurityAuditCommand `cmd:"" help:"Export up to 10,000 retained security audit events as CSV to stdout."`
}

type securityAuditFilters struct {
	ActorType      *api.EventActorType  `name:"actor-type" help:"Filter by actor type."`
	ActorSlug      *string              `name:"actor-slug" help:"Filter by actor slug."`
	EntityType     *api.EventEntityType `name:"entity-type" help:"Filter by entity type."`
	EntitySlug     *string              `name:"entity-slug" help:"Filter by entity slug."`
	Action         *api.EventAction     `name:"action" help:"Filter by action."`
	RequestID      *string              `name:"request-id" help:"Filter by request correlation identifier."`
	OccurredFrom   *time.Time           `name:"occurred-from" format:"2006-01-02T15:04:05Z07:00" help:"Include events at or after this RFC3339 timestamp."`
	OccurredBefore *time.Time           `name:"occurred-before" format:"2006-01-02T15:04:05Z07:00" help:"Include events strictly before this RFC3339 timestamp."`
}

func (f *securityAuditFilters) params() (api.ExportSecurityAuditEventsParams, error) {
	p := api.ExportSecurityAuditEventsParams{ActorType: f.ActorType, ActorSlug: f.ActorSlug, EntityType: f.EntityType, EntitySlug: f.EntitySlug, Action: f.Action, RequestId: f.RequestID, OccurredFrom: f.OccurredFrom, OccurredBefore: f.OccurredBefore}
	if f.ActorType != nil && !f.ActorType.Valid() {
		return p, xerror.ValidationFailed().Detail("invalid actor-type")
	}
	if f.EntityType != nil && !f.EntityType.Valid() {
		return p, xerror.ValidationFailed().Detail("invalid entity-type")
	}
	if f.Action != nil && !f.Action.Valid() {
		return p, xerror.ValidationFailed().Detail("invalid action")
	}
	if f.ActorSlug != nil {
		if _, err := slug.New(*f.ActorSlug); err != nil {
			return p, err
		}
	}
	if f.EntitySlug != nil {
		if _, err := slug.New(*f.EntitySlug); err != nil {
			return p, err
		}
	}
	if f.RequestID != nil && (utf8.RuneCountInString(*f.RequestID) < 1 || utf8.RuneCountInString(*f.RequestID) > 255) {
		return p, xerror.ValidationFailed().Detail("request-id must contain between 1 and 255 characters")
	}
	if f.OccurredFrom != nil && f.OccurredBefore != nil && !f.OccurredFrom.Before(*f.OccurredBefore) {
		return p, xerror.ValidationFailed().Detail("occurred-from must be before occurred-before")
	}
	return p, nil
}

type listSecurityAuditCommand struct {
	apiAuthFlags
	securityAuditFilters
	Limit     *int32                                      `name:"limit" help:"Maximum events to return (1-100)."`
	Cursor    *string                                     `name:"cursor" help:"Pagination cursor from a previous response."`
	Direction *api.ListSecurityAuditEventsParamsDirection `name:"direction" help:"Pagination direction: next or prev."`
	SortBy    *api.ListSecurityAuditEventsParamsSortBy    `name:"sort-by" help:"Sort field: event_occurred_at."`
	Order     *api.ListSecurityAuditEventsParamsOrder     `name:"order" help:"Sort order: asc or desc."`
}

func (c *listSecurityAuditCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	f, err := c.params()
	if err != nil {
		return err
	}
	if c.Limit != nil && (*c.Limit < 1 || *c.Limit > 100) {
		return xerror.ValidationFailed().Detail("limit must be between 1 and 100")
	}
	if c.Direction != nil && !c.Direction.Valid() {
		return xerror.ValidationFailed().Detail("direction must be next or prev")
	}
	if c.SortBy != nil && !c.SortBy.Valid() {
		return xerror.ValidationFailed().Detail("sort-by must be event_occurred_at")
	}
	if c.Order != nil && !c.Order.Valid() {
		return xerror.ValidationFailed().Detail("order must be asc or desc")
	}
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	p := api.ListSecurityAuditEventsParams{Limit: c.Limit, Cursor: c.Cursor, Direction: c.Direction, SortBy: c.SortBy, Order: c.Order, ActorType: f.ActorType, ActorSlug: f.ActorSlug, EntityType: f.EntityType, EntitySlug: f.EntitySlug, Action: f.Action, RequestId: f.RequestId, OccurredFrom: f.OccurredFrom, OccurredBefore: f.OccurredBefore}
	result, err := client.listSecurityAuditEvents(context.Background(), &p)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, result)
}

type exportSecurityAuditCommand struct {
	apiAuthFlags
	securityAuditFilters
}

func (c *exportSecurityAuditCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	p, err := c.params()
	if err != nil {
		return err
	}
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	body, err := client.exportSecurityAuditEvents(context.Background(), &p)
	if err != nil {
		return err
	}
	if _, err := output.stdout.Write(body); err != nil {
		return xerror.Unexpected().With(err)
	}
	return nil
}
