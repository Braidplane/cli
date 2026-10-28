package main

import (
	"context"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

type inspectPipelineRunCompletionCommand struct {
	apiPipelineFlags
	Connected   bool   `help:"Inspect the bounded historical connected execution instead of one logical run."`
	RunSlug     string `arg:"" name:"run-slug" help:"Any run attempt in the logical root." required:""`
	Environment string `help:"Project environment slug." name:"environment" env:"BRAIDPLANE_ENVIRONMENT"`
}

func (c *inspectPipelineRunCompletionCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, environment, err := newPipelineEnvironmentAPIClient(c.apiPipelineFlags, c.Environment, runtime)
	if err != nil {
		return err
	}
	if c.Connected {
		response, err := client.openapi.GetPipelineRunConnectedWithResponse(context.Background(), client.organization, project, environment, c.RunSlug)
		if err != nil {
			return xerror.Wrapf("inspect connected runs: %w", err)
		}
		if response.StatusCode() != 200 {
			return decodeAPIError(response.StatusCode(), response.Body)
		}
		if response.JSON200 == nil {
			return xerror.Unexpected().Detail("unexpected API response: missing connected evidence")
		}
		return writeCLIJSON(output.stdout, response.JSON200)
	}
	evidence, err := client.getPipelineRunCompletion(context.Background(), project, environment, c.RunSlug)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, evidence)
}

func (c *cliAPIClient) getPipelineRunCompletion(ctx context.Context, project, environment, run string) (*api.PipelineRunCompletion, error) {
	response, err := c.openapi.GetPipelineRunCompletionWithResponse(ctx, c.organization, project, environment, run)
	if err != nil {
		return nil, xerror.Wrapf("inspect pipeline run completion: %w", err)
	}
	if response.StatusCode() != 200 {
		return nil, decodeAPIError(response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing completion evidence")
	}
	return response.JSON200, nil
}
