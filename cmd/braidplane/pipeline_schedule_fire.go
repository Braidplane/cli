package main

import (
	"context"
	"strings"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

type pipelineScheduleFireCommand struct {
	List listPipelineScheduleFiresCommand `cmd:"" help:"List persisted schedule-fire diagnostics for a trigger."`
}
type listPipelineScheduleFiresCommand struct {
	apiPipelineFlags
	Environment string `help:"Project environment slug." name:"environment" env:"BRAIDPLANE_ENVIRONMENT" required:""`
	TriggerSlug string `arg:"" name:"trigger-slug" required:"" help:"Pipeline trigger slug."`
	Limit       int32  `help:"Maximum number of fires to return." name:"limit"`
	Cursor      string `help:"Pagination cursor." name:"cursor"`
	Direction   string `help:"Pagination direction: next or prev." name:"direction"`
}

func (c *listPipelineScheduleFiresCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, environment, err := newPipelineEnvironmentAPIClient(c.apiPipelineFlags, c.Environment, runtime)
	if err != nil {
		return err
	}
	params, err := listPipelineScheduleFiresParams(c.Limit, c.Cursor, c.Direction)
	if err != nil {
		return err
	}
	fires, err := client.listPipelineScheduleFires(context.Background(), project, environment, c.TriggerSlug, params)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, fires)
}

func listPipelineScheduleFiresParams(limit int32, cursor, direction string) (*api.ListPipelineScheduleFiresParams, error) {
	if limit < 0 || limit > 100 {
		return nil, xerror.InvalidInput(xerror.Pipeline).Detail("limit must be between 1 and 100")
	}
	params := &api.ListPipelineScheduleFiresParams{}
	if limit > 0 {
		v := limit
		params.Limit = &v
	}
	if cursor != "" {
		v := cursor
		params.Cursor = &v
	}
	if direction != "" {
		v := api.ListPipelineScheduleFiresParamsDirection(strings.ToLower(strings.TrimSpace(direction)))
		if !v.Valid() {
			return nil, xerror.InvalidInput(xerror.Pipeline).Detail("direction must be next or prev")
		}
		params.Direction = &v
	}
	return params, nil
}

func (c *cliAPIClient) listPipelineScheduleFires(ctx context.Context, project, environment, trigger string, params *api.ListPipelineScheduleFiresParams) (*api.PipelineScheduleFireListResponse, error) {
	resp, err := c.openapi.ListPipelineScheduleFiresWithResponse(ctx, c.organization, project, environment, trigger, params)
	if err != nil {
		return nil, xerror.Wrapf("list pipeline schedule fires: %w", err)
	}
	if resp.StatusCode() != 200 {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing schedule fire list payload")
	}
	return resp.JSON200, nil
}
