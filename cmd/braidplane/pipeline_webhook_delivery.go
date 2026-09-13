package main

import (
	"context"
	"strings"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

type pipelineWebhookDeliveryCommand struct {
	List   listPipelineWebhookDeliveriesCommand `cmd:"" help:"List safe webhook delivery diagnostics for a trigger."`
	Get    getPipelineWebhookDeliveryCommand    `cmd:"" help:"Get one safe webhook delivery diagnostic."`
	Replay replayPipelineWebhookDeliveryCommand `cmd:"" help:"Explicitly replay retained delivery input when safe."`
}

type pipelineWebhookDeliveryFlags struct {
	apiPipelineFlags
	Environment string `help:"Project environment slug." name:"environment" env:"BRAIDPLANE_ENVIRONMENT" required:""`
	TriggerSlug string `help:"Pipeline webhook trigger slug." name:"trigger" required:""`
}

type listPipelineWebhookDeliveriesCommand struct {
	pipelineWebhookDeliveryFlags
	Limit     int32  `help:"Maximum number of deliveries to return." name:"limit"`
	Cursor    string `help:"Pagination cursor." name:"cursor"`
	Direction string `help:"Pagination direction: next or prev." name:"direction"`
}

func (c *listPipelineWebhookDeliveriesCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, environment, err := newPipelineEnvironmentAPIClient(c.apiPipelineFlags, c.Environment, runtime)
	if err != nil {
		return err
	}
	params, err := listPipelineWebhookDeliveriesParams(c.Limit, c.Cursor, c.Direction)
	if err != nil {
		return err
	}
	deliveries, err := client.listPipelineWebhookDeliveries(context.Background(), project, environment, c.TriggerSlug, params)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, deliveries)
}

type getPipelineWebhookDeliveryCommand struct {
	pipelineWebhookDeliveryFlags
	DeliveryID string `arg:"" name:"delivery-id" required:"" help:"Verified sender delivery identity."`
}

func (c *getPipelineWebhookDeliveryCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, environment, err := newPipelineEnvironmentAPIClient(c.apiPipelineFlags, c.Environment, runtime)
	if err != nil {
		return err
	}
	delivery, err := client.getPipelineWebhookDelivery(context.Background(), project, environment, c.TriggerSlug, c.DeliveryID)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, delivery)
}

type replayPipelineWebhookDeliveryCommand struct {
	pipelineWebhookDeliveryFlags
	DeliveryID string `arg:"" name:"delivery-id" required:"" help:"Verified sender delivery identity."`
}

func (c *replayPipelineWebhookDeliveryCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, environment, err := newPipelineEnvironmentAPIClient(c.apiPipelineFlags, c.Environment, runtime)
	if err != nil {
		return err
	}
	delivery, err := client.replayPipelineWebhookDelivery(context.Background(), project, environment, c.TriggerSlug, c.DeliveryID)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, delivery)
}

func listPipelineWebhookDeliveriesParams(limit int32, cursor, direction string) (*api.ListPipelineWebhookDeliveriesParams, error) {
	if limit < 0 || limit > 100 {
		return nil, xerror.InvalidInput(xerror.Pipeline).Detail("limit must be between 1 and 100")
	}
	params := &api.ListPipelineWebhookDeliveriesParams{}
	if limit > 0 {
		params.Limit = &limit
	}
	if cursor != "" {
		params.Cursor = &cursor
	}
	if direction != "" {
		value := api.ListPipelineWebhookDeliveriesParamsDirection(strings.ToLower(strings.TrimSpace(direction)))
		if !value.Valid() {
			return nil, xerror.InvalidInput(xerror.Pipeline).Detail("direction must be next or prev")
		}
		params.Direction = &value
	}
	return params, nil
}

func (c *cliAPIClient) listPipelineWebhookDeliveries(ctx context.Context, project, environment, trigger string, params *api.ListPipelineWebhookDeliveriesParams) (*api.PipelineWebhookDeliveryListResponse, error) {
	response, err := c.openapi.ListPipelineWebhookDeliveriesWithResponse(ctx, c.organization, project, environment, trigger, params)
	if err != nil {
		return nil, xerror.Wrapf("list pipeline webhook deliveries: %w", err)
	}
	if response.StatusCode() != 200 {
		return nil, decodeAPIError(response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing webhook delivery list payload")
	}
	return response.JSON200, nil
}

func (c *cliAPIClient) getPipelineWebhookDelivery(ctx context.Context, project, environment, trigger, deliveryID string) (*api.PipelineWebhookDelivery, error) {
	response, err := c.openapi.GetPipelineWebhookDeliveryWithResponse(ctx, c.organization, project, environment, trigger, deliveryID)
	if err != nil {
		return nil, xerror.Wrapf("get pipeline webhook delivery: %w", err)
	}
	if response.StatusCode() != 200 {
		return nil, decodeAPIError(response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing webhook delivery payload")
	}
	return response.JSON200, nil
}

func (c *cliAPIClient) replayPipelineWebhookDelivery(ctx context.Context, project, environment, trigger, deliveryID string) (*api.PipelineWebhookDelivery, error) {
	response, err := c.openapi.ReplayPipelineWebhookDeliveryWithResponse(ctx, c.organization, project, environment, trigger, deliveryID)
	if err != nil {
		return nil, xerror.Wrapf("replay pipeline webhook delivery: %w", err)
	}
	if response.StatusCode() != 200 {
		return nil, decodeAPIError(response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing webhook delivery replay payload")
	}
	return response.JSON200, nil
}
