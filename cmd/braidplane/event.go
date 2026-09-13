package main

import (
	"context"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

type eventCommand struct {
	List listEventsCommand `cmd:"" help:"List durable organization events from the Braidplane API."`
}

const maxEventListLimit int32 = 100

type listEventsCommand struct {
	apiAuthFlags

	Limit      *int32 `help:"Maximum number of events to return." name:"limit"`
	Cursor     string `help:"Pagination cursor from a previous list response." name:"cursor"`
	Direction  string `help:"Pagination direction: next or prev." name:"direction"`
	ActorType  string `help:"Filter by actor type: api_key, external, system, unknown, or user." name:"actor-type"`
	ActorSlug  string `help:"Filter by actor slug." name:"actor-slug"`
	EntityType string `help:"Filter by affected entity type." name:"entity-type"`
	EntitySlug string `help:"Filter by affected entity slug." name:"entity-slug"`
	Action     string `help:"Filter by event action." name:"action"`
	SortBy     string `help:"Sort field: event_occurred_at." name:"sort-by"`
	Order      string `help:"Sort order: asc or desc." name:"order"`
}

// Run lists organization-scoped durable events using the shared API-backed CLI output contract.
func (c *listEventsCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}

	params, err := c.params()
	if err != nil {
		return err
	}

	events, err := client.listEvents(context.Background(), &params)
	if err != nil {
		if c.Cursor != "" {
			return xerror.Wrapf("list events with cursor %q: %w", c.Cursor, err)
		}
		return xerror.Wrapf("list events: %w", err)
	}

	return writeCLIJSON(output.stdout, events)
}

// params converts CLI flags to the generated OpenAPI event list query parameters.
func (c *listEventsCommand) params() (api.ListEventsParams, error) {
	params := api.ListEventsParams{}
	if c.Limit != nil {
		if *c.Limit < 1 || *c.Limit > maxEventListLimit {
			return params, xerror.ValidationFailed().Detail("limit must be between 1 and 100")
		}
		limit := *c.Limit
		params.Limit = &limit
	}
	if c.Cursor != "" {
		cursor := c.Cursor
		params.Cursor = &cursor
	}
	if c.Direction != "" {
		direction := api.ListEventsParamsDirection(c.Direction)
		if !direction.Valid() {
			return params, xerror.ValidationFailed().Detail("direction must be next or prev")
		}
		params.Direction = &direction
	}
	if c.ActorType != "" {
		actorType := api.EventActorType(c.ActorType)
		if !actorType.Valid() {
			return params, xerror.ValidationFailed().Detail("actor-type must be one of api_key, external, system, unknown, or user")
		}
		params.ActorType = &actorType
	}
	if c.ActorSlug != "" {
		actorSlug := c.ActorSlug
		params.ActorSlug = &actorSlug
	}
	if c.EntityType != "" {
		entityType := api.EventEntityType(c.EntityType)
		if !entityType.Valid() {
			return params, xerror.ValidationFailed().Detail("entity-type must be one of api_key, data_source, data_source_binding, data_source_ref, environment, member, member_invitation, organization, pipeline, pipeline_checkpoint, pipeline_deployment, pipeline_run, pipeline_trigger, pipeline_version, project, project_environment, query, query_execution, role, or unknown")
		}
		params.EntityType = &entityType
	}
	if c.EntitySlug != "" {
		entitySlug := c.EntitySlug
		params.EntitySlug = &entitySlug
	}
	if c.Action != "" {
		action := api.EventAction(c.Action)
		if !action.Valid() {
			return params, xerror.ValidationFailed().Detail("action must be one of accept, auto_accept, create, decline, delete, deploy, disable, enable, execute, expire, publish, purge, reconcile, refresh, restore, revoke, rotate, test_connection, unknown, or update")
		}
		params.Action = &action
	}
	if c.SortBy != "" {
		sortBy := api.ListEventsParamsSortBy(c.SortBy)
		if !sortBy.Valid() {
			return params, xerror.ValidationFailed().Detail("sort-by must be event_occurred_at")
		}
		params.SortBy = &sortBy
	}
	if c.Order != "" {
		order := api.ListEventsParamsOrder(c.Order)
		if !order.Valid() {
			return params, xerror.ValidationFailed().Detail("order must be asc or desc")
		}
		params.Order = &order
	}

	return params, nil
}
