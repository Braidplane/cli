package main

import (
	"context"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

type notificationCommand struct {
	List        listNotificationsCommand       `cmd:"" help:"List organization notifications."`
	Show        showNotificationCommand        `cmd:"" help:"Show one organization notification."`
	Acknowledge acknowledgeNotificationCommand `cmd:"" help:"Acknowledge one open notification."`
	Resolve     resolveNotificationCommand     `cmd:"" help:"Resolve one open or acknowledged notification."`
	Delivery    notificationDeliveryCommand    `cmd:"" help:"Inspect safe alert delivery-attempt diagnostics."`
}

const maxNotificationListLimit int32 = 100

type listNotificationsCommand struct {
	apiAuthFlags

	Limit            *int32 `help:"Maximum number of notifications to return." name:"limit"`
	Cursor           string `help:"Pagination cursor from a previous list response." name:"cursor"`
	Direction        string `help:"Pagination direction: next or prev." name:"direction"`
	LifecycleState   string `help:"Filter by lifecycle state: open, acknowledged, or resolved. Omit to list active unresolved notifications." name:"lifecycle-state"`
	NotificationType string `help:"Filter by notification type. Use pipeline capabilities to discover available producers." name:"notification-type"`
	Severity         string `help:"Filter by severity: info, warning, or critical." name:"severity"`
	SourceKind       string `help:"Filter by source entity, such as pipeline_run. Source action values come from alert policy source metadata." name:"source-kind"`
	SourceAction     string `help:"Filter by source action. Requires --source-kind." name:"source-action"`
	SourceRef        string `help:"Filter by generic source ref or pipeline slug source ref." name:"source-ref"`
	SortBy           string `help:"Sort field: notification_last_seen_at." name:"sort-by"`
	Order            string `help:"Sort order: asc or desc." name:"order"`
}

// Run lists organization notifications with API-backed filters and pagination.
func (c *listNotificationsCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}

	params, err := c.params()
	if err != nil {
		return err
	}

	response, err := client.listNotifications(context.Background(), &params)
	if err != nil {
		return err
	}

	return writeCLIJSON(output.stdout, response)
}

// params converts CLI notification filters to generated OpenAPI query parameters.
func (c *listNotificationsCommand) params() (api.ListNotificationsParams, error) {
	params := api.ListNotificationsParams{}
	if c.Limit != nil {
		if *c.Limit < 1 || *c.Limit > maxNotificationListLimit {
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
		direction := api.ListNotificationsParamsDirection(c.Direction)
		if !direction.Valid() {
			return params, xerror.ValidationFailed().Detail("direction must be next or prev")
		}
		params.Direction = &direction
	}
	if c.LifecycleState != "" {
		lifecycleState := api.NotificationLifecycleState(c.LifecycleState)
		if !lifecycleState.Valid() || lifecycleState == api.NotificationLifecycleStateUnknown {
			return params, xerror.ValidationFailed().Detail("lifecycle-state must be open, acknowledged, or resolved")
		}
		params.LifecycleState = &lifecycleState
	}
	if c.NotificationType != "" {
		notificationType := api.NotificationType(c.NotificationType)
		if !notificationType.Valid() || notificationType == api.NotificationTypeUnknown {
			return params, xerror.ValidationFailed().Detail("notification-type must be a supported API enum value; current producer-backed values are pipeline.retry_exhausted, pipeline.run_succeeded, pipeline.contract_failure, pipeline.run_failed, pipeline.scheduled_run_delayed and pipeline.scheduled_run_missing")
		}
		params.NotificationType = &notificationType
	}
	if c.Severity != "" {
		severity := api.NotificationSeverity(c.Severity)
		if !severity.Valid() || severity == api.NotificationSeverityUnknown {
			return params, xerror.ValidationFailed().Detail("severity must be info, warning, or critical")
		}
		params.Severity = &severity
	}
	if c.SourceKind != "" {
		sourceKind := api.AlertPolicySourceKind(c.SourceKind)
		if !sourceKind.Valid() || sourceKind == api.EventEntityTypeUnknown {
			return params, xerror.ValidationFailed().Detail("source-kind must be a supported event entity type")
		}
		params.SourceKind = &sourceKind
	}
	if c.SourceAction != "" {
		if c.SourceKind == "" {
			return params, xerror.ValidationFailed().Detail("source-action requires source-kind")
		}
		sourceAction := api.AlertPolicySourceAction(c.SourceAction)
		if !sourceAction.Valid() || sourceAction == api.EventActionUnknown {
			return params, xerror.ValidationFailed().Detail("source-action must be a supported event action")
		}
		params.SourceAction = &sourceAction
	}
	if c.SourceRef != "" {
		sourceRef := c.SourceRef
		params.SourceRef = &sourceRef
	}
	if c.SortBy != "" {
		sortBy := api.ListNotificationsParamsSortBy(c.SortBy)
		if !sortBy.Valid() {
			return params, xerror.ValidationFailed().Detail("sort-by must be notification_last_seen_at")
		}
		params.SortBy = &sortBy
	}
	if c.Order != "" {
		order := api.ListNotificationsParamsOrder(c.Order)
		if !order.Valid() {
			return params, xerror.ValidationFailed().Detail("order must be asc or desc")
		}
		params.Order = &order
	}

	return params, nil
}

type showNotificationCommand struct {
	apiAuthFlags

	NotificationSlug string `arg:"" name:"notification-slug" help:"Notification record slug." required:""`
}

// Run prints one display-safe notification record.
func (c *showNotificationCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	return runNotificationSlugCommand(output, runtime, c.apiAuthFlags, c.NotificationSlug, (*cliAPIClient).getNotification)
}

type acknowledgeNotificationCommand struct {
	apiAuthFlags

	NotificationSlug string `arg:"" name:"notification-slug" help:"Notification record slug." required:""`
}

// Run acknowledges one notification and prints the updated record.
func (c *acknowledgeNotificationCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	return runNotificationSlugCommand(output, runtime, c.apiAuthFlags, c.NotificationSlug, (*cliAPIClient).acknowledgeNotification)
}

type resolveNotificationCommand struct {
	apiAuthFlags

	NotificationSlug string `arg:"" name:"notification-slug" help:"Notification record slug." required:""`
}

// Run resolves one notification and prints the updated record.
func (c *resolveNotificationCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	return runNotificationSlugCommand(output, runtime, c.apiAuthFlags, c.NotificationSlug, (*cliAPIClient).resolveNotification)
}

// runNotificationSlugCommand executes one notification-slug API action and prints the returned record.
func runNotificationSlugCommand(
	output *cliOutput,
	runtime *cliRuntime,
	auth apiAuthFlags,
	notificationSlug string,
	call func(*cliAPIClient, context.Context, string) (*api.NotificationRecord, error),
) error {
	client, err := newAPIClient(auth, runtime)
	if err != nil {
		return err
	}

	notification, err := call(client, context.Background(), notificationSlug)
	if err != nil {
		return err
	}

	return writeCLIJSON(output.stdout, notification)
}
