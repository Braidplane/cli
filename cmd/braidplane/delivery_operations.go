package main

import (
	"context"
	"strings"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

type notificationDeliveryCommand struct {
	List listNotificationDeliveryAttemptsCommand `cmd:"" help:"List safe delivery attempts for one notification."`
}
type listNotificationDeliveryAttemptsCommand struct {
	apiAuthFlags
	NotificationSlug string `arg:"" name:"notification-slug" required:"" help:"Notification record slug."`
	Limit            int32  `help:"Maximum attempts to return." name:"limit"`
	Cursor           string `help:"Pagination cursor." name:"cursor"`
	SortBy           string `help:"Sort field: alert_delivery_attempt_created_at." name:"sort-by"`
	Order            string `help:"Sort order: asc or desc." name:"order"`
	Direction        string `help:"Pagination direction: next or prev." name:"direction"`
}

func (c *listNotificationDeliveryAttemptsCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	params, err := notificationDeliveryAttemptParams(c.Limit, c.Cursor, c.SortBy, c.Order, c.Direction)
	if err != nil {
		return err
	}
	attempts, err := client.listNotificationDeliveryAttempts(context.Background(), c.NotificationSlug, params)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, attempts)
}

func notificationDeliveryAttemptParams(limit int32, cursor, sortBy, order, direction string) (*api.ListNotificationDeliveryAttemptsParams, error) {
	if limit < 0 || limit > 100 {
		return nil, xerror.InvalidInput(xerror.Notification).Detail("limit must be between 1 and 100")
	}
	p := &api.ListNotificationDeliveryAttemptsParams{}
	if limit > 0 {
		v := limit
		p.Limit = &v
	}
	if cursor != "" {
		v := cursor
		p.Cursor = &v
	}
	if sortBy != "" {
		v := api.ListNotificationDeliveryAttemptsParamsSortBy(strings.TrimSpace(sortBy))
		if !v.Valid() {
			return nil, xerror.InvalidInput(xerror.Notification).Detail("sort-by must be alert_delivery_attempt_created_at")
		}
		p.SortBy = &v
	}
	if order != "" {
		v := api.ListNotificationDeliveryAttemptsParamsOrder(strings.ToLower(strings.TrimSpace(order)))
		if !v.Valid() {
			return nil, xerror.InvalidInput(xerror.Notification).Detail("order must be asc or desc")
		}
		p.Order = &v
	}
	if direction != "" {
		v := api.ListNotificationDeliveryAttemptsParamsDirection(strings.ToLower(strings.TrimSpace(direction)))
		if !v.Valid() {
			return nil, xerror.InvalidInput(xerror.Notification).Detail("direction must be next or prev")
		}
		p.Direction = &v
	}
	return p, nil
}

type alertDeliveryCommand struct {
	List listAlertDeliveryAttemptsCommand `cmd:"" help:"List recent delivery attempts for selected notifications."`
}
type listAlertDeliveryAttemptsCommand struct {
	apiAuthFlags
	NotificationSlugs    []string `help:"Notification slug. Repeat for multiple notifications." name:"notification" required:""`
	LimitPerNotification int      `help:"Maximum recent attempts per notification." name:"limit-per-notification"`
}

func (c *listAlertDeliveryAttemptsCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	if c.LimitPerNotification < 0 {
		return xerror.InvalidInput(xerror.Notification).Detail("limit-per-notification must be positive")
	}
	params := &api.ListAlertDeliveryAttemptsParams{NotificationSlugs: append(api.NotificationSlugsParam(nil), c.NotificationSlugs...)}
	if c.LimitPerNotification > 0 {
		v := c.LimitPerNotification
		params.LimitPerNotification = &v
	}
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	attempts, err := client.listAlertDeliveryAttempts(context.Background(), params)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, attempts)
}

type listAlertPolicySourceMetadataCommand struct {
	apiAuthFlags
	Scope           string `help:"Policy scope: organization, project, or project_environment." name:"scope"`
	ProjectSlug     string `help:"Project slug used to constrain source refs." name:"project"`
	EnvironmentSlug string `help:"Environment slug used to constrain source refs." name:"environment"`
	SourceKind      string `help:"Optional event source kind filter." name:"source-kind"`
	SourceAction    string `help:"Optional event source action filter." name:"source-action"`
}

func (c *listAlertPolicySourceMetadataCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	params, err := c.params()
	if err != nil {
		return err
	}
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	metadata, err := client.listAlertPolicySourceMetadata(context.Background(), params)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, metadata)
}

func (c *listAlertPolicySourceMetadataCommand) params() (*api.ListAlertPolicySourceMetadataParams, error) {
	p := &api.ListAlertPolicySourceMetadataParams{}
	if v := strings.TrimSpace(c.Scope); v != "" {
		typed := api.AlertPolicyScope(v)
		if !typed.Valid() {
			return nil, xerror.InvalidInput(xerror.Notification).Detail("scope must be organization, project, or project_environment")
		}
		p.Scope = &typed
	}
	if v := strings.TrimSpace(c.ProjectSlug); v != "" {
		typed := v
		p.ProjectSlug = &typed
	}
	if v := strings.TrimSpace(c.EnvironmentSlug); v != "" {
		typed := v
		p.EnvironmentSlug = &typed
	}
	if v := strings.TrimSpace(c.SourceKind); v != "" {
		typed := api.EventEntityType(v)
		if !typed.Valid() {
			return nil, xerror.InvalidInput(xerror.Notification).Detail("unsupported source-kind")
		}
		p.SourceKind = &typed
	}
	if v := strings.TrimSpace(c.SourceAction); v != "" {
		typed := api.EventAction(v)
		if !typed.Valid() {
			return nil, xerror.InvalidInput(xerror.Notification).Detail("unsupported source-action")
		}
		p.SourceAction = &typed
	}
	return p, nil
}
